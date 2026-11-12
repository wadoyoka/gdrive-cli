// Package cli は、コマンドラインの入口（引数の解釈と表示）を持つ。
package cli

import (
	"context"
	"io"
	"slices"

	"github.com/spf13/cobra"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

// Deps は、コマンドが使うものをまとめたもの。
type Deps struct {
	Init     usecase.Init
	Login    usecase.Login
	Logout   usecase.Logout
	Status   usecase.Status
	Mkdir    usecase.Mkdir
	List     usecase.List
	Upload   usecase.Upload
	Download usecase.Download
	Move     usecase.Move
	Policy   usecase.PolicyStore
	Setup    usecase.Setup
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Progress bool // 標準エラーがターミナルなら true
	// Interactive は、標準入力と標準エラーが両方とも端末なら true。
	Interactive bool
	// 後のタスクで、各 usecase のフィールドを足していく
}

// 拒否リストを確かめないコマンド。
var unchecked = []string{"init", "setup", "help", "completion", "gdrive-cli"}

// Execute は、args を実行して終了コードを返す。
func Execute(ctx context.Context, d Deps, args []string) int {
	root := newRootCommand(d)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		asJSON, _ := root.PersistentFlags().GetBool("json")
		writeError(d.Stderr, asJSON, err)
		return domain.KindOf(err).ExitCode()
	}
	return 0
}

func usageError(err error) error {
	if err == nil {
		return nil
	}
	return domain.Errorf(domain.KindUsage, "%s", err.Error()).Wrap(err)
}

func usageArgs(f cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error { return usageError(f(cmd, args)) }
}

func newRootCommand(d Deps) *cobra.Command {
	asJSON := new(bool)
	root := &cobra.Command{
		Use:           "gdrive-cli",
		Short:         "Google Drive を操作する CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          usageArgs(cobra.NoArgs),
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if slices.Contains(unchecked, cmd.Name()) {
				return nil
			}
			p, err := d.Policy.Load()
			if err != nil {
				return err
			}
			return p.Check(cmd.Name())
		},
	}
	root.SetIn(d.Stdin)
	root.SetOut(d.Stdout)
	root.SetErr(d.Stderr)
	root.PersistentFlags().BoolVar(asJSON, "json", false, "結果を JSON で出す")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError(err) })
	root.AddCommand(newInitCommand(d, asJSON), newSetupCommand(d, asJSON), newLoginCommand(d, asJSON), newLogoutCommand(d, asJSON), newStatusCommand(d, asJSON),
		newMkdirCommand(d, asJSON), newLsCommand(d, asJSON), newUploadCommand(d, asJSON), newDownloadCommand(d, asJSON), newMvCommand(d, asJSON))
	return root
}
