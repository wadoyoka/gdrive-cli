package usecase

import (
	"context"
	"slices"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

func seedMove(d *fakeDrive) (root, a, b domain.Entry) {
	root = d.addRootFolder()
	a = d.addFolder(root.ID, "a")
	b = d.addFolder(root.ID, "b")
	d.addFile(a.ID, "x.txt", []byte("x"))
	return root, a, b
}

func TestMove(t *testing.T) {
	ctx := context.Background()

	t.Run("ファイルをフォルダへ移す", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		_, a, b := seedMove(d)
		u := Move{Remote: newRemote(d)}

		// Act
		got, err := u.Run(ctx, []domain.RemotePath{mustPath(t, "/a/x.txt")}, mustPath(t, "/b"))

		// Assert
		if err != nil || len(got) != 1 || got[0].From != "/a/x.txt" || got[0].To != "/b/x.txt" || got[0].ID == "" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		if len(d.childrenOf(a.ID)) != 0 || len(d.childrenOf(b.ID)) != 1 {
			t.Errorf("a=%v b=%v", d.childrenOf(a.ID), d.childrenOf(b.ID))
		}
	})

	t.Run("名前を変える", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		_, a, b := seedMove(d)
		u := Move{Remote: newRemote(d)}

		// Act
		got, err := u.Run(ctx, []domain.RemotePath{mustPath(t, "/a/x.txt")}, mustPath(t, "/b/y.txt"))

		// Assert
		if err != nil || len(got) != 1 || got[0].To != "/b/y.txt" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		if c := d.childrenOf(b.ID); len(c) != 1 || c[0].Name != "y.txt" || len(d.childrenOf(a.ID)) != 0 {
			t.Errorf("b=%v", c)
		}
	})

	t.Run("同じフォルダの中で名前だけ変える", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		_, a, _ := seedMove(d)
		u := Move{Remote: newRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{mustPath(t, "/a/x.txt")}, mustPath(t, "/a/z.txt"))

		// Assert
		if c := d.childrenOf(a.ID); err != nil || len(c) != 1 || c[0].Name != "z.txt" {
			t.Errorf("a=%v err=%v", c, err)
		}
	})

	t.Run("フォルダを中身ごと移す", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		seedMove(d)
		r := newRemote(d)

		// Act
		_, err := Move{Remote: r}.Run(ctx, []domain.RemotePath{mustPath(t, "/a")}, mustPath(t, "/b"))
		got, lerr := List{Remote: r}.Run(ctx, domain.RootPath(), true)

		// Assert
		if want := []string{"/b", "/b/a", "/b/a/x.txt"}; err != nil || lerr != nil || !slices.Equal(paths(got), want) {
			t.Errorf("got=%v err=%v lerr=%v", paths(got), err, lerr)
		}
	})

	t.Run("移動先に同じ名前があれば、1 つも移さない", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		_, a, b := seedMove(d)
		d.addFile(a.ID, "y.txt", []byte("y"))
		d.addFile(b.ID, "y.txt", []byte("other"))
		u := Move{Remote: newRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{mustPath(t, "/a/x.txt"), mustPath(t, "/a/y.txt")}, mustPath(t, "/b"))

		// Assert
		if domain.KindOf(err) != domain.KindConflict || d.callCount("Move") != 0 {
			t.Errorf("err=%v moves=%d", err, d.callCount("Move"))
		}
	})

	t.Run("複数のソースが同じ名前になるなら、1 つも移さない", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		root, _, b := seedMove(d)
		d.addFile(b.ID, "x.txt", []byte("bx"))
		dst := d.addFolder(root.ID, "d")
		u := Move{Remote: newRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{mustPath(t, "/a/x.txt"), mustPath(t, "/b/x.txt")}, mustPath(t, "/d"))

		// Assert
		if domain.KindOf(err) != domain.KindConflict || d.callCount("Move") != 0 || len(d.childrenOf(dst.ID)) != 0 {
			t.Errorf("err=%v moves=%d", err, d.callCount("Move"))
		}
	})

	t.Run("自分自身の中へは移せない", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		seedMove(d)
		u := Move{Remote: newRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{mustPath(t, "/a")}, mustPath(t, "/a"))

		// Assert
		if domain.KindOf(err) != domain.KindUsage || d.callCount("Move") != 0 {
			t.Errorf("err=%v moves=%d", err, d.callCount("Move"))
		}
	})

	t.Run("複数のソースで、移動先がなければ KindNotFound", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		seedMove(d)
		u := Move{Remote: newRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{mustPath(t, "/a"), mustPath(t, "/b")}, mustPath(t, "/nope"))

		// Assert
		if domain.KindOf(err) != domain.KindNotFound || d.callCount("Move") != 0 {
			t.Errorf("err=%v moves=%d", err, d.callCount("Move"))
		}
	})

	t.Run("移動先の親がなければ KindNotFound", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		seedMove(d)
		u := Move{Remote: newRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{mustPath(t, "/a/x.txt")}, mustPath(t, "/nope/y.txt"))

		// Assert
		if domain.KindOf(err) != domain.KindNotFound || d.callCount("Move") != 0 {
			t.Errorf("err=%v moves=%d", err, d.callCount("Move"))
		}
	})

	t.Run("移すものがなければ KindNotFound", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		seedMove(d)
		u := Move{Remote: newRemote(d)}

		// Act
		_, err := u.Run(ctx, []domain.RemotePath{mustPath(t, "/a/none")}, mustPath(t, "/b"))

		// Assert
		if domain.KindOf(err) != domain.KindNotFound || d.callCount("Move") != 0 {
			t.Errorf("err=%v moves=%d", err, d.callCount("Move"))
		}
	})
}
