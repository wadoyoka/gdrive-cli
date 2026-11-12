package repository

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

const (
	clientSecretName       = "client_secret.enc"
	tokenName              = "token.enc"
	legacyClientSecretName = "client_secret.json"
	legacyTokenName        = "token.json"
	defaultRevokeURL       = "https://oauth2.googleapis.com/revoke"
	defaultTimeout         = 5 * time.Minute
	keyLockTimeout         = 30 * time.Second
)

const (
	msgNoKey    = "鍵が見つかりません。`gdrive-cli login --client-secret <JSON>` をやり直してください"
	msgMismatch = "今の鍵で読めません。`gdrive-cli login --client-secret <JSON>` をやり直してください"
	msgLegacy   = "古い形式のファイルがあります。`gdrive-cli login --client-secret <JSON>` をやり直してください"
)

// Auth は、Google へのログインと、設定ディレクトリのトークンを扱う。
type Auth struct {
	Dir  string
	Keys KeyStore
	Lock func(ctx context.Context) (unlock func(), err error) // 鍵を作るときの排他。nil ならロックしない

	mu  sync.Mutex
	key []byte // Keys.Load で見つけた鍵。mu で守る

	OpenBrowser  func(url string) error
	Endpoint     oauth2.Endpoint // ゼロ値なら google.Endpoint
	RevokeURL    string          // "" なら https://oauth2.googleapis.com/revoke
	HTTPClient   *http.Client    // nil なら http.DefaultClient
	LoginTimeout time.Duration   // 0 なら 5 分
}

// NewAuth は、dir を使う Auth を返す。
func NewAuth(dir string, keys KeyStore, lock func(context.Context) (func(), error)) *Auth {
	return &Auth{Dir: dir, Keys: keys, Lock: lock, OpenBrowser: OpenBrowser}
}

func (a *Auth) clientSecretPath() string { return filepath.Join(a.Dir, clientSecretName) }
func (a *Auth) tokenPath() string        { return filepath.Join(a.Dir, tokenName) }

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// HasClientSecret は、client_secret.enc があるかを返す。
func (a *Auth) HasClientSecret() bool { return fileExists(a.clientSecretPath()) }

// HasToken は、token.enc があるかを返す。
func (a *Auth) HasToken() bool { return fileExists(a.tokenPath()) }

func (a *Auth) httpClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}

func (a *Auth) withHTTPClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, a.httpClient())
}

// loadKey は、鍵を Keys.Load で探す。見つけたらメモリに持つ。鍵を作ることはしない。
func (a *Auth) loadKey() (key []byte, found bool, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.key != nil {
		return a.key, true, nil
	}
	key, found, err = a.Keys.Load()
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	a.key = key
	return key, true, nil
}

// ensureKey は、Lock の中で鍵を探し、確かにないときだけ作って保存する。新しく作ったら created が true。
func (a *Auth) ensureKey(ctx context.Context) (key []byte, created bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, keyLockTimeout)
	defer cancel()
	if a.Lock != nil {
		unlock, lockErr := a.Lock(ctx)
		if lockErr != nil {
			return nil, false, domain.Errorf(domain.KindUnexpected, "鍵を作る順番を待てません").Wrap(lockErr)
		}
		defer unlock()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key, found, err := a.Keys.Load()
	if err != nil {
		return nil, false, err
	}
	if found {
		a.key = key
		return key, false, nil
	}
	key, err = newKey()
	if err != nil {
		return nil, false, domain.Errorf(domain.KindUnexpected, "鍵を作れません").Wrap(err)
	}
	if err := a.Keys.Save(key); err != nil {
		return nil, false, err
	}
	a.key = key
	return key, true, nil
}

