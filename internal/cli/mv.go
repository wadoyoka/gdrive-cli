package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wadoyoka/gdrive-cli/internal/usecase"
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
				if !*asJSON {
					_ = writeMoveItems(d, items)
				}
				return err
			}
			if *asJSON {
				if items == nil {
					items = []usecase.MoveItem{}
				}
				return writeJSON(d.Stdout, items)
			}
			return writeMoveItems(d, items)
		},
	}
}

func writeMoveItems(d Deps, items []usecase.MoveItem) error {
	for _, it := range items {
		if _, err := fmt.Fprintf(d.Stdout, "%s\t%s\t%s\n", it.From, it.To, it.WebViewLink); err != nil {
			return err
		}
	}
	return nil
}
