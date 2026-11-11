package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

func newUploadCommand(d Deps, asJSON *bool) *cobra.Command {
	var parents, skipExisting bool
	cmd := &cobra.Command{
		Use:   "upload [-p] [--skip-existing] <手元のパス>... <送り先のフォルダ>",
		Short: "手元のファイルやフォルダをドライブへ送る",
		Args:  usageArgs(cobra.MinimumNArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest, err := domain.ParseRemotePath(args[len(args)-1])
			if err != nil {
				return usageError(err)
			}
			in := usecase.UploadInput{Sources: args[:len(args)-1], Dest: dest, Parents: parents, SkipExisting: skipExisting}
			var progress *progressPrinter
			if d.Progress && !*asJSON {
				progress = newProgressPrinter(d.Stderr)
				in.Progress = progress.Report
			}
			res, err := d.Upload.Run(cmd.Context(), in)
			progress.Finish()
			werr := writeTransferResult(d, *asJSON, res, err == nil)
			if err != nil {
				return err
			}
			return werr
		},
	}
	cmd.Flags().BoolVarP(&parents, "parents", "p", false, "送り先のフォルダがなければ作る")
	cmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "同じ名前のファイルがあれば飛ばす（中身は比べない）")
	return cmd
}

// writeTransferResult は、送受信の結果を書く。succeeded が false で項目がなければ、標準出力には何も書かない。
// JSON でなければ、警告を標準エラーに、行えた項目を 1 件ずつ「<action>\t<remotePath>\t<webViewLink>」で標準出力に書く。
// 失敗した項目は、エラーの補足として標準エラーに出るので、ここでは書かない。
func writeTransferResult(d Deps, asJSON bool, res usecase.TransferResult, succeeded bool) error {
	if asJSON {
		if !succeeded && len(res.Items) == 0 {
			return nil
		}
		return writeJSON(d.Stdout, res)
	}
	for _, w := range res.Warnings {
		_, _ = fmt.Fprintf(d.Stderr, "注意: %s\n", w)
	}
	for _, it := range res.Items {
		if it.Error != "" {
			continue
		}
		if _, err := fmt.Fprintf(d.Stdout, "%s\t%s\t%s\n", it.Action, it.RemotePath, it.WebViewLink); err != nil {
			return err
		}
	}
	return nil
}
