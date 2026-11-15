package account

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/usecasetest"
)

func TestLogin(t *testing.T) {
	t.Run("パスを渡すと取り込んでからログインする", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		a := &usecasetest.Auth{}
		u := Login{Auth: a, Remote: usecasetest.NewRemote(d)}

		// Act
		got, err := u.Run(context.Background(), LoginInput{ClientSecretPath: "/x.json", NoBrowser: true})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a.Calls, []string{"import", "login"}) || a.Imported != "/x.json" || !a.Opt.NoBrowser {
			t.Errorf("calls=%v imported=%q opt=%+v", a.Calls, a.Imported, a.Opt)
		}
		if got.Email != "user@example.com" || got.RootFolderID == "" || got.RootFolderLink == "" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("client_secret がなければ KindSetup", func(t *testing.T) {
		// Arrange
		a := &usecasetest.Auth{}
		u := Login{Auth: a, Remote: usecasetest.NewRemote(usecasetest.NewDrive(t))}

		// Act
		_, err := u.Run(context.Background(), LoginInput{})

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "--client-secret") {
			t.Errorf("err=%v", err)
		}
		if len(a.Calls) != 0 {
			t.Errorf("calls=%v", a.Calls)
		}
	})

	t.Run("login: --client-secret がなく、鍵が合わなければ、その文言で KindSetup", func(t *testing.T) {
		// Arrange
		a := &usecasetest.Auth{Problem: "鍵が合いません"}
		u := Login{Auth: a, Remote: usecasetest.NewRemote(usecasetest.NewDrive(t))}

		// Act
		_, err := u.Run(context.Background(), LoginInput{})

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "鍵が合いません") {
			t.Errorf("err=%v", err)
		}
		if len(a.Calls) != 0 {
			t.Errorf("calls=%v", a.Calls)
		}
	})

	t.Run("login: --client-secret がなく、何もなければ、初回の案内で KindSetup", func(t *testing.T) {
		// Arrange
		u := Login{Auth: &usecasetest.Auth{}, Remote: usecasetest.NewRemote(usecasetest.NewDrive(t))}

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
		a := &usecasetest.Auth{SecretSaved: true, InspErr: want}
		u := Login{Auth: a, Remote: usecasetest.NewRemote(usecasetest.NewDrive(t))}

		// Act
		_, err := u.Run(context.Background(), LoginInput{})

		// Assert
		if !errors.Is(err, want) || len(a.Calls) != 0 {
			t.Errorf("err=%v calls=%v", err, a.Calls)
		}
	})

	t.Run("取り込み済みなら、パスなしでログインできる", func(t *testing.T) {
		// Arrange
		a := &usecasetest.Auth{SecretSaved: true}
		u := Login{Auth: a, Remote: usecasetest.NewRemote(usecasetest.NewDrive(t))}

		// Act
		_, err := u.Run(context.Background(), LoginInput{})

		// Assert
		if err != nil || !slices.Equal(a.Calls, []string{"login"}) {
			t.Errorf("err=%v calls=%v", err, a.Calls)
		}
	})
}

func TestLogout(t *testing.T) {
	t.Run("Auth の Logout を呼ぶ", func(t *testing.T) {
		// Arrange
		a := &usecasetest.Auth{TokenSaved: true}

		// Act
		err := Logout{Auth: a}.Run(context.Background())

		// Assert
		if err != nil || a.TokenSaved || !slices.Equal(a.Calls, []string{"logout"}) {
			t.Errorf("err=%v calls=%v", err, a.Calls)
		}
	})
}

func TestStatus(t *testing.T) {
	t.Run("Status はログインしていなければ LoggedIn=false と KindSetup", func(t *testing.T) {
		// Arrange
		u := Status{Dir: usecasetest.Dir{DirPath: "/cfg"}, Auth: &usecasetest.Auth{}, Remote: usecasetest.NewRemote(usecasetest.NewDrive(t))}

		// Act
		got, err := u.Run(context.Background())

		// Assert
		if domain.KindOf(err) != domain.KindSetup || got.LoggedIn || got.ConfigDir != "/cfg" {
			t.Errorf("got=%+v err=%v", got, err)
		}
	})

	t.Run("Status はログインしていればメールとルートを返す", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		root := d.AddRootFolder()
		r := usecasetest.NewRemote(d)
		r.State = &usecasetest.State{ID: root.ID}
		u := Status{Dir: usecasetest.Dir{DirPath: "/cfg"}, Auth: &usecasetest.Auth{TokenSaved: true, KeyStore: "os"}, Remote: r}

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
		u := Status{Dir: usecasetest.Dir{DirPath: "/cfg"}, Auth: &usecasetest.Auth{KeyStore: "file"}, Remote: usecasetest.NewRemote(usecasetest.NewDrive(t))}

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
		u := Status{Dir: usecasetest.Dir{DirPath: "/cfg"}, Auth: &usecasetest.Auth{KeyStore: "os", Problem: "鍵が合いません"}, Remote: usecasetest.NewRemote(usecasetest.NewDrive(t))}

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
		u := Status{Dir: usecasetest.Dir{DirPath: "/cfg"}, Auth: &usecasetest.Auth{KeyStore: "os", InspErr: want}, Remote: usecasetest.NewRemote(usecasetest.NewDrive(t))}

		// Act
		got, err := u.Run(context.Background())

		// Assert
		if !errors.Is(err, want) || got.KeyStore != "os" || got.LoggedIn || got.ConfigDir != "/cfg" {
			t.Errorf("got=%+v err=%v", got, err)
		}
	})
}
