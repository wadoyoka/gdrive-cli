package usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

// fakeAuth は、呼ばれた順を覚える偽の Authenticator。
type fakeAuth struct {
	hasSecret bool
	hasToken  bool
	calls     []string
	opt       LoginOptions
	imported  string
	keyStore  string
	problem   string
	inspErr   error
}

func (a *fakeAuth) HasClientSecret() bool { return a.hasSecret }
func (a *fakeAuth) ImportClientSecret(path string) error {
	a.calls = append(a.calls, "import")
	a.imported = path
	a.hasSecret = true
	return nil
}

func (a *fakeAuth) Login(_ context.Context, opt LoginOptions) error {
	a.calls = append(a.calls, "login")
	a.opt = opt
	a.hasToken = true
	return nil
}

func (a *fakeAuth) Logout(context.Context) error {
	a.calls = append(a.calls, "logout")
	a.hasToken = false
	return nil
}
func (a *fakeAuth) HasToken() bool { return a.hasToken }

func (a *fakeAuth) Inspect() (AuthInspection, error) {
	return AuthInspection{KeyStore: a.keyStore, ClientSecret: a.hasSecret, Token: a.hasToken, Problem: a.problem}, a.inspErr
}

func newRemote(d Drive) Remote {
	return Remote{
		Open:   func(context.Context) (Drive, error) { return d, nil },
		State:  &fakeState{},
		Locker: &fakeLocker{},
	}
}

func TestLogin(t *testing.T) {
	t.Run("パスを渡すと取り込んでからログインする", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		a := &fakeAuth{}
		u := Login{Auth: a, Remote: newRemote(d)}

		// Act
		got, err := u.Run(context.Background(), LoginInput{ClientSecretPath: "/x.json", NoBrowser: true})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a.calls, []string{"import", "login"}) || a.imported != "/x.json" || !a.opt.NoBrowser {
			t.Errorf("calls=%v imported=%q opt=%+v", a.calls, a.imported, a.opt)
		}
		if got.Email != "user@example.com" || got.RootFolderID == "" || got.RootFolderLink == "" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("client_secret がなければ KindSetup", func(t *testing.T) {
		// Arrange
		a := &fakeAuth{}
		u := Login{Auth: a, Remote: newRemote(newFakeDrive(t))}

		// Act
		_, err := u.Run(context.Background(), LoginInput{})

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "--client-secret") {
			t.Errorf("err=%v", err)
		}
		if len(a.calls) != 0 {
			t.Errorf("calls=%v", a.calls)
		}
	})

	t.Run("login: --client-secret がなく、鍵が合わなければ、その文言で KindSetup", func(t *testing.T) {
		// Arrange
		a := &fakeAuth{problem: "鍵が合いません"}
		u := Login{Auth: a, Remote: newRemote(newFakeDrive(t))}

		// Act
		_, err := u.Run(context.Background(), LoginInput{})

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "鍵が合いません") {
			t.Errorf("err=%v", err)
		}
		if len(a.calls) != 0 {
			t.Errorf("calls=%v", a.calls)
		}
	})

	t.Run("login: --client-secret がなく、何もなければ、初回の案内で KindSetup", func(t *testing.T) {
		// Arrange
		u := Login{Auth: &fakeAuth{}, Remote: newRemote(newFakeDrive(t))}

		// Act
		_, err := u.Run(context.Background(), LoginInput{})

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "初回は --client-secret") {
			t.Errorf("err=%v", err)
		}
	})

	t.Run("login: 鍵の置き場所が使えなければ、そのエラーを返す", func(t *testing.T) {
		// Arrange
		want := domain.Errorf(domain.KindSetup, "キーチェーンが使えません")
		a := &fakeAuth{hasSecret: true, inspErr: want}
		u := Login{Auth: a, Remote: newRemote(newFakeDrive(t))}

		// Act
		_, err := u.Run(context.Background(), LoginInput{})

		// Assert
		if !errors.Is(err, want) || len(a.calls) != 0 {
			t.Errorf("err=%v calls=%v", err, a.calls)
		}
	})

	t.Run("取り込み済みなら、パスなしでログインできる", func(t *testing.T) {
		// Arrange
		a := &fakeAuth{hasSecret: true}
		u := Login{Auth: a, Remote: newRemote(newFakeDrive(t))}

		// Act
		_, err := u.Run(context.Background(), LoginInput{})

		// Assert
		if err != nil || !slices.Equal(a.calls, []string{"login"}) {
			t.Errorf("err=%v calls=%v", err, a.calls)
		}
	})
}

