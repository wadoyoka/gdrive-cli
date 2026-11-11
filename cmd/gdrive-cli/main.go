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
	auth := repository.NewAuth(dir)
	remote := usecase.Remote{
		Open:   repository.NewDriveOpener(auth),
		State:  repository.NewStateFile(dir),
		Locker: repository.NewLockFile(dir),
	}
	return cli.Execute(ctx, cli.Deps{
		Init:     usecase.Init{Dir: configDir, Policy: policy},
		Login:    usecase.Login{Auth: auth, Remote: remote},
		Logout:   usecase.Logout{Auth: auth},
		Status:   usecase.Status{Dir: configDir, Auth: auth, Remote: remote},
		Mkdir:    usecase.Mkdir{Remote: remote},
		List:     usecase.List{Remote: remote},
		Upload:   usecase.Upload{Remote: remote},
		Download: usecase.Download{Remote: remote},
		Move:     usecase.Move{Remote: remote},
		Policy:   policy,
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Progress: term.IsTerminal(int(os.Stderr.Fd())),
	}, os.Args[1:])
}
