// gdrive-cli は、Google Drive を操作するコマンドラインツール。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"golang.org/x/term"

	"github.com/wadoyoka/gdrive-cli/internal/cli"
	"github.com/wadoyoka/gdrive-cli/internal/repository/auth"
	"github.com/wadoyoka/gdrive-cli/internal/repository/browser"
	"github.com/wadoyoka/gdrive-cli/internal/repository/config"
	"github.com/wadoyoka/gdrive-cli/internal/repository/driveapi"
	"github.com/wadoyoka/gdrive-cli/internal/repository/gcloud"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/account"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/remotefs"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/setup"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/transfer"
)

func main() {
	// Ctrl-C だけでなく、エージェントの実行環境などが送る SIGTERM でも止め、download の一時ファイルを片付ける。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx)
	stop()
	os.Exit(code)
}

func run(ctx context.Context) int {
	onWindows := runtime.GOOS == "windows"
	dir, err := config.DefaultDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		return 1
	}
	configDir := config.NewDir(dir)
	policy := config.NewPolicyFile(dir)
	keys := auth.NewKeyStore(dir, os.Getenv("GDRIVE_CLI_KEYRING"))
	authn := auth.NewAuth(dir, keys, config.NewLockFile(dir).Lock)
	remote := usecase.Remote{
		Open:   driveapi.NewOpener(authn),
		State:  config.NewStateFile(dir),
		Locker: config.NewLockFile(dir),
	}
	login := account.Login{Auth: authn, Remote: remote}
	status := account.Status{Dir: configDir, Auth: authn, Remote: remote}
	initUC := setup.Init{Dir: configDir, Policy: policy}
	return cli.Execute(ctx, cli.Deps{
		Init:     initUC,
		Login:    login,
		Logout:   account.Logout{Auth: authn},
		Status:   status,
		Mkdir:    remotefs.Mkdir{Remote: remote},
		List:     remotefs.List{Remote: remote},
		Upload:   transfer.Upload{Remote: remote},
		Download: transfer.Download{Remote: remote, WindowsNames: onWindows},
		Move:     remotefs.Move{Remote: remote},
		Policy:   policy,
		Setup: setup.Setup{
			Init:    initUC,
			Policy:  policy,
			Auth:    authn,
			Cloud:   gcloud.NewGCloud(),
			Login:   login,
			Status:  status,
			OpenURL: browser.Open,
			Windows: onWindows,
		},
		WindowsPaths: onWindows,
		Interactive:  term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd())),
		Stdin:        os.Stdin,
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
		Progress:     term.IsTerminal(int(os.Stderr.Fd())),
	}, os.Args[1:])
}
