package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCommand(d Deps, asJSON *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "ログインの状態を表示する",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, runErr := d.Status.Run(cmd.Context())
			var werr error
			switch {
			case *asJSON:
				werr = writeJSON(d.Stdout, res)
			case res.LoggedIn:
				_, werr = fmt.Fprintf(d.Stdout, "設定ディレクトリ: %s\nログイン: 済み（%s）\nルートフォルダ: %s\n",
					res.ConfigDir, res.Email, res.RootFolderLink)
			default:
				_, werr = fmt.Fprintf(d.Stdout, "設定ディレクトリ: %s\nログイン: していません\n", res.ConfigDir)
			}
			if runErr != nil {
				return runErr
			}
			return werr
		},
	}
}
