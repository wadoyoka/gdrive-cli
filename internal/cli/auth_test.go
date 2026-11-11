package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

type stubAuth struct{ token bool }

func (*stubAuth) HasClientSecret() bool                             { return true }
func (*stubAuth) ImportClientSecret(string) error                   { return nil }
func (*stubAuth) Login(context.Context, usecase.LoginOptions) error { return nil }
func (s *stubAuth) Logout(context.Context) error                    { s.token = false; return nil }
func (s *stubAuth) HasToken() bool                                  { return s.token }

// stubDrive は、status に必要な 2 つだけを持つ偽の Drive。
type stubDrive struct{ usecase.Drive }

func (stubDrive) AccountEmail(context.Context) (string, error) { return "user@example.com", nil }
func (stubDrive) Get(context.Context, string) (domain.Entry, error) {
	return domain.Entry{ID: "root-1", Name: "gdrive-cli", Type: domain.TypeFolder, WebViewLink: "https://example.com/root-1"}, nil
}

type stubState struct{}

func (stubState) RootFolderID() (string, error) { return "root-1", nil }
func (stubState) SetRootFolderID(string) error  { return nil }

type stubLocker struct{}

func (stubLocker) Lock(context.Context) (func(), error) { return func() {}, nil }

func withAuth(d Deps, loggedIn bool) Deps {
	a := &stubAuth{token: loggedIn}
	r := usecase.Remote{
		Open:   func(context.Context) (usecase.Drive, error) { return stubDrive{}, nil },
		State:  stubState{},
		Locker: stubLocker{},
	}
	d.Login = usecase.Login{Auth: a, Remote: r}
	d.Logout = usecase.Logout{Auth: a}
	d.Status = usecase.Status{Dir: d.Init.Dir, Auth: a, Remote: r}
	return d
}

func initAndClear(t *testing.T, d Deps, out, errb *bytes.Buffer) {
	t.Helper()
	Execute(context.Background(), d, []string{"init"})
	out.Reset()
	errb.Reset()
}

func TestStatusCommand(t *testing.T) {
	t.Run("status --json", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withAuth(d, true)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"--json", "status"})

		// Assert
		var got map[string]any
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("json: %v: %q", err, out.String())
		}
		if code != 0 || got["loggedIn"] != true || got["email"] != "user@example.com" ||
			got["rootFolderId"] != "root-1" || got["rootFolderLink"] != "https://example.com/root-1" || got["configDir"] == "" {
			t.Errorf("code=%d got=%v", code, got)
		}
	})

	t.Run("ログインしていない status", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withAuth(d, false)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"--json", "status"})

		// Assert
		if code != 3 || !strings.Contains(out.String(), `"loggedIn":false`) {
			t.Errorf("code=%d out=%q", code, out.String())
		}
	})

	t.Run("拒否リストで login を拒否すると 7", func(t *testing.T) {
		// Arrange
		d, out, errb, dir := newDeps(t)
		d = withAuth(d, false)
		initAndClear(t, d, out, errb)
		if err := os.WriteFile(filepath.Join(dir, "policy.toml"), []byte(`deny = ["login"]`), 0o600); err != nil {
			t.Fatal(err)
		}

		// Act
		code := Execute(context.Background(), d, []string{"login"})

		// Assert
		if code != 7 {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("login に余計な引数を渡すと 2", func(t *testing.T) {
		// Arrange
		d, _, _, _ := newDeps(t)
		d = withAuth(d, false)

		// Act
		code := Execute(context.Background(), d, []string{"login", "x"})

		// Assert
		if code != 2 {
			t.Errorf("code=%d", code)
		}
	})

	t.Run("logout は 0 を返す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withAuth(d, true)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"logout"})

		// Assert
		if code != 0 || !strings.Contains(out.String(), "ログアウト") {
			t.Errorf("code=%d out=%q", code, out.String())
		}
	})
}
