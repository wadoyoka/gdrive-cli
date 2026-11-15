package domain

import "testing"

func TestParseRemotePath(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		want  string
		usage bool
	}{
		{"空文字はルート", "", "/", false},
		{"スラッシュだけはルート", "/", "/", false},
		{"先頭のスラッシュがなくてもよい", "videos", "/videos", false},
		{"末尾のスラッシュは除く", "/videos/2026-11/", "/videos/2026-11", false},
		{"空の名前は誤り", "a//b", "", true},
		{"親への参照は誤り", "/a/../b", "", true},
		{"現在への参照は誤り", "/./a", "", true},
		{"NFD は NFC にそろえる", "/ダ.mp4", "/ダ.mp4", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange
			// Act
			p, err := ParseRemotePath(c.in)

			// Assert
			if c.usage {
				if KindOf(err) != KindUsage {
					t.Fatalf("err = %v, want KindUsage", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if p.String() != c.want {
				t.Errorf("got %q, want %q", p.String(), c.want)
			}
		})
	}
}

func mustParse(t *testing.T, s string) RemotePath {
	t.Helper()
	p, err := ParseRemotePath(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRemotePath(t *testing.T) {
	t.Run("Parent と Base", func(t *testing.T) {
		// Arrange
		p := mustParse(t, "/a/b")

		// Act
		parent, base := p.Parent(), p.Base()

		// Assert
		if parent.String() != "/a" || base != "b" {
			t.Errorf("got %q, %q", parent, base)
		}
	})

	t.Run("Join は下に足す", func(t *testing.T) {
		// Arrange
		p := mustParse(t, "/a")

		// Act
		got := p.Join("c")

		// Assert
		if got.String() != "/a/c" {
			t.Errorf("got %q", got)
		}
		if p.String() != "/a" {
			t.Errorf("元のパスが変わった: %q", p)
		}
	})

	t.Run("Contains は同じか下にあるときだけ真", func(t *testing.T) {
		// Arrange
		a := mustParse(t, "/a")

		// Act / Assert
		for q, want := range map[string]bool{"/a/b": true, "/ab": false, "/a": true, "/": false} {
			if got := a.Contains(mustParse(t, q)); got != want {
				t.Errorf("/a.Contains(%s) = %v, want %v", q, got, want)
			}
		}
		if !RootPath().Contains(a) {
			t.Error("ルートはすべてを含む")
		}
	})

	t.Run("ルートの親はルートで Base は空", func(t *testing.T) {
		// Arrange
		r := RootPath()

		// Act
		parent := r.Parent()

		// Assert
		if !parent.IsRoot() || r.Base() != "" || r.String() != "/" || len(r.Segments()) != 0 {
			t.Errorf("ルートの性質が崩れている: %q", parent)
		}
	})

	t.Run("Segments の変更が元に影響しない", func(t *testing.T) {
		// Arrange
		p := mustParse(t, "/a/b")

		// Act
		p.Segments()[0] = "z"

		// Assert
		if p.String() != "/a/b" {
			t.Errorf("got %q", p)
		}
	})
}
