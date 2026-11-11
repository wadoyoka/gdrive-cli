package domain

import "testing"

func TestDecide(t *testing.T) {
	file, folder := TypeFile, TypeFolder
	modes := []ConflictMode{ConflictError, ConflictSkip, ConflictOverwrite}

	t.Run("送り先に何もなければ、種類とモードに関係なく created", func(t *testing.T) {
		for _, src := range []EntryType{TypeFile, TypeFolder} {
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
			got, err := Decide(TypeFolder, &folder, mode)

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
			kind ErrorKind // want が空のときだけ見る
		}{
			{ConflictError, "", KindConflict},
			{ConflictSkip, ActionSkip, 0},
			{ConflictOverwrite, ActionOverwrite, 0},
		}

		for _, c := range cases {
			// Act
			got, err := Decide(TypeFile, &file, c.mode)

			// Assert
			if c.want == "" {
				if KindOf(err) != c.kind || err == nil || got != "" {
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
			src      EntryType
			existing *EntryType
		}{
			{TypeFile, &folder},
			{TypeFolder, &file},
		}

		for _, c := range cases {
			for _, mode := range modes {
				// Act
				got, err := Decide(c.src, c.existing, mode)

				// Assert
				if err == nil || KindOf(err) != KindConflict || got != "" {
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