// readSealed は、name を読んで今の鍵で復号する。ファイルがなければ os.ErrNotExist を返す。
func (a *Auth) readSealed(name, kind string) ([]byte, error) {
	sealed, err := os.ReadFile(filepath.Join(a.Dir, name)) //nolint:gosec // 設定ディレクトリの中
	if errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err != nil {
		return nil, domain.Errorf(domain.KindUnexpected, "%s を読めません", name).Wrap(err)
	}
	key, found, err := a.loadKey()
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, domain.Errorf(domain.KindSetup, "%s", msgNoKey)
	}
	plain, err := openSecret(key, kind, sealed)
	if err != nil {
		return nil, domain.Errorf(domain.KindSetup, "%s", msgMismatch).Wrap(err)
	}
	return plain, nil
}

func (a *Auth) writeSealed(name, kind string, plain []byte) error {
	key, found, err := a.loadKey()
	if err != nil {
		return err
	}
	if !found {
		return domain.Errorf(domain.KindSetup, "鍵が見つかりません。`gdrive-cli login` をやり直してください")
	}
	return a.writeSealedWith(key, name, kind, plain)
}

func (a *Auth) writeSealedWith(key []byte, name, kind string, plain []byte) error {
	sealed, err := sealSecret(key, kind, plain)
	if err != nil {
		return domain.Errorf(domain.KindUnexpected, "%s を暗号化できません", name).Wrap(err)
	}
	if err := writeFileAtomic(filepath.Join(a.Dir, name), sealed, 0o600); err != nil {
		return domain.Errorf(domain.KindUnexpected, "%s を保存できません", name).Wrap(err)
	}
	return nil
}

func (a *Auth) legacyExists() bool {
	return fileExists(filepath.Join(a.Dir, legacyClientSecretName)) || fileExists(filepath.Join(a.Dir, legacyTokenName))
}

func (a *Auth) config() (*oauth2.Config, error) {
	data, err := a.readSealed(clientSecretName, secretKindClientSecret)
	if errors.Is(err, os.ErrNotExist) {
		if a.legacyExists() {
			return nil, domain.Errorf(domain.KindSetup, "%s", msgLegacy)
		}
		return nil, domain.Errorf(domain.KindSetup, "クライアントシークレットがありません。`gdrive-cli login --client-secret <path>` で取り込んでください")
	}
	if err != nil {
		return nil, err
	}
	cfg, err := google.ConfigFromJSON(data, drive.DriveFileScope)
	if err != nil {
		return nil, domain.Errorf(domain.KindSetup, "クライアントシークレットを読めません。`gdrive-cli login --client-secret <path>` で取り込み直してください").Wrap(err)
	}
	if a.Endpoint != (oauth2.Endpoint{}) {
		cfg.Endpoint = a.Endpoint
	} else {
		cfg.Endpoint = google.Endpoint
	}
	return cfg, nil
}

