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
	clientSecretName = "client_secret.json"
	tokenName        = "token.json"
	defaultRevokeURL = "https://oauth2.googleapis.com/revoke"
	defaultTimeout   = 5 * time.Minute
)

// Auth は、Google へのログインと、設定ディレクトリのトークンを扱う。
type Auth struct {
	Dir          string
	OpenBrowser  func(url string) error
	Endpoint     oauth2.Endpoint // ゼロ値なら google.Endpoint
	RevokeURL    string          // "" なら https://oauth2.googleapis.com/revoke
	HTTPClient   *http.Client    // nil なら http.DefaultClient
	LoginTimeout time.Duration   // 0 なら 5 分
}

// NewAuth は、dir を使う Auth を返す。
func NewAuth(dir string) *Auth { return &Auth{Dir: dir, OpenBrowser: OpenBrowser} }

func (a *Auth) clientSecretPath() string { return filepath.Join(a.Dir, clientSecretName) }
func (a *Auth) tokenPath() string        { return filepath.Join(a.Dir, tokenName) }

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// HasClientSecret は、client_secret.json があるかを返す。
func (a *Auth) HasClientSecret() bool { return fileExists(a.clientSecretPath()) }

// HasToken は、token.json があるかを返す。
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

func (a *Auth) config() (*oauth2.Config, error) {
	data, err := os.ReadFile(a.clientSecretPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.Errorf(domain.KindSetup, "client_secret.json がありません。`gdrive-cli init --client-secret <path>` で取り込んでください")
	}
	if err != nil {
		return nil, domain.Errorf(domain.KindUnexpected, "client_secret.json を読めません").Wrap(err)
	}
	cfg, err := google.ConfigFromJSON(data, drive.DriveFileScope)
	if err != nil {
		return nil, domain.Errorf(domain.KindSetup, "client_secret.json を読めません。`gdrive-cli init --client-secret <path>` で取り込み直してください").Wrap(err)
	}
	if a.Endpoint != (oauth2.Endpoint{}) {
		cfg.Endpoint = a.Endpoint
	} else {
		cfg.Endpoint = google.Endpoint
	}
	return cfg, nil
}

// ImportClientSecret は、path の client_secret を検めて、設定ディレクトリへ 0600 でコピーする。
func (a *Auth) ImportClientSecret(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // 利用者が指定したパスを読むのが目的
	if err != nil {
		return domain.Errorf(domain.KindUsage, "client_secret のファイルを読めません: %s", path).Wrap(err)
	}
	if _, err := google.ConfigFromJSON(data, drive.DriveFileScope); err != nil {
		return domain.Errorf(domain.KindUsage, "client_secret の形式が正しくありません（デスクトップアプリの OAuth クライアントの JSON を指定してください）").
			WithDetails(err.Error()).Wrap(err)
	}
	if err := writeFileAtomic(a.clientSecretPath(), data, 0o600); err != nil {
		return domain.Errorf(domain.KindUnexpected, "client_secret.json を保存できません").Wrap(err)
	}
	return nil
}

func (a *Auth) loadToken() (*oauth2.Token, error) {
	data, err := os.ReadFile(a.tokenPath())
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func (a *Auth) saveToken(tok *oauth2.Token) error {
	data, err := json.MarshalIndent(tok, "", "  ") //nolint:gosec // トークンの保存が目的
	if err != nil {
		return err
	}
	return writeFileAtomic(a.tokenPath(), data, 0o600)
}

func needLogin() *domain.Error {
	return domain.Errorf(domain.KindSetup, "ログインしていません。`gdrive-cli login` を実行してください")
}

// savingSource は、更新で AccessToken が変わったら token.json を書き直す。
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
			return nil, domain.Errorf(domain.KindUnexpected, "token.json を保存できません").Wrap(err)
		}
		s.last = tok.AccessToken
	}
	return tok, nil
}

// TokenSource は、token.json から、更新したら保存し直す TokenSource を返す。
func (a *Auth) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if !a.HasToken() || !a.HasClientSecret() {
		return nil, needLogin()
	}
	cfg, err := a.config()
	if err != nil {
		return nil, err
	}
	tok, err := a.loadToken()
	if err != nil {
		return nil, domain.Errorf(domain.KindSetup, "token.json を読めません。`gdrive-cli login` を実行してください").Wrap(err)
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

// Login は、ブラウザ（または貼り付け）で認可コードを受け取り、token.json に保存する。
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
		return domain.Errorf(domain.KindUnexpected, "token.json を保存できません").Wrap(err)
	}
	return nil
}

func trySend(ch chan<- callbackResult, r callbackResult) {
	select {
	case ch <- r:
	default:
	}
}

// readPasted は、Input から 1 行読み、貼り付けられた URL のクエリから認可コードを取り出す。
// Input の読み取りは中断できないため、Input が閉じられるか行が届くまで残ることがある。
func readPasted(opt usecase.LoginOptions, state string, results chan<- callbackResult) {
	if opt.Input == nil {
		return
	}
	line, err := bufio.NewReader(opt.Input).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		if err != nil {
			trySend(results, callbackResult{err: domain.Errorf(domain.KindSetup, "貼り付けた内容を読めませんでした")})
		}
		return
	}
	u, perr := url.Parse(line)
	if perr != nil {
		trySend(results, callbackResult{err: domain.Errorf(domain.KindSetup, "貼り付けた内容が URL ではありません").Wrap(perr)})
		return
	}
	code, ok, cerr := parseCallback(u.Query(), state)
	switch {
	case cerr != nil:
		trySend(results, callbackResult{err: cerr})
	case !ok:
		trySend(results, callbackResult{err: domain.Errorf(domain.KindSetup, "貼り付けた URL に認可コードがありません")})
	default:
		trySend(results, callbackResult{code: code})
	}
}

// Logout は、Google 側でトークンを取り消してから token.json を消す。
func (a *Auth) Logout(ctx context.Context) error {
	tok, err := a.loadToken()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var revokeErr error
	if err != nil {
		revokeErr = err
	} else {
		revokeErr = a.revoke(ctx, tok)
	}
	if rmErr := os.Remove(a.tokenPath()); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		return domain.Errorf(domain.KindUnexpected, "token.json を消せません").Wrap(rmErr)
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
