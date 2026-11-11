package repository

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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
		OpenBrowser:  func(string) error { return nil },
		Endpoint:     oauth2.Endpoint{AuthURL: "https://accounts.example/auth", TokenURL: g.srv.URL + "/token"},
		RevokeURL:    g.srv.URL + "/revoke",
		LoginTimeout: 5 * time.Second,
	}
}

func writeClientSecret(t *testing.T, a *Auth) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(a.Dir, "client_secret.json"), []byte(testClientSecret), 0o600); err != nil {
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
		info, statErr := os.Stat(filepath.Join(a.Dir, "token.json"))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("perm = %v", info.Mode().Perm())
		}
		body, _ := os.ReadFile(filepath.Join(a.Dir, "token.json"))
		if !strings.Contains(string(body), "r1") {
			t.Errorf("token.json = %s", body)
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
			t.Error("token.json ができている")
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
			t.Error("token.json ができている")
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
			t.Error("token.json がない")
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
			t.Error("token.json がない")
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
			t.Error("token.json がない")
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
		a := &Auth{Dir: t.TempDir()}
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
		a := &Auth{Dir: t.TempDir()}

		// Act
		err := a.ImportClientSecret(filepath.Join(t.TempDir(), "none.json"))

		// Assert
		if domain.KindOf(err) != domain.KindUsage {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
	})

	t.Run("正しい installed 形式なら 0600 でコピーする", func(t *testing.T) {
		// Arrange
		a := &Auth{Dir: t.TempDir()}
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
		dst := filepath.Join(a.Dir, "client_secret.json")
		info, statErr := os.Stat(dst)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("perm = %v", info.Mode().Perm())
		}
		body, _ := os.ReadFile(dst) //nolint:gosec // テストの一時ファイル
		if string(body) != testClientSecret {
			t.Errorf("body = %s", body)
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
		expired, _ := json.Marshal(&oauth2.Token{ //nolint:gosec // テストの偽トークン
			AccessToken: "a0", RefreshToken: "r1", TokenType: "Bearer",
			Expiry: time.Now().Add(-time.Hour),
		})
		if err := os.WriteFile(filepath.Join(a.Dir, "token.json"), expired, 0o600); err != nil {
			t.Fatal(err)
		}

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
		body, _ := os.ReadFile(filepath.Join(a.Dir, "token.json"))
		if !strings.Contains(string(body), "a2") || !strings.Contains(string(body), "r1") {
			t.Errorf("token.json = %s", body)
		}
	})
}

func TestAuthLogout(t *testing.T) {
	t.Run("取り消してからトークンを消す", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		tokenJSON, _ := json.Marshal(&oauth2.Token{AccessToken: "a1", RefreshToken: "r1"}) //nolint:gosec // テストの偽トークン
		if err := os.WriteFile(filepath.Join(a.Dir, "token.json"), tokenJSON, 0o600); err != nil {
			t.Fatal(err)
		}

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
			t.Error("token.json が残っている")
		}
	})

	t.Run("取り消しに失敗してもファイルは消し、エラーを返す", func(t *testing.T) {
		// Arrange
		g := newFakeGoogle(t, func(url.Values) (int, string) { return http.StatusOK, okToken })
		a := g.auth(t)
		a.RevokeURL = g.srv.URL + "/missing"
		tokenJSON, _ := json.Marshal(&oauth2.Token{AccessToken: "a1", RefreshToken: "r1"}) //nolint:gosec // テストの偽トークン
		if err := os.WriteFile(filepath.Join(a.Dir, "token.json"), tokenJSON, 0o600); err != nil {
			t.Fatal(err)
		}

		// Act
		err := a.Logout(context.Background())

		// Assert
		if domain.KindOf(err) != domain.KindUnexpected {
			t.Fatalf("kind = %v, err = %v", domain.KindOf(err), err)
		}
		if a.HasToken() {
			t.Error("token.json が残っている")
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
