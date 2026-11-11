package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

func newLsCommand(d Deps, asJSON *bool) *cobra.Command {
	var recursive, long bool
	cmd := &cobra.Command{
		Use:   "ls [-R] [-l] [パス]",
		Short: "フォルダの中身を表示する",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := "/"
			if len(args) == 1 {
				arg = args[0]
			}
			p, err := domain.ParseRemotePath(arg)
			if err != nil {
				return usageError(err)
			}
			entries, err := d.List.Run(cmd.Context(), p, recursive)
			if err != nil {
				return err
			}
			if *asJSON {
				return writeJSON(d.Stdout, entries)
			}
			for _, e := range entries {
				if err := writeLsLine(d.Stdout, e, recursive, long); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&recursive, "recursive", "R", false, "下の階層もすべて表示する")
	cmd.Flags().BoolVarP(&long, "long", "l", false, "種類、サイズ、更新日時、ID も表示する")
	return cmd
}

func writeLsLine(w io.Writer, e domain.Entry, full, long bool) error {
	if long {
		_, err := fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n", e.Type, e.Size, e.ModifiedTime.Format(time.RFC3339), e.ID, e.Path)
		return err
	}
	name := e.Name
	if full {
		name = e.Path
	}
	if e.Type == domain.TypeFolder {
		name += "/"
	}
	_, err := fmt.Fprintln(w, name)
	return err
}
