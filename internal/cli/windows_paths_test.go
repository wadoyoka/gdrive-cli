package cli

import (
	"context"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/usecase"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/remotefs"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/transfer"
)

// gitBashPath は、Git Bash が /videos を書き換えた形。
const gitBashPath = "C:/Program Files/Git/x"

// withCountingRemote は、全コマンドが同じ偽のドライブを使い、開いた回数を数える。
func withCountingRemote(d Deps, opens *int) Deps {
	r := usecase.Remote{
		Open: func(context.Context) (usecase.Drive, error) {
			*opens++
			return emptyDrive{}, nil
		},
		State:  stubState{},
		Locker: stubLocker{},
	}
	d.Mkdir = remotefs.Mkdir{Remote: r}
	d.List = remotefs.List{Remote: r}
	d.Move = remotefs.Move{Remote: r}
	d.Upload = transfer.Upload{Remote: r, Concurrency: 1}
	d.Download = transfer.Download{Remote: r, Concurrency: 1}
	return d
}

func TestWindowsPathsStopsAllCommands(t *testing.T) {
	t.Run("ドライブ側の引数が Git Bash の書き換えた形なら、どのコマンドも終了コード 2 で止まり、ドライブを開かない", func(t *testing.T) {
		cases := []struct {
			name string
			args []string
		}{
			{"ls", []string{"ls", gitBashPath}},
			{"mkdir", []string{"mkdir", gitBashPath}},
			{"mv の移動元", []string{"mv", gitBashPath, "/dst"}},
			{"mv の移動先", []string{"mv", "/src", gitBashPath}},
			{"upload の送り先", []string{"upload", "a.txt", gitBashPath}},
			{"download のドライブのパス", []string{"download", gitBashPath, "out"}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				// Arrange
				d, out, errb, _ := newDeps(t)
				opens := 0
				d = withCountingRemote(d, &opens)
				d.WindowsPaths = true
				initAndClear(t, d, out, errb)

				// Act
				code := Execute(context.Background(), d, c.args)

				// Assert
				if code != 2 || opens != 0 {
					t.Errorf("code=%d opens=%d err=%q", code, opens, errb.String())
				}
			})
		}
	})

	t.Run("download の手元のパス（最後の引数）は、C: で始まっても止めない", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		opens := 0
		d = withCountingRemote(d, &opens)
		d.WindowsPaths = true
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"download", "/a.txt", "C:/out"})

		// Assert
		if code == 2 {
			t.Errorf("手元のパスを Windows の判定で止めた: code=%d err=%q", code, errb.String())
		}
	})
}
