package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

const testClientSecret = `{"installed":{"client_id":"test-client-id.apps.googleusercontent.com","client_secret":"test-client-secret","auth_uri":"https://accounts.example/auth","token_uri":"https://accounts.example/token","redirect_uris":["http://localhost"]}}`

var _ usecase.Authenticator = (*Auth)(nil)

type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// memKeys は、メモリ上の偽の KeyStore。本物のキーチェーンには触れない。
type memKeys struct {
	mu      sync.Mutex
	kind    string
	key     []byte
	loadErr error
	loads   int
	saves   int
}

func (m *memKeys) Kind() string { return m.kind }

func (m *memKeys) Load() ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loads++
	if m.loadErr != nil {
		return nil, false, m.loadErr
	}
	if m.key == nil {
		return nil, false, nil
	}
	return append([]byte(nil), m.key...), true, nil
}

func (m *memKeys) Save(key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves++
	m.key = append([]byte(nil), key...)
	return nil
}

func (m *memKeys) savedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saves
}

func newMemKeys() *memKeys { return &memKeys{kind: "os"} }

// fakeGoogle は、偽のトークンエンドポイントと取り消しエンドポイント。
type fakeGoogle struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	forms   []url.Values
	revoked []string
}

func newFakeGoogle(t *testing.T, tokenCheck func(url.Values) (status int, body string)) *fakeGoogle {
	t.Helper()
	g := &fakeGoogle{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		g.mu.Lock()
		g.forms = append(g.forms, r.PostForm)
		g.mu.Unlock()
		status, body := tokenCheck(r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	mux.HandleFunc("/revoke", func(_ http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		g.mu.Lock()
		g.revoked = append(g.revoked, r.PostForm.Get("token"))
		g.mu.Unlock()
	})
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGoogle) auth(t *testing.T) *Auth {
	t.Helper()
	dir := t.TempDir()
	return &Auth{
		Dir:          dir,
		Keys:         newMemKeys(),
		OpenBrowser:  func(string) error { return nil },
		Endpoint:     oauth2.Endpoint{AuthURL: "https://accounts.example/auth", TokenURL: g.srv.URL + "/token"},
		RevokeURL:    g.srv.URL + "/revoke",
		LoginTimeout: 5 * time.Second,
	}
}

// testKey は、a の偽の KeyStore に鍵がなければ作って返す。
func testKey(t *testing.T, a *Auth) []byte {
	t.Helper()
	m := a.Keys.(*memKeys)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.key == nil {
		k, err := newKey()
		if err != nil {
			t.Fatal(err)
		}
		m.key = k
	}
	return append([]byte(nil), m.key...)
}

func writeSealedFile(t *testing.T, a *Auth, name, kind string, plain []byte) {
	t.Helper()
	sealed, err := sealSecret(testKey(t, a), kind, plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.Dir, name), sealed, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeClientSecret(t *testing.T, a *Auth) {
	t.Helper()
	writeSealedFile(t, a, "client_secret.enc", secretKindClientSecret, []byte(testClientSecret))
}

func writeToken(t *testing.T, a *Auth, tok *oauth2.Token) {
	t.Helper()
	data, err := json.Marshal(tok) //nolint:gosec // テストの偽トークン
	if err != nil {
		t.Fatal(err)
	}
	writeSealedFile(t, a, "token.enc", secretKindToken, data)
}

func readToken(t *testing.T, a *Auth) string {
	t.Helper()
	sealed, err := os.ReadFile(filepath.Join(a.Dir, "token.enc"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := openSecret(testKey(t, a), secretKindToken, sealed)
	if err != nil {
		t.Fatal(err)
	}
	return string(plain)
}

func writeLegacy(t *testing.T, a *Auth, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(a.Dir, name), []byte(`{"legacy":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

const okToken = `{"access_token":"a1","refresh_token":"r1","token_type":"Bearer","expires_in":3600}`

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func httpGet(t *testing.T, u string) {
	t.Helper()
	resp, err := http.Get(u) //nolint:gosec,noctx // テスト内の 127.0.0.1 宛て
	if err != nil {
		t.Errorf("GET %s: %v", u, err)
		return
	}
	_ = resp.Body.Close()
}

func TestAuthLogin(t *testing.T) {
	t.Run("ループバックでログインできる", func(t *testing.T) {
		// Arrange
		var challenge string
		var cmu sync.Mutex
		g := newFakeGoogle(t, func(f url.Values) (int, string) {
			cmu.Lock()
			defer cmu.Unlock()
			if f.Get("grant_type") != "authorization_code" || f.Get("code") != "c1" ||
				challengeOf(f.Get("code_verifier")) != challenge {
				return http.StatusBadRequest, `{"error":"invalid_grant"}`
			}
			return http.StatusOK, okToken
		})
		a := g.auth(t)
		writeClientSecret(t, a)
		var browseErr []string
		a.OpenBrowser = func(raw string) error {
			u, err := url.Parse(raw)
			if err != nil {
				return err
			}
			q := u.Query()
			cmu.Lock()
			challenge = q.Get("code_challenge")
			cmu.Unlock()
			if q.Get("code_challenge_method") != "S256" {
				browseErr = append(browseErr, "code_challenge_method")
			}
			if q.Get("access_type") != "offline" {
				browseErr = append(browseErr, "access_type")
			}
			if q.Get("scope") != "https://www.googleapis.com/auth/drive.file" {
				browseErr = append(browseErr, "scope")
			}
			if !strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") {
				browseErr = append(browseErr, "redirect_uri")
			}
			httpGet(t, q.Get("redirect_uri")+"?code=c1&state="+url.QueryEscape(q.Get("state")))
			return nil
		}
		prompt := &lockedBuffer{}

		// Act
		err := a.Login(context.Background(), usecase.LoginOptions{Prompt: prompt})
		// Assert
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if len(browseErr) > 0 {
			t.Errorf("URL の不備: %v", browseErr)
		}
		info, statErr := os.Stat(filepath.Join(a.Dir, "token.enc"))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("perm = %v", info.Mode().Perm())
		}
		if body := readToken(t, a); !strings.Contains(body, "r1") {
			t.Errorf("token = %s", body)
		}
		if !strings.Contains(prompt.String(), "ブラウザで次の URL を開いてログインしてください:") {
			t.Errorf("prompt = %q", prompt.String())
		}
	})

	t.Run("state が違うとログインできず、待ち受けも閉じる", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		writeClientSecret(t, a)
		var redirect string
		a.OpenBrowser = func(raw string) error {
			u, _ := url.Parse(raw)
			redirect = u.Query().Get("redirect_uri")
			httpGet(t, redirect+"?code=c1&state=wrong")
			return nil
		}

		// Act
		err := a.Login(context.Background(), usecase.LoginOptions{Prompt: io.Discard})

		// Assert
		if domain.KindOf(err) != domain.KindSetup {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
		if a.HasToken() {
			t.Error("token.enc ができている")
		}
		u, _ := url.Parse(redirect)
		if conn, dialErr := net.DialTimeout("tcp", u.Host, time.Second); dialErr == nil {
			_ = conn.Close()
			t.Error("待ち受けが閉じていない")
		}
	})

	t.Run("error パラメータがあると準備が必要", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		writeClientSecret(t, a)
		a.OpenBrowser = func(raw string) error {
			u, _ := url.Parse(raw)
			q := u.Query()
			httpGet(t, q.Get("redirect_uri")+"?error=access_denied&state="+url.QueryEscape(q.Get("state")))
			return nil
		}

		// Act
		err := a.Login(context.Background(), usecase.LoginOptions{Prompt: io.Discard})

		// Assert
		if domain.KindOf(err) != domain.KindSetup {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
	})

	t.Run("時間切れなら準備が必要", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		writeClientSecret(t, a)
		a.LoginTimeout = 50 * time.Millisecond

		// Act
		err := a.Login(context.Background(), usecase.LoginOptions{Prompt: io.Discard})

		// Assert
		if domain.KindOf(err) != domain.KindSetup {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
	})

	t.Run("ctx を取り消すと止まる", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		writeClientSecret(t, a)
		ctx, cancel := context.WithCancel(context.Background())
		a.OpenBrowser = func(string) error { cancel(); return nil }

		// Act
		err := a.Login(ctx, usecase.LoginOptions{Prompt: io.Discard})

		// Assert
		if err == nil {
			t.Fatal("エラーになるはず")
		}
		if a.HasToken() {
			t.Error("token.enc ができている")
		}
	})

	t.Run("--no-browser で貼り付けた URL からログインできる", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(f url.Values) (int, string) {
			if f.Get("code") != "c2" {
				return http.StatusBadRequest, `{"error":"invalid_grant"}`
			}
			return http.StatusOK, okToken
		})
		a := g.auth(t)
		writeClientSecret(t, a)
		a.OpenBrowser = func(string) error {
			t.Error("--no-browser なのにブラウザを開いた")
			return nil
		}
		prompt := &lockedBuffer{}
		pr, pw := io.Pipe()
		t.Cleanup(func() { _ = pw.Close() })
		done := make(chan error, 1)

		// Act
		go func() {
			done <- a.Login(context.Background(), usecase.LoginOptions{NoBrowser: true, Prompt: prompt, Input: pr})
		}()
		var state string
		deadline := time.Now().Add(5 * time.Second)
		for state == "" && time.Now().Before(deadline) {
			for _, f := range strings.Fields(prompt.String()) {
				if u, err := url.Parse(f); err == nil && u.Query().Get("state") != "" {
					state = u.Query().Get("state")
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		if state == "" {
			t.Fatalf("Prompt に URL がない: %q", prompt.String())
		}
		_, _ = io.WriteString(pw, "http://127.0.0.1:9/?code=c2&state="+url.QueryEscape(state)+"\n")
		err := <-done
		// Assert
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if !a.HasToken() {
			t.Error("token.enc がない")
		}
	})

	t.Run("空行のあとに正しい URL を貼ってもログインできる", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(f url.Values) (int, string) {
			if f.Get("code") != "c2" {
				return http.StatusBadRequest, `{"error":"invalid_grant"}`
			}
			return http.StatusOK, okToken
		})
		a := g.auth(t)
		writeClientSecret(t, a)
		a.OpenBrowser = func(string) error {
			t.Error("--no-browser なのにブラウザを開いた")
			return nil
		}
		prompt := &lockedBuffer{}
		pr, pw := io.Pipe()
		t.Cleanup(func() { _ = pw.Close() })
		done := make(chan error, 1)

		// Act
		go func() {
			done <- a.Login(context.Background(), usecase.LoginOptions{NoBrowser: true, Prompt: prompt, Input: pr})
		}()
		var state string
		deadline := time.Now().Add(5 * time.Second)
		for state == "" && time.Now().Before(deadline) {
			for _, f := range strings.Fields(prompt.String()) {
				if u, err := url.Parse(f); err == nil && u.Query().Get("state") != "" {
					state = u.Query().Get("state")
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		if state == "" {
			t.Fatalf("Prompt に URL がない: %q", prompt.String())
		}
		_, _ = io.WriteString(pw, "\n  \n")
		_, _ = io.WriteString(pw, "http://127.0.0.1:9/?code=c2&state="+url.QueryEscape(state)+"\n")
		err := <-done
		// Assert
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if !a.HasToken() {
			t.Error("token.enc がない")
		}
	})

	t.Run("URL でない行のあとに正しい URL を貼ってもログインできる", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(f url.Values) (int, string) {
			if f.Get("code") != "c2" {
				return http.StatusBadRequest, `{"error":"invalid_grant"}`
			}
			return http.StatusOK, okToken
		})
		a := g.auth(t)
		writeClientSecret(t, a)
		a.OpenBrowser = func(string) error {
			t.Error("--no-browser なのにブラウザを開いた")
			return nil
		}
		prompt := &lockedBuffer{}
		pr, pw := io.Pipe()
		t.Cleanup(func() { _ = pw.Close() })
		done := make(chan error, 1)

		// Act
		go func() {
			done <- a.Login(context.Background(), usecase.LoginOptions{NoBrowser: true, Prompt: prompt, Input: pr})
		}()
		var state string
		deadline := time.Now().Add(5 * time.Second)
		for state == "" && time.Now().Before(deadline) {
			for _, f := range strings.Fields(prompt.String()) {
				if u, err := url.Parse(f); err == nil && u.Query().Get("state") != "" {
					state = u.Query().Get("state")
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		if state == "" {
			t.Fatalf("Prompt に URL がない: %q", prompt.String())
		}
		_, _ = io.WriteString(pw, "garbage\nhttp://127.0.0.1:9/?nothing=1\n")
		_, _ = io.WriteString(pw, "http://127.0.0.1:9/?code=c2&state="+url.QueryEscape(state)+"\n")
		err := <-done
		// Assert
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if !a.HasToken() {
			t.Error("token.enc がない")
		}
	})

	t.Run("client_secret がなければ準備が必要", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)

		// Act
		err := a.Login(context.Background(), usecase.LoginOptions{Prompt: io.Discard})

		// Assert
		if domain.KindOf(err) != domain.KindSetup {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
	})
}

func TestAuthImportClientSecret(t *testing.T) {
	t.Run("壊れた JSON は引数の誤り", func(t *testing.T) {
		// Arrange
		a := &Auth{Dir: t.TempDir(), Keys: newMemKeys()}
		src := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(src, []byte(`{not json`), 0o600); err != nil {
			t.Fatal(err)
		}

		// Act
		err := a.ImportClientSecret(src)

		// Assert
		if domain.KindOf(err) != domain.KindUsage {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
		if a.HasClientSecret() {
			t.Error("コピーされている")
		}
	})

	t.Run("読めないパスは引数の誤り", func(t *testing.T) {
		// Arrange
		a := &Auth{Dir: t.TempDir(), Keys: newMemKeys()}

		// Act
		err := a.ImportClientSecret(filepath.Join(t.TempDir(), "none.json"))

		// Assert
		if domain.KindOf(err) != domain.KindUsage {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
	})

	t.Run("正しい installed 形式なら暗号化して 0600 で置く", func(t *testing.T) {
		// Arrange
		a := &Auth{Dir: t.TempDir(), Keys: newMemKeys()}
		src := filepath.Join(t.TempDir(), "cs.json")
		if err := os.WriteFile(src, []byte(testClientSecret), 0o644); err != nil { //nolint:gosec // 元ファイルは緩い権限でよい
			t.Fatal(err)
		}

		// Act
		err := a.ImportClientSecret(src)
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(a.Dir, "client_secret.enc")
		info, statErr := os.Stat(dst)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("perm = %v", info.Mode().Perm())
		}
		body, _ := os.ReadFile(dst) //nolint:gosec // テストの一時ファイル
		plain, openErr := openSecret(testKey(t, a), secretKindClientSecret, body)
		if openErr != nil || string(plain) != testClientSecret {
			t.Errorf("復号 = %s, err = %v", plain, openErr)
		}
		if !a.HasClientSecret() {
			t.Error("HasClientSecret = false")
		}
	})
}

func TestAuthTokenSource(t *testing.T) {
	t.Run("トークンも client_secret もなければ準備が必要", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)

		// Act
		_, err := a.TokenSource(context.Background())

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "gdrive-cli login") {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
	})

	t.Run("更新したトークンを保存し直す", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(f url.Values) (int, string) {
			if f.Get("grant_type") != "refresh_token" || f.Get("refresh_token") != "r1" {
				return http.StatusBadRequest, `{"error":"invalid_grant"}`
			}
			return http.StatusOK, `{"access_token":"a2","token_type":"Bearer","expires_in":3600}`
		})
		a := g.auth(t)
		writeClientSecret(t, a)
		writeToken(t, a, &oauth2.Token{
			AccessToken: "a0", RefreshToken: "r1", TokenType: "Bearer",
			Expiry: time.Now().Add(-time.Hour),
		})

		// Act
		ts, err := a.TokenSource(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		tok, tokErr := ts.Token()

		// Assert
		if tokErr != nil {
			t.Fatal(tokErr)
		}
		if tok.AccessToken != "a2" {
			t.Errorf("AccessToken = %q", tok.AccessToken)
		}
		if body := readToken(t, a); !strings.Contains(body, "a2") || !strings.Contains(body, "r1") {
			t.Errorf("token = %s", body)
		}
	})
}

func TestAuthLogout(t *testing.T) {
	t.Run("取り消してからトークンを消す", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		writeToken(t, a, &oauth2.Token{AccessToken: "a1", RefreshToken: "r1"})

		// Act
		err := a.Logout(context.Background())
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		g.mu.Lock()
		revoked := append([]string(nil), g.revoked...)
		g.mu.Unlock()
		if len(revoked) != 1 || revoked[0] != "r1" {
			t.Errorf("revoked = %v", revoked)
		}
		if a.HasToken() {
			t.Error("token.enc が残っている")
		}
	})

	t.Run("取り消しに失敗してもファイルは消し、エラーを返す", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		a.RevokeURL = g.srv.URL + "/missing"
		writeToken(t, a, &oauth2.Token{AccessToken: "a1", RefreshToken: "r1"})

		// Act
		err := a.Logout(context.Background())

		// Assert
		if domain.KindOf(err) != domain.KindUnexpected {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
		if a.HasToken() {
			t.Error("token.enc が残っている")
		}
	})

	t.Run("トークンがなければ何もしない", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)

		// Act
		err := a.Logout(context.Background())

		// Assert
		g.mu.Lock()
		n := len(g.revoked)
		g.mu.Unlock()
		if err != nil || n != 0 {
			t.Errorf("err = %v, revoked = %d", err, n)
		}
	})
}

func writeSrcSecret(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "cs.json")
	if err := os.WriteFile(src, []byte(testClientSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	return src
}

func TestAuthKeyCreation(t *testing.T) {
	t.Run("取り込むと client_secret.enc が暗号化されて置かれ、平文が含まれない", func(t *testing.T) {
		// Arrange
		a := &Auth{Dir: t.TempDir(), Keys: newMemKeys()}

		// Act
		err := a.ImportClientSecret(writeSrcSecret(t))
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		body, _ := os.ReadFile(filepath.Join(a.Dir, "client_secret.enc"))
		if bytes.Contains(body, []byte("test-client-id")) || bytes.Contains(body, []byte("test-client-secret")) {
			t.Errorf("平文が含まれる: %s", body)
		}
	})

	t.Run("鍵がなければ取り込みで鍵を作る", func(t *testing.T) {
		// Arrange
		keys := newMemKeys()
		a := &Auth{Dir: t.TempDir(), Keys: keys}

		// Act
		err := a.ImportClientSecret(writeSrcSecret(t))
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if keys.savedCount() != 1 || len(keys.key) != keyLength {
			t.Errorf("saves = %d, key = %d バイト", keys.savedCount(), len(keys.key))
		}
	})

	t.Run("鍵が既にあれば取り込みでも作り直さない", func(t *testing.T) {
		// Arrange
		keys := newMemKeys()
		a := &Auth{Dir: t.TempDir(), Keys: keys}
		testKey(t, a)

		// Act
		err := a.ImportClientSecret(writeSrcSecret(t))

		// Assert
		if err != nil || keys.savedCount() != 0 {
			t.Errorf("err = %v, saves = %d", err, keys.savedCount())
		}
	})

	t.Run("鍵を新しく作ったときは古い token.enc を消す", func(t *testing.T) {
		// Arrange
		a := &Auth{Dir: t.TempDir(), Keys: newMemKeys()}
		if err := os.WriteFile(filepath.Join(a.Dir, "token.enc"), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Act
		err := a.ImportClientSecret(writeSrcSecret(t))
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if a.HasToken() {
			t.Error("token.enc が残っている")
		}
	})

	t.Run("キーチェーンが使えなければ取り込みは KindSetup で、鍵を作らない", func(t *testing.T) {
		// Arrange
		keys := newMemKeys()
		keys.loadErr = domain.Errorf(domain.KindSetup, "キーチェーンが使えません")
		a := &Auth{Dir: t.TempDir(), Keys: keys}

		// Act
		err := a.ImportClientSecret(writeSrcSecret(t))

		// Assert
		if domain.KindOf(err) != domain.KindSetup {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
		if keys.savedCount() != 0 {
			t.Error("鍵を作ってしまった")
		}
		if a.HasClientSecret() {
			t.Error("client_secret.enc ができている")
		}
	})

	t.Run("鍵を作るときはロックを取る", func(t *testing.T) {
		// Arrange
		var locked, unlocked int
		a := &Auth{Dir: t.TempDir(), Keys: newMemKeys(), Lock: func(context.Context) (func(), error) {
			locked++
			return func() { unlocked++ }, nil
		}}

		// Act
		err := a.ImportClientSecret(writeSrcSecret(t))
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if locked != 1 || unlocked != 1 {
			t.Errorf("locked = %d, unlocked = %d", locked, unlocked)
		}
	})

	t.Run("ロックを取れなければ鍵を作らない", func(t *testing.T) {
		// Arrange
		keys := newMemKeys()
		a := &Auth{Dir: t.TempDir(), Keys: keys, Lock: func(context.Context) (func(), error) {
			return nil, errors.New("取れない")
		}}

		// Act
		err := a.ImportClientSecret(writeSrcSecret(t))

		// Assert
		if err == nil || keys.savedCount() != 0 {
			t.Errorf("err = %v, saves = %d", err, keys.savedCount())
		}
	})
}

func TestAuthEncryptedToken(t *testing.T) {
	t.Run("ログインのあと token.enc は暗号化されていて、平文のアクセストークンを含まない", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		writeClientSecret(t, a)
		a.OpenBrowser = func(raw string) error {
			u, _ := url.Parse(raw)
			q := u.Query()
			httpGet(t, q.Get("redirect_uri")+"?code=c1&state="+url.QueryEscape(q.Get("state")))
			return nil
		}

		// Act
		err := a.Login(context.Background(), usecase.LoginOptions{Prompt: io.Discard})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		body, _ := os.ReadFile(filepath.Join(a.Dir, "token.enc"))
		if bytes.Contains(body, []byte("a1")) || bytes.Contains(body, []byte("r1")) {
			t.Errorf("平文が含まれる: %s", body)
		}
	})

	t.Run("ログインに成功したら古い平文のファイルを消して、そのことを表示する", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		writeClientSecret(t, a)
		writeLegacy(t, a, "token.json")
		writeLegacy(t, a, "client_secret.json")
		a.OpenBrowser = func(raw string) error {
			u, _ := url.Parse(raw)
			q := u.Query()
			httpGet(t, q.Get("redirect_uri")+"?code=c1&state="+url.QueryEscape(q.Get("state")))
			return nil
		}
		prompt := &lockedBuffer{}

		// Act
		err := a.Login(context.Background(), usecase.LoginOptions{Prompt: prompt})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"token.json", "client_secret.json"} {
			if fileExists(filepath.Join(a.Dir, name)) {
				t.Errorf("%s が残っている", name)
			}
			if !strings.Contains(prompt.String(), "古い形式のファイルを消しました: "+name) {
				t.Errorf("%s の表示がない: %q", name, prompt.String())
			}
		}
	})

	t.Run("トークンの更新の保存では鍵を作らない", func(t *testing.T) {
		// Arrange
		keys := newMemKeys()
		a := &Auth{Dir: t.TempDir(), Keys: keys}

		// Act
		err := a.saveToken(&oauth2.Token{AccessToken: "a2", RefreshToken: "r1"})

		// Assert
		if domain.KindOf(err) != domain.KindSetup {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
		if keys.savedCount() != 0 || a.HasToken() {
			t.Errorf("saves = %d, HasToken = %v", keys.savedCount(), a.HasToken())
		}
	})

	t.Run("更新のあとの保存で鍵が消えていたら KindSetup", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) {
			return http.StatusOK, `{"access_token":"a2","token_type":"Bearer","expires_in":3600}`
		})
		a := g.auth(t)
		writeClientSecret(t, a)
		writeToken(t, a, &oauth2.Token{AccessToken: "a0", RefreshToken: "r1", Expiry: time.Now().Add(-time.Hour)})
		cfg, err := a.config()
		if err != nil {
			t.Fatal(err)
		}
		tok, err := a.loadToken()
		if err != nil {
			t.Fatal(err)
		}
		other := &Auth{Dir: a.Dir, Keys: newMemKeys()} // 鍵のない別の Auth
		src := &savingSource{inner: cfg.TokenSource(context.Background(), tok), last: tok.AccessToken, save: other.saveToken}

		// Act
		_, tokErr := src.Token()

		// Assert
		if domain.KindOf(tokErr) != domain.KindSetup {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(tokErr), tokErr)
		}
	})

	t.Run("古い平文のファイルだけなら TokenSource は KindSetup で古い形式の文言", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		writeLegacy(t, a, "token.json")
		writeLegacy(t, a, "client_secret.json")

		// Act
		_, err := a.TokenSource(context.Background())

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "古い形式のファイルがあります") {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
	})

	t.Run("鍵が合わなければ TokenSource は KindSetup で、合わない文言", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		writeClientSecret(t, a)
		writeToken(t, a, &oauth2.Token{AccessToken: "a1", RefreshToken: "r1"})
		other, _ := newKey()
		a.Keys.(*memKeys).key = other

		// Act
		_, err := a.TokenSource(context.Background())

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "今の鍵で読めません") {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
	})

	t.Run("鍵がなければ TokenSource は KindSetup で、鍵がない文言", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		writeClientSecret(t, a)
		writeToken(t, a, &oauth2.Token{AccessToken: "a1", RefreshToken: "r1"})
		a.Keys.(*memKeys).key = nil

		// Act
		_, err := a.TokenSource(context.Background())

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "鍵が見つかりません") {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
	})
}

func TestAuthInspect(t *testing.T) {
	const (
		noKey    = "鍵が見つかりません。`gdrive-cli login --client-secret <JSON>` をやり直してください"
		mismatch = "今の鍵で読めません。`gdrive-cli login --client-secret <JSON>` をやり直してください"
		legacy   = "古い形式のファイルがあります。`gdrive-cli login --client-secret <JSON>` をやり直してください"
	)
	tests := []struct {
		name    string
		setup   func(t *testing.T, a *Auth)
		want    usecase.AuthInspection
		wantErr domain.ErrorKind
		hasErr  bool
	}{
		{"何もない", func(*testing.T, *Auth) {}, usecase.AuthInspection{KeyStore: "os"}, 0, false},
		{"両方読める", func(t *testing.T, a *Auth) {
			writeClientSecret(t, a)
			writeToken(t, a, &oauth2.Token{AccessToken: "a1"})
		}, usecase.AuthInspection{KeyStore: "os", ClientSecret: true, Token: true}, 0, false},
		{"鍵がない", func(t *testing.T, a *Auth) {
			writeClientSecret(t, a)
			writeToken(t, a, &oauth2.Token{AccessToken: "a1"})
			a.Keys.(*memKeys).key = nil
		}, usecase.AuthInspection{KeyStore: "os", Problem: noKey}, 0, false},
		{"鍵が合わない", func(t *testing.T, a *Auth) {
			writeClientSecret(t, a)
			writeToken(t, a, &oauth2.Token{AccessToken: "a1"})
			other, _ := newKey()
			a.Keys.(*memKeys).key = other
		}, usecase.AuthInspection{KeyStore: "os", Problem: mismatch}, 0, false},
		{"古い形式だけ", func(t *testing.T, a *Auth) {
			writeLegacy(t, a, "token.json")
			writeLegacy(t, a, "client_secret.json")
		}, usecase.AuthInspection{KeyStore: "os", Problem: legacy}, 0, false},
		{"KeyStore が使えない", func(t *testing.T, a *Auth) {
			writeClientSecret(t, a)
			a.Keys.(*memKeys).loadErr = domain.Errorf(domain.KindSetup, "キーチェーンが使えません")
		}, usecase.AuthInspection{KeyStore: "os"}, domain.KindSetup, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			a := &Auth{Dir: t.TempDir(), Keys: newMemKeys()}
			tt.setup(t, a)

			// Act
			got, err := a.Inspect()

			// Assert
			if tt.hasErr {
				if domain.KindOf(err) != tt.wantErr {
					t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAuthLogoutEncrypted(t *testing.T) {
	t.Run("ログアウトは token.enc と古い token.json を消し、鍵は消さない", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		writeToken(t, a, &oauth2.Token{AccessToken: "a1", RefreshToken: "r1"})
		writeLegacy(t, a, "token.json")

		// Act
		err := a.Logout(context.Background())
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if a.HasToken() || fileExists(filepath.Join(a.Dir, "token.json")) {
			t.Error("トークンのファイルが残っている")
		}
		if a.Keys.(*memKeys).key == nil {
			t.Error("鍵が消えた")
		}
	})
}
