package plan

import (
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

func TestDecide(t *testing.T) {
	file, folder := domain.TypeFile, domain.TypeFolder
	modes := []ConflictMode{ConflictError, ConflictSkip, ConflictOverwrite}

	t.Run("送り先に何もなければ、種類とモードに関係なく created", func(t *testing.T) {
		for _, src := range []domain.EntryType{domain.TypeFile, domain.TypeFolder} {
			for _, mode := range modes {
				// Act
				got, err := Decide(src, nil, mode)

				// Assert
				if err != nil || got != ActionCreate {
					t.Errorf("src=%s mode=%d: got=%q err=%v", src, mode, got, err)
				}
			}
		}
	})

	t.Run("フォルダとフォルダは、モードに関係なく merged", func(t *testing.T) {
		for _, mode := range modes {
			// Act
			got, err := Decide(domain.TypeFolder, &folder, mode)

			// Assert
			if err != nil || got != ActionMerge {
				t.Errorf("mode=%d: got=%q err=%v", mode, got, err)
			}
		}
	})

	t.Run("ファイルとファイルは、モードで決まる", func(t *testing.T) {
		// Arrange
		cases := []struct {
			mode ConflictMode
			want Action
			kind domain.ErrorKind // want が空のときだけ見る
		}{
			{ConflictError, "", domain.KindConflict},
			{ConflictSkip, ActionSkip, 0},
			{ConflictOverwrite, ActionOverwrite, 0},
		}

		for _, c := range cases {
			// Act
			got, err := Decide(domain.TypeFile, &file, c.mode)

			// Assert
			if c.want == "" {
				if domain.KindOf(err) != c.kind || err == nil || got != "" {
					t.Errorf("mode=%d: got=%q err=%v", c.mode, got, err)
				}
				continue
			}
			if err != nil || got != c.want {
				t.Errorf("mode=%d: got=%q err=%v", c.mode, got, err)
			}
		}
	})

	t.Run("種類が違うと、モードに関係なく KindConflict", func(t *testing.T) {
		// Arrange
		cases := []struct {
			src      domain.EntryType
			existing *domain.EntryType
		}{
			{domain.TypeFile, &folder},
			{domain.TypeFolder, &file},
		}

		for _, c := range cases {
			for _, mode := range modes {
				// Act
				got, err := Decide(c.src, c.existing, mode)

				// Assert
				if err == nil || domain.KindOf(err) != domain.KindConflict || got != "" {
					t.Errorf("src=%s existing=%s mode=%d: got=%q err=%v", c.src, *c.existing, mode, got, err)
				}
			}
		}
	})

	t.Run("Action の値は JSON に出す文字列", func(t *testing.T) {
		// Arrange
		want := map[Action]string{ActionCreate: "created", ActionMerge: "merged", ActionSkip: "skipped", ActionOverwrite: "overwritten"}

		for a, s := range want {
			// Act
			got := string(a)

			// Assert
			if got != s {
				t.Errorf("got=%q want=%q", got, s)
			}
		}
	})
}
