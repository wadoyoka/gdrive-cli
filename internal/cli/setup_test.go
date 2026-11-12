package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

// recCloud は、呼ばれたことを記録する偽の gcloud。
type recCloud struct {
	calls []string
}

func (c *recCloud) Installed() bool { c.calls = append(c.calls, "Installed"); return true }
func (c *recCloud) ActiveAccount(context.Context) (string, error) {
	c.calls = append(c.calls, "ActiveAccount")
	return "me@example.com", nil
}

func (c *recCloud) DescribeProject(context.Context, string) (usecase.CloudProject, bool, error) {
	c.calls = append(c.calls, "DescribeProject")
	return usecase.CloudProject{}, false, nil
}

func (c *recCloud) CreateProject(context.Context, string) error {
	c.calls = append(c.calls, "CreateProject")
	return nil
}

func (c *recCloud) EnableDrive(context.Context, string) error {
	c.calls = append(c.calls, "EnableDrive")
	return nil
}

// setupAuth は、取り込みとログインの流れを真似る偽の Authenticator。
type setupAuth struct {
	stubAuth
	imported  bool
	loginRead string // Login が Input から読んだ1行
}

func (a *setupAuth) ClientSecretProjectID(string) (string, error) { return "", nil }
func (a *setupAuth) ImportClientSecret(string) error              { a.imported = true; return nil }
func (a *setupAuth) Inspect() (usecase.AuthInspection, error) {
	return usecase.AuthInspection{KeyStore: "os", ClientSecret: a.imported}, nil
}

func (a *setupAuth) Login(_ context.Context, opt usecase.LoginOptions) error {
	line, err := bufio.NewReader(opt.Input).ReadString('\n')
	a.loginRead = strings.TrimSpace(line)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	a.token = true
	return nil
}

func setupDeps(t *testing.T, stdin string) (Deps, *bytes.Buffer, *bytes.Buffer, *recCloud, *setupAuth) {
	t.Helper()
	d, out, errb, _ := newDeps(t)
	d.Stdin = strings.NewReader(stdin)
	d.Interactive = true
	cloud := &recCloud{}
	auth := &setupAuth{stubAuth: stubAuth{keyStore: "os"}}
	r := usecase.Remote{
		Open:   func(context.Context) (usecase.Drive, error) { return stubDrive{}, nil },
		State:  stubState{},
		Locker: stubLocker{},
	}
	d.Setup = usecase.Setup{
		Init:   d.Init,
		Policy: d.Policy,
		Auth:   auth,
		Cloud:  cloud,
		Login:  usecase.Login{Auth: auth, Remote: r},
		Status: usecase.Status{Dir: d.Init.Dir, Auth: auth, Remote: r},
	}
	return d, out, errb, cloud, auth
}