// readClientSecretFile は、path の client_secret を読み、形を検める。中身と installed.project_id を返す。
func readClientSecretFile(path string) (data []byte, projectID string, err error) {
	data, err = os.ReadFile(path) //nolint:gosec // 利用者が指定したパスを読むのが目的
	if err != nil {
		return nil, "", domain.Errorf(domain.KindUsage, "client_secret のファイルを読めません: %s", path).Wrap(err)
	}
	if _, err := google.ConfigFromJSON(data, drive.DriveFileScope); err != nil {
		return nil, "", domain.Errorf(domain.KindUsage, "client_secret の形式が正しくありません（デスクトップアプリの OAuth クライアントの JSON を指定してください）").
			WithDetails(err.Error()).Wrap(err)
	}
	var doc struct {
		Installed struct {
			ProjectID string `json:"project_id"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, "", domain.Errorf(domain.KindUsage, "client_secret の形式が正しくありません").Wrap(err)
	}
	return data, doc.Installed.ProjectID, nil
}

// ClientSecretProjectID は、path の client_secret を検めて、installed.project_id を返す。何も保存しない。
func (a *Auth) ClientSecretProjectID(path string) (string, error) {
	_, id, err := readClientSecretFile(path)
	return id, err
}

// ImportClientSecret は、path の client_secret を検めて、暗号化して設定ディレクトリへ 0600 で置く。
// 鍵がなければここで作る（鍵を作るのはここだけ）。
func (a *Auth) ImportClientSecret(path string) error {
	data, _, err := readClientSecretFile(path)
	if err != nil {
		return err
	}
	key, created, err := a.ensureKey(context.Background())
	if err != nil {
		return err
	}
	if created {
		// 古い鍵で暗号化した token.enc は、新しい鍵では読めない。
		if err := os.Remove(a.tokenPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return domain.Errorf(domain.KindUnexpected, "古い %s を消せません", tokenName).Wrap(err)
		}
	}
	return a.writeSealedWith(key, clientSecretName, secretKindClientSecret, data)
}

func (a *Auth) loadToken() (*oauth2.Token, error) {
	data, err := a.readSealed(tokenName, secretKindToken)
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// saveToken は、トークンを暗号化して置く。鍵は作らない。
func (a *Auth) saveToken(tok *oauth2.Token) error {
	data, err := json.MarshalIndent(tok, "", "  ") //nolint:gosec // トークンの保存が目的
	if err != nil {
		return err
	}
	return a.writeSealed(tokenName, secretKindToken, data)
}

// Inspect は、保存したものが今の鍵で読めるかを調べる。KeyStore は常に埋める。
func (a *Auth) Inspect() (usecase.AuthInspection, error) {
	insp := usecase.AuthInspection{KeyStore: a.Keys.Kind()}
	hasCS, hasTok := a.HasClientSecret(), a.HasToken()
	if !hasCS && !hasTok {
		if _, _, err := a.loadKey(); err != nil { // 見つかったかは問わない。置き場が使えるかだけを見る
			return insp, err
		}
		if a.legacyExists() {
			insp.Problem = msgLegacy
		}
		return insp, nil
	}
	_, found, err := a.loadKey()
	if err != nil {
		return insp, err
	}
	if !found {
		insp.Problem = msgNoKey
		return insp, nil
	}
	if hasCS {
		_, err := a.readSealed(clientSecretName, secretKindClientSecret)
		insp.ClientSecret = err == nil
		if err != nil {
			insp.Problem = msgMismatch
		}
	}
	if hasTok {
		_, err := a.readSealed(tokenName, secretKindToken)
		insp.Token = err == nil
		if err != nil {
			insp.Problem = msgMismatch
		}
	}
	return insp, nil
}

func needLogin() *domain.Error {
	return domain.Errorf(domain.KindSetup, "ログインしていません。`gdrive-cli login` を実行してください")
}

// savingSource は、更新で AccessToken が変わったら token.enc を書き直す。
type savingSource struct {
	mu    sync.Mutex
	inner oauth2.TokenSource
	last  string
	save  func(*oauth2.Token) error
}

func (s *savingSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, err := s.inner.Token()
	if err != nil {
		return nil, err
	}
	if tok.AccessToken != s.last {
		if err := s.save(tok); err != nil {
			var de *domain.Error
			if errors.As(err, &de) {
				return nil, err
			}
			return nil, domain.Errorf(domain.KindUnexpected, "%s を保存できません", tokenName).Wrap(err)
		}
		s.last = tok.AccessToken
	}
	return tok, nil
}

// TokenSource は、token.enc から、更新したら保存し直す TokenSource を返す。
func (a *Auth) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if !a.HasToken() {
		if fileExists(filepath.Join(a.Dir, legacyTokenName)) {
			return nil, domain.Errorf(domain.KindSetup, "%s", msgLegacy)
		}
		return nil, needLogin()
	}
	cfg, err := a.config()
	if err != nil {
		return nil, err
	}
	tok, err := a.loadToken()
	if err != nil {
		var de *domain.Error
		if errors.As(err, &de) {
			return nil, err
		}
		return nil, domain.Errorf(domain.KindSetup, "%s を読めません。`gdrive-cli login` を実行してください", tokenName).Wrap(err)
	}
	return &savingSource{
		inner: cfg.TokenSource(a.withHTTPClient(ctx), tok),
		last:  tok.AccessToken,
		save:  a.saveToken,
	}, nil
}

type callbackResult struct {
	code string
	err  error
}

// parseCallback は、リダイレクトのクエリから認可コードを取り出す。
func parseCallback(q url.Values, state string) (code string, ok bool, err error) {
	if q.Get("code") == "" && q.Get("error") == "" {
		return "", false, nil
	}
	if q.Get("state") != state {
		return "", true, domain.Errorf(domain.KindSetup, "state が一致しません。もう一度 `gdrive-cli login` を実行してください")
	}
	if e := q.Get("error"); e != "" {
		return "", true, domain.Errorf(domain.KindSetup, "Google から認可が拒否されました: %s", e)
	}
	if c := q.Get("code"); c != "" {
		return c, true, nil
	}
	return "", false, nil
}

func newState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Login は、ブラウザ（または貼り付け）で認可コードを受け取り、token.enc に保存する。
func (a *Auth) Login(ctx context.Context, opt usecase.LoginOptions) error {
	cfg, err := a.config()
	if err != nil {
		return err
	}
	timeout := a.LoginTimeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return domain.Errorf(domain.KindUnexpected, "ローカルで待ち受けられません").Wrap(err)
	}
	cfg.RedirectURL = fmt.Sprintf("http://%s/", ln.Addr().String())

	state, err := newState()
	if err != nil {
		_ = ln.Close()
		return domain.Errorf(domain.KindUnexpected, "state を作れません").Wrap(err)
	}
	verifier := oauth2.GenerateVerifier()
	authURL := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "consent"))

	results := make(chan callbackResult, 2) // 受け口ごとに高々 1 件
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code, ok, perr := parseCallback(r.URL.Query(), state)
		switch {
		case !ok:
			http.NotFound(w, r)
		case perr != nil:
			http.Error(w, "ログインできませんでした。ターミナルを確認してください。", http.StatusBadRequest)
			trySend(results, callbackResult{err: perr})
		default:
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = fmt.Fprint(w, "ログインできました。このタブを閉じてください。")
			trySend(results, callbackResult{code: code})
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(ln)
	}()
	defer func() {
		_ = srv.Close()
		<-served
	}()

	_, _ = fmt.Fprintf(opt.Prompt, "ブラウザで次の URL を開いてログインしてください:\n%s\n", authURL)
	if opt.NoBrowser {
		_, _ = fmt.Fprintln(opt.Prompt, "ブラウザが開けない場合は、ログイン後にアドレスバーへ表示された URL をここに貼り付けて Enter を押してください。")
		go readPasted(opt, state, results)
	} else if a.OpenBrowser != nil {
		if err := a.OpenBrowser(authURL); err != nil {
			_, _ = fmt.Fprintf(opt.Prompt, "ブラウザを開けませんでした（%v）。上の URL を手で開いてください。\n", err)
		}
	}

	var code string
	select {
	case res := <-results:
		if res.err != nil {
			return res.err
		}
		code = res.code
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return domain.Errorf(domain.KindSetup, "時間切れです。もう一度 `gdrive-cli login` を実行してください")
		}
		return domain.Errorf(domain.KindUnexpected, "ログインを中断しました").Wrap(ctx.Err())
	}

	tok, err := cfg.Exchange(a.withHTTPClient(ctx), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return domain.Errorf(domain.KindSetup, "認可コードをトークンに交換できません").WithDetails(err.Error()).Wrap(err)
	}
	if err := a.saveToken(tok); err != nil {
		return err
	}
	a.removeLegacy(opt)
	return nil
}

// removeLegacy は、旧形式の平文のファイルが残っていれば消して、そのことを Prompt に書く。
func (a *Auth) removeLegacy(opt usecase.LoginOptions) {
	for _, name := range []string{legacyTokenName, legacyClientSecretName} {
		p := filepath.Join(a.Dir, name)
		if !fileExists(p) {
			continue
		}
		if err := os.Remove(p); err == nil && opt.Prompt != nil {
			_, _ = fmt.Fprintf(opt.Prompt, "古い形式のファイルを消しました: %s\n", name)
		}
	}
}

func trySend(ch chan<- callbackResult, r callbackResult) {
	select {
	case ch <- r:
	default:
	}
}

// readPasted は、Input を 1 行ずつ読み、貼り付けられた URL のクエリから認可コードを取り出す。
// 空行や、URL でない行・認可コードのない行は案内を出して読み続ける。
// 結果を送るのは、認可コードを得たときと、state の不一致や error パラメータのときだけ。
// EOF では何も送らず戻る（ループバックか時間切れの判定に任せる）。
// Input の読み取りは中断できないため、Input が閉じられるか行が届くまで残ることがある。
func readPasted(opt usecase.LoginOptions, state string, results chan<- callbackResult) {
	if opt.Input == nil {
		return
	}
	reader := bufio.NewReader(opt.Input)
	for {
		raw, readErr := reader.ReadString('\n')
		if line := strings.TrimSpace(raw); line != "" {
			u, perr := url.Parse(line)
			if perr != nil {
				_, _ = fmt.Fprintln(opt.Prompt, "URL として読めませんでした。ログイン後にアドレスバーへ表示された URL を貼り付けてください。")
			} else {
				code, ok, cerr := parseCallback(u.Query(), state)
				switch {
				case cerr != nil:
					trySend(results, callbackResult{err: cerr})
					return
				case !ok:
					_, _ = fmt.Fprintln(opt.Prompt, "その URL には認可コードがありません。ログイン後にアドレスバーへ表示された URL を貼り付けてください。")
				default:
					trySend(results, callbackResult{code: code})
					return
				}
			}
		}
		if readErr != nil {
			return
		}
	}
}

// Logout は、Google 側でトークンを取り消してから token.enc と旧 token.json を消す。鍵は消さない。
func (a *Auth) Logout(ctx context.Context) error {
	// 鍵の置き場そのものが使えないときは、何も消さずにそのエラーを返す（トークンは Google 側でまだ有効）。
	if a.HasToken() {
		if _, _, keyErr := a.loadKey(); keyErr != nil {
			return keyErr
		}
	}
	tok, err := a.loadToken()
	if errors.Is(err, os.ErrNotExist) {
		if rmErr := os.Remove(filepath.Join(a.Dir, legacyTokenName)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return domain.Errorf(domain.KindUnexpected, "%s を消せません", legacyTokenName).Wrap(rmErr)
		}
		return nil
	}
	var revokeErr error
	if err != nil {
		revokeErr = err
	} else {
		revokeErr = a.revoke(ctx, tok)
	}
	for _, p := range []string{a.tokenPath(), filepath.Join(a.Dir, legacyTokenName)} {
		if rmErr := os.Remove(p); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return domain.Errorf(domain.KindUnexpected, "%s を消せません", filepath.Base(p)).Wrap(rmErr)
		}
	}
	if revokeErr != nil {
		return domain.Errorf(domain.KindUnexpected, "トークンは消しましたが、Google 側での取り消しに失敗しました").
			WithDetails(revokeErr.Error()).Wrap(revokeErr)
	}
	return nil
}

func (a *Auth) revoke(ctx context.Context, tok *oauth2.Token) error {
	value := tok.RefreshToken
	if value == "" {
		value = tok.AccessToken
	}
	endpoint := a.RevokeURL
	if endpoint == "" {
		endpoint = defaultRevokeURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(url.Values{"token": {value}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("取り消しの応答: %s", resp.Status)
	}
	return nil
}
