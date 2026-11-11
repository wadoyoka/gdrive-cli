package usecase

import (
	"context"
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
		u := Status{Dir: fakeDir{path: "/cfg"}, Auth: &fakeAuth{hasToken: true}, Remote: r}

		// Act
		got, err := u.Run(context.Background())

		// Assert
		want := StatusResult{ConfigDir: "/cfg", LoggedIn: true, Email: "user@example.com", RootFolderID: root.ID, RootFolderLink: root.WebViewLink}
		if err != nil || got != want {
			t.Errorf("got=%+v err=%v want=%+v", got, err, want)
		}
	})
}