func TestLogout(t *testing.T) {
	t.Run("Auth の Logout を呼ぶ", func(t *testing.T) {
		// Arrange
		a := &fakeAuth{hasToken: true}

		// Act
		err := Logout{Auth: a}.Run(context.Background())

		// Assert
		if err != nil || a.hasToken || !slices.Equal(a.calls, []string{"logout"}) {
			t.Errorf("err=%v calls=%v", err, a.calls)
		}
	})
}

func TestStatus(t *testing.T) {
	t.Run("Status はログインしていなければ LoggedIn=false と KindSetup", func(t *testing.T) {
		// Arrange
		u := Status{Dir: fakeDir{path: "/cfg"}, Auth: &fakeAuth{}, Remote: newRemote(newFakeDrive(t))}

		// Act
		got, err := u.Run(context.Background())

		// Assert
		if domain.KindOf(err) != domain.KindSetup || got.LoggedIn || got.ConfigDir != "/cfg" {
			t.Errorf("got=%+v err=%v", got, err)
		}
	})

	t.Run("Status はログインしていればメールとルートを返す", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		root := d.addRootFolder()
		r := newRemote(d)
		r.State = &fakeState{id: root.ID}
		u := Status{Dir: fakeDir{path: "/cfg"}, Auth: &fakeAuth{hasToken: true, keyStore: "os"}, Remote: r}

		// Act
		got, err := u.Run(context.Background())

		// Assert
		want := StatusResult{ConfigDir: "/cfg", LoggedIn: true, KeyStore: "os", Email: "user@example.com", RootFolderID: root.ID, RootFolderLink: root.WebViewLink}
		if err != nil || got != want {
			t.Errorf("got=%+v err=%v want=%+v", got, err, want)
		}
	})

	t.Run("status: 鍵の置き場所を結果に入れる", func(t *testing.T) {
		// Arrange
		u := Status{Dir: fakeDir{path: "/cfg"}, Auth: &fakeAuth{keyStore: "file"}, Remote: newRemote(newFakeDrive(t))}

		// Act
		got, err := u.Run(context.Background())

		// Assert
		if domain.KindOf(err) != domain.KindSetup || got.KeyStore != "file" || got.LoggedIn || got.Problem != "" {
			t.Errorf("got=%+v err=%v", got, err)
		}
		if !strings.Contains(err.Error(), "ログインしていません") {
			t.Errorf("err=%v", err)
		}
	})

	t.Run("status: 鍵が合わなければ LoggedIn=false、Problem 付きで KindSetup", func(t *testing.T) {
		// Arrange
		u := Status{Dir: fakeDir{path: "/cfg"}, Auth: &fakeAuth{keyStore: "os", problem: "鍵が合いません"}, Remote: newRemote(newFakeDrive(t))}

		// Act
		got, err := u.Run(context.Background())

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "鍵が合いません") {
			t.Errorf("err=%v", err)
		}
		if got.LoggedIn || got.Problem != "鍵が合いません" || got.KeyStore != "os" {
			t.Errorf("got=%+v", got)
		}
	})

	t.Run("status: 鍵の置き場所が使えなくても KeyStore は入る", func(t *testing.T) {
		// Arrange
		want := domain.Errorf(domain.KindSetup, "キーチェーンが使えません")
		u := Status{Dir: fakeDir{path: "/cfg"}, Auth: &fakeAuth{keyStore: "os", inspErr: want}, Remote: newRemote(newFakeDrive(t))}

		// Act
		got, err := u.Run(context.Background())

		// Assert
		if !errors.Is(err, want) || got.KeyStore != "os" || got.LoggedIn || got.ConfigDir != "/cfg" {
			t.Errorf("got=%+v err=%v", got, err)
		}
	})
}
