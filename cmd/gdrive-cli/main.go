// gdrive-cli は、Google Drive を操作するコマンドラインツール。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/wadoyoka/gdrive-cli/internal/cli"
	"github.com/wadoyoka/gdrive-cli/internal/repository"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

func main() {
	// Ctrl-C だけでなく、エージェントの実行環境などが送る SIGTERM でも止め、download の一時ファイルを片付ける。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx)
	stop()
	os.Exit(code)
}

func run(ctx context.Context) int {
	dir, err := repository.DefaultConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		return 1
	}
	configDir := repository.NewConfigDir(dir)
	policy := repository.NewPolicyFile(dir)
	keys := repository.NewKeyStore(dir, os.Getenv("GDRIVE_CLI_KEYRING"))
	auth := repository.NewAuth(dir, keys, repository.NewLockFile(dir).Lock)
	remote := usecase.Remote{
		Open:   repository.NewDriveOpener(auth),
		State:  repository.NewStateFile(dir),
		Locker: repository.NewLockFile(dir),
	}
	login := usecase.Login{Auth: auth, Remote: remote}
	status := usecase.Status{Dir: configDir, Auth: auth, Remote: remote}
	initUC := usecase.Init{Dir: configDir, Policy: policy}
	return cli.Execute(ctx, cli.Deps{
		Init:     initUC,
		Login:    login,
		Logout:   usecase.Logout{Auth: auth},
		Status:   status,
		Mkdir:    usecase.Mkdir{Remote: remote},
		List:     usecase.List{Remote: remote},
		Upload:   usecase.Upload{Remote: remote},
		Download: usecase.Download{Remote: remote},
		Move:     usecase.Move{Remote: remote},
		Policy:   policy,
		Setup: usecase.Setup{
			Init:    initUC,
			Policy:  policy,
			Auth:    auth,
			Cloud:   repository.NewGCloud(),
			Login:   login,
			Status:  status,
			OpenURL: repository.OpenBrowser,
		},
		Interactive: term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd())),
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Progress:    term.IsTerminal(int(os.Stderr.Fd())),
	}, os.Args[1:])
}