func TestSetupCommand(t *testing.T) {
	t.Run("端末でなければ終了コード 2 で、gcloud を呼ばない", func(t *testing.T) {
		// Arrange
		d, _, errb, cloud, _ := setupDeps(t, "")
		d.Interactive = false

		// Act
		code := Execute(context.Background(), d, []string{"setup"})

		// Assert
		if code != 2 || !strings.Contains(errb.String(), "人が端末で実行してください") {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
		if len(cloud.calls) != 0 {
			t.Errorf("gcloud が呼ばれた: %v", cloud.calls)
		}
	})

	t.Run("--json を付けると終了コード 2 で、gcloud を呼ばない", func(t *testing.T) {
		// Arrange
		d, _, errb, cloud, _ := setupDeps(t, "")

		// Act
		code := Execute(context.Background(), d, []string{"--json", "setup"})

		// Assert
		if code != 2 || !strings.Contains(errb.String(), "--json に対応していません") {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
		if len(cloud.calls) != 0 {
			t.Errorf("gcloud が呼ばれた: %v", cloud.calls)
		}
	})

	t.Run("拒否リストのファイルがなくても、入口では止めない", func(t *testing.T) {
		// Arrange（init していないので policy.toml も設定ディレクトリもない）
		d, _, errb, cloud, _ := setupDeps(t, "")
		d.Interactive = false

		// Act
		code := Execute(context.Background(), d, []string{"setup"})

		// Assert（root の判定で止まらず、setup 自身の判定に届いている）
		if code != 2 || !strings.Contains(errb.String(), "人が端末で実行してください") {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
		if len(cloud.calls) != 0 {
			t.Errorf("gcloud が呼ばれた: %v", cloud.calls)
		}
	})

	t.Run("結果を標準出力にまとめて出す", func(t *testing.T) {
		// Arrange
		d, out, _, _, _ := setupDeps(t, "my-proj-12345\n/tmp/client.json\ncode-line\n")

		// Act
		code := Execute(context.Background(), d, []string{"setup", "--no-browser"})

		// Assert
		want := "プロジェクト: my-proj-12345（gcloud のアカウント: me@example.com）\n" +
			"ログイン: user@example.com\n" +
			"ルートフォルダ: https://example.com/root-1\n" +
			"鍵の置き場所: OS のキーチェーン\n" +
			"ダウンロードした JSON は、パスワードマネージャーなど安全な場所に保管してください。\n"
		if code != 0 || out.String() != want {
			t.Errorf("code=%d out=%q", code, out.String())
		}
	})

	t.Run("質問とログインが同じ読み手を使う", func(t *testing.T) {
		// Arrange
		d, _, _, _, auth := setupDeps(t, "my-proj-12345\n/tmp/client.json\ncode-line\n")

		// Act
		code := Execute(context.Background(), d, []string{"setup", "--no-browser"})

		// Assert
		if code != 0 || auth.loginRead != "code-line" {
			t.Errorf("code=%d loginRead=%q", code, auth.loginRead)
		}
	})

	t.Run("setup --help が使い方を出す", func(t *testing.T) {
		// Arrange
		d, out, _, cloud, _ := setupDeps(t, "")

		// Act
		code := Execute(context.Background(), d, []string{"setup", "--help"})

		// Assert
		if code != 0 || !strings.Contains(out.String(), "--no-browser") || !strings.Contains(out.String(), "--project") {
			t.Errorf("code=%d out=%q", code, out.String())
		}
		if len(cloud.calls) != 0 {
			t.Errorf("gcloud が呼ばれた: %v", cloud.calls)
		}
	})
}

func TestTermPrompter(t *testing.T) {
	t.Run("Confirm は y と yes だけを true にする", func(t *testing.T) {
		for _, tc := range []struct {
			in   string
			want bool
		}{{"y\n", true}, {"yes\n", true}, {"YES\r\n", true}, {"Y\n", true}, {"\n", false}, {"n\n", false}, {"yep\n", false}} {
			// Arrange
			var errb bytes.Buffer
			p := termPrompter{in: bufio.NewReader(strings.NewReader(tc.in)), out: &errb}

			// Act
			got, err := p.Confirm("続けますか")

			// Assert
			if err != nil || got != tc.want {
				t.Errorf("in=%q got=%v err=%v", tc.in, got, err)
			}
			if !strings.Contains(errb.String(), "続けますか [y/N]: ") {
				t.Errorf("質問の表示: %q", errb.String())
			}
		}
	})

	t.Run("Ask は Enter だけなら既定値を返し、入力の終わりは io.EOF", func(t *testing.T) {
		// Arrange
		var errb bytes.Buffer
		p := termPrompter{in: bufio.NewReader(strings.NewReader("\nabc\r\n")), out: &errb}

		// Act
		a, errA := p.Ask("ID", "def")
		b, errB := p.Ask("ID", "def")
		_, errC := p.Ask("ID", "def")

		// Assert
		if errA != nil || a != "def" || errB != nil || b != "abc" || !errors.Is(errC, io.EOF) {
			t.Errorf("a=%q,%v b=%q,%v c=%v", a, errA, b, errB, errC)
		}
		if !strings.Contains(errb.String(), "ID [def]: ") {
			t.Errorf("質問の表示: %q", errb.String())
		}
	})
}
