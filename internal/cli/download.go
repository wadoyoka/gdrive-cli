package cli

import (
	"github.com/spf13/cobra"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/transfer"
)

func newDownloadCommand(d Deps, asJSON *bool) *cobra.Command {
	var parents, overwrite, skipExisting bool
	cmd := &cobra.Command{
		Use:   "download [-p] [--overwrite | --skip-existing] <ドライブのパス>... <手元のフォルダ>",
		Short: "ドライブのファイルやフォルダを手元へ受け取る",
		Args:  usageArgs(cobra.MinimumNArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			sources := make([]domain.RemotePath, 0, len(args)-1)
			for _, a := range args[:len(args)-1] {
				p, err := domain.ParseRemotePath(a)
				if err != nil {
					return usageError(err)
				}
				sources = append(sources, p)
			}
			in := transfer.DownloadInput{
				Sources: sources, DestDir: args[len(args)-1],
				Parents: parents, Overwrite: overwrite, SkipExisting: skipExisting,
			}
			var progress *progressPrinter
			if d.Progress && !*asJSON {
				progress = newProgressPrinter(d.Stderr)
				in.Progress = progress.Report
			}
			res, err := d.Download.Run(cmd.Context(), in)
			progress.Finish()
			werr := writeTransferResult(d, *asJSON, res, err == nil)
			if err != nil {
				return err
			}
			return werr
		},
	}
	cmd.Flags().BoolVarP(&parents, "parents", "p", false, "保存先のフォルダがなければ作る")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "同じ名前のファイルを置き換える")
	cmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "同じ名前のファイルがあれば飛ばす（中身は比べない）")
	return cmd
}
