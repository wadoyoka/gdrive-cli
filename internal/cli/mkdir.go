package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/domain/winpath"
)

// parseRemotePath は、引数をドライブ側のパスにする。誤りは KindUsage。
// windows が true なら、解釈の前に、Git Bash が書き換えたパスと \ 区切りのパスを止める。
func parseRemotePath(s string, windows bool) (domain.RemotePath, error) {
	if windows {
		if err := winpath.CheckRemoteArg(s); err != nil {
			return domain.RemotePath{}, usageError(err)
		}
	}
	p, err := domain.ParseRemotePath(s)
	if err != nil {
		return domain.RemotePath{}, usageError(err)
	}
	return p, nil
}

// parseRemotePaths は、引数をそれぞれドライブ側のパスにする。誤りは KindUsage。
func parseRemotePaths(args []string, windows bool) ([]domain.RemotePath, error) {
	paths := make([]domain.RemotePath, 0, len(args))
	for _, a := range args {
		p, err := parseRemotePath(a, windows)
		if err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func newMkdirCommand(d Deps, asJSON *bool) *cobra.Command {
	var parents bool
	cmd := &cobra.Command{
		Use:   "mkdir [-p] <パス>...",
		Short: "フォルダを作る",
		Args:  usageArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := parseRemotePaths(args, d.WindowsPaths)
			if err != nil {
				return err
			}
			entries, err := d.Mkdir.Run(cmd.Context(), paths, parents)
			if err != nil {
				// 途中で失敗しても、作れたものは出す。JSON なら、結果は標準出力に、エラーは標準エラーに出る。
				if len(entries) > 0 {
					_ = writeMkdirEntries(d, *asJSON, entries)
				}
				return err
			}
			return writeMkdirEntries(d, *asJSON, entries)
		},
	}
	cmd.Flags().BoolVarP(&parents, "parents", "p", false, "途中のフォルダも作り、既にあってもエラーにしない")
	return cmd
}

// writeMkdirEntries は、作ったフォルダを、JSON の配列か「<パス>\t<webViewLink>」の行で書く。
func writeMkdirEntries(d Deps, asJSON bool, entries []domain.Entry) error {
	if asJSON {
		return writeJSON(d.Stdout, entries)
	}
	for _, e := range entries {
		if _, err := fmt.Fprintf(d.Stdout, "%s\t%s\n", e.Path, e.WebViewLink); err != nil {
			return err
		}
	}
	return nil
}
