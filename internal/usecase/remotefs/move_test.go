package remotefs

import (
	"context"
	"slices"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/usecasetest"
)

func seedMove(d *usecasetest.Drive) (root, a, b domain.Entry) {
	root = d.AddRootFolder()
	a = d.AddFolder(root.ID, "a")
	b = d.AddFolder(root.ID, "b")
	d.AddFile(a.ID, "x.txt", []byte("x"))
	return root, a, b
}

func TestMove(t *testing.T) {
	ctx := context.Background()

	t.Run("ファイルをフォルダへ移す", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		_, a, b := seedMove(d)
		u := Move{Remote: usecasetest.NewRemote(d)}

		// Act
		got, err := u.Run(ctx, []domain.RemotePath{usecasetest.MustPath(t, "/a/x.txt")}, usecasetest.MustPath(t, "/b"))

		// Assert
		if err != nil || len(got) != 1 || got[0].From != "/a/x.txt" || got[0].To != "/b/x.txt" || got[0].ID == "" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		if len(d.ChildrenOf(a.ID)) != 0 || len(d.ChildrenOf(b.ID)) != 1 {
			t.Errorf("a=%v b=%v", d.ChildrenOf(a.ID), d.ChildrenOf(b.ID))
		}
	})

	t.Run("名前を変える", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		_, a, b := seedMove(d)
		u := Move{Remote: usecasetest.NewRemote(d)}

		// Act
		got, err := u.Run(ctx, []domain.RemotePath{usecasetest.MustPath(t, "/a/x.txt")}, usecasetest.MustPath(t, "/b/y.txt"))

		// Assert
		if err != nil || len(got) != 1 || got[0].To != "/b/y.txt" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		if c := d.ChildrenOf(b.ID); len(c) != 1 || c[0].Name != "y.txt" || len(d.ChildrenOf(a.ID)) != 0 {
			t.Errorf("b=%v", c)
		}
	})

	t.Run("同じフォルダの中で名前だけ変える", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		_, a, _ := seedMove(d)
		u := Move{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{usecasetest.MustPath(t, "/a/x.txt")}, usecasetest.MustPath(t, "/a/z.txt"))

		// Assert
		if c := d.ChildrenOf(a.ID); err != nil || len(c) != 1 || c[0].Name != "z.txt" {
			t.Errorf("a=%v err=%v", c, err)
		}
	})

	t.Run("フォルダを中身ごと移す", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		seedMove(d)
		r := usecasetest.NewRemote(d)

		// Act
		_, err := Move{Remote: r}.Run(ctx, []domain.RemotePath{usecasetest.MustPath(t, "/a")}, usecasetest.MustPath(t, "/b"))
		got, lerr := List{Remote: r}.Run(ctx, domain.RootPath(), true)

		// Assert
		if want := []string{"/b", "/b/a", "/b/a/x.txt"}; err != nil || lerr != nil || !slices.Equal(paths(got), want) {
			t.Errorf("got=%v err=%v lerr=%v", paths(got), err, lerr)
		}
	})

	t.Run("移動先に同じ名前があれば、1 つも移さない", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		_, a, b := seedMove(d)
		d.AddFile(a.ID, "y.txt", []byte("y"))
		d.AddFile(b.ID, "y.txt", []byte("other"))
		u := Move{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{usecasetest.MustPath(t, "/a/x.txt"), usecasetest.MustPath(t, "/a/y.txt")}, usecasetest.MustPath(t, "/b"))

		// Assert
		if domain.KindOf(err) != domain.KindConflict || d.CallCount("Move") != 0 {
			t.Errorf("err=%v moves=%d", err, d.CallCount("Move"))
		}
	})

	t.Run("複数のソースが同じ名前になるなら、1 つも移さない", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		root, _, b := seedMove(d)
		d.AddFile(b.ID, "x.txt", []byte("bx"))
		dst := d.AddFolder(root.ID, "d")
		u := Move{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{usecasetest.MustPath(t, "/a/x.txt"), usecasetest.MustPath(t, "/b/x.txt")}, usecasetest.MustPath(t, "/d"))

		// Assert
		if domain.KindOf(err) != domain.KindConflict || d.CallCount("Move") != 0 || len(d.ChildrenOf(dst.ID)) != 0 {
			t.Errorf("err=%v moves=%d", err, d.CallCount("Move"))
		}
	})

	t.Run("自分自身の中へは移せない", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		seedMove(d)
		u := Move{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{usecasetest.MustPath(t, "/a")}, usecasetest.MustPath(t, "/a"))

		// Assert
		if domain.KindOf(err) != domain.KindUsage || d.CallCount("Move") != 0 {
			t.Errorf("err=%v moves=%d", err, d.CallCount("Move"))
		}
	})

	t.Run("複数のソースで、移動先がなければ KindNotFound", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		seedMove(d)
		u := Move{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{usecasetest.MustPath(t, "/a"), usecasetest.MustPath(t, "/b")}, usecasetest.MustPath(t, "/nope"))

		// Assert
		if domain.KindOf(err) != domain.KindNotFound || d.CallCount("Move") != 0 {
			t.Errorf("err=%v moves=%d", err, d.CallCount("Move"))
		}
	})

	t.Run("移動先の親がなければ KindNotFound", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		seedMove(d)
		u := Move{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{usecasetest.MustPath(t, "/a/x.txt")}, usecasetest.MustPath(t, "/nope/y.txt"))

		// Assert
		if domain.KindOf(err) != domain.KindNotFound || d.CallCount("Move") != 0 {
			t.Errorf("err=%v moves=%d", err, d.CallCount("Move"))
		}
	})

	t.Run("移すものがなければ KindNotFound", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		seedMove(d)
		u := Move{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{usecasetest.MustPath(t, "/a/none")}, usecasetest.MustPath(t, "/b"))

		// Assert
		if domain.KindOf(err) != domain.KindNotFound || d.CallCount("Move") != 0 {
			t.Errorf("err=%v moves=%d", err, d.CallCount("Move"))
		}
	})
}
