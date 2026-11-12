package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wadoyoka/gdrive-cli/internal/usecase/remotefs"
)

func newMvCommand(d Deps, asJSON *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "mv <移動元>... <移動先>",
		Short: "ファイルやフォルダを移す（名前も変えられる）",
		Args:  usageArgs(cobra.MinimumNArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := parseRemotePaths(args)
			if err != nil {
				return err
			}
			items, err := d.Move.Run(cmd.Context(), paths[:len(paths)-1], paths[len(paths)-1])
			if err != nil {
				// 途中で失敗しても、移せたものは出す。JSON なら、結果は標準出力に、エラーは標準エラーに出る。
				if len(items) > 0 {
					_ = writeMoveItems(d, *asJSON, items)
				}
				return err
			}
			return writeMoveItems(d, *asJSON, items)
		},
	}
}

// writeMoveItems は、移したものを、JSON の配列か「<移動元>\t<移動先>\t<webViewLink>」の行で書く。
func writeMoveItems(d Deps, asJSON bool, items []remotefs.MoveItem) error {
	if asJSON {
		if items == nil {
			items = []remotefs.MoveItem{}
		}
		return writeJSON(d.Stdout, items)
	}
	for _, it := range items {
		if _, err := fmt.Fprintf(d.Stdout, "%s\t%s\t%s\n", it.From, it.To, it.WebViewLink); err != nil {
			return err
		}
	}
	return nil
}
