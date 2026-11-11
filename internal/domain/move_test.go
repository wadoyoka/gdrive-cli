package domain

import (
	"slices"
	"strings"
	"testing"
)

func mp(t *testing.T, s string) RemotePath {
	t.Helper()
	p, err := ParseRemotePath(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlanMove(t *testing.T) {
	folder, file := TypeFolder, TypeFile

	t.Run("src にルートがあると KindUsage", func(t *testing.T) {
		// Arrange
		srcs := []RemotePath{RootPath()}

		// Act
		_, err := PlanMove(srcs, mp(t, "/d"), &folder)

		// Assert
		if KindOf(err) != KindUsage || !strings.Contains(err.Error(), "ルートフォルダは移せません") {
			t.Errorf("err=%v", err)
		}
	})

	t.Run("dest がフォルダなら、元の名前のまま中へ移す", func(t *testing.T) {
		// Arrange
		srcs := []RemotePath{mp(t, "/a/x"), mp(t, "/b/y")}

		// Act
		got, err := PlanMove(srcs, mp(t, "/d"), &folder)

		// Assert
		want := []MoveTarget{
			{Src: mp(t, "/a/x"), DestParent: mp(t, "/d"), Name: "x"},
			{Src: mp(t, "/b/y"), DestParent: mp(t, "/d"), Name: "y"},
		}
		if err != nil || !slices.EqualFunc(got, want, func(a, b MoveTarget) bool {
			return a.Src.String() == b.Src.String() && a.DestParent.String() == b.DestParent.String() && a.Name == b.Name
		}) {
			t.Errorf("got=%v err=%v", got, err)
		}
	})

	t.Run("dest がなく src が 1 つなら、移動と名前変更を同時に行う", func(t *testing.T) {
		// Act
		got, err := PlanMove([]RemotePath{mp(t, "/a/x")}, mp(t, "/d/new"), nil)

		// Assert
		if err != nil || len(got) != 1 || got[0].DestParent.String() != "/d" || got[0].Name != "new" {
			t.Errorf("got=%v err=%v", got, err)
		}
	})

	t.Run("dest がなく src が 2 つ以上なら KindNotFound", func(t *testing.T) {
		// Act
		_, err := PlanMove([]RemotePath{mp(t, "/a"), mp(t, "/b")}, mp(t, "/d"), nil)

		// Assert
		if KindOf(err) != KindNotFound || !strings.Contains(err.Error(), "/d が見つかりません") {
			t.Errorf("err=%v", err)
		}
	})

	t.Run("dest がファイルなら KindConflict", func(t *testing.T) {
		// Act
		_, err := PlanMove([]RemotePath{mp(t, "/a")}, mp(t, "/f"), &file)

		// Assert
		if KindOf(err) != KindConflict || !strings.Contains(err.Error(), "/f は既にあります") {
			t.Errorf("err=%v", err)
		}
	})

	t.Run("自分自身の中へは移せない", func(t *testing.T) {
		for _, dest := range []string{"/a", "/a/b"} {
			// Act
			_, err := PlanMove([]RemotePath{mp(t, "/a")}, mp(t, dest), &folder)

			// Assert
			if KindOf(err) != KindUsage || !strings.Contains(err.Error(), "/a を自分自身の中へは移せません") {
				t.Errorf("dest=%s err=%v", dest, err)
			}
		}
	})

	t.Run("名前を変えて自分の中へ移そうとしても KindUsage", func(t *testing.T) {
		// Act
		_, err := PlanMove([]RemotePath{mp(t, "/a")}, mp(t, "/a/b/c"), nil)

		// Assert
		if KindOf(err) != KindUsage {
			t.Errorf("err=%v", err)
		}
	})

	t.Run("移動先が元と同じ場所なら KindUsage", func(t *testing.T) {
		cases := []struct {
			dest  string
			dtype *EntryType
		}{{"/a", &folder}, {"/a/x", nil}}
		for _, c := range cases {
			// Act
			_, err := PlanMove([]RemotePath{mp(t, "/a/x")}, mp(t, c.dest), c.dtype)

			// Assert
			if KindOf(err) != KindUsage || !strings.Contains(err.Error(), "移動元と移動先が同じです") {
				t.Errorf("dest=%s err=%v", c.dest, err)
			}
		}
	})
}
