package usecase

import (
	"context"
	"slices"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

func seedTree(d *fakeDrive) {
	root := d.addRootFolder()
	d.addFile(root.ID, "z.html", []byte("z"))
	a := d.addFolder(root.ID, "a")
	b := d.addFolder(a.ID, "b")
	d.addFile(b.ID, "c.mp4", []byte("c"))
}

func paths(es []domain.Entry) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.Path)
	}
	return out
}

func TestList(t *testing.T) {
	t.Run("ls はフォルダの中身を返す", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		seedTree(d)
		u := List{Remote: newRemote(d)}

		// Act
		got, err := u.Run(context.Background(), domain.RootPath(), false)

		// Assert
		if want := []string{"/a", "/z.html"}; err != nil || !slices.Equal(paths(got), want) {
			t.Errorf("got=%v err=%v", paths(got), err)
		}
	})

	t.Run("ls -R はパスの順に全部返す", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		seedTree(d)
		u := List{Remote: newRemote(d)}

		// Act
		got, err := u.Run(context.Background(), domain.RootPath(), true)

		// Assert
		if want := []string{"/a", "/a/b", "/a/b/c.mp4", "/z.html"}; err != nil || !slices.Equal(paths(got), want) {
			t.Errorf("got=%v err=%v", paths(got), err)
		}
	})

	t.Run("ls にファイルを渡すとそれだけ返す", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		seedTree(d)
		u := List{Remote: newRemote(d)}

		// Act
		got, err := u.Run(context.Background(), mustPath(t, "/z.html"), false)

		// Assert
		if want := []string{"/z.html"}; err != nil || !slices.Equal(paths(got), want) {
			t.Errorf("got=%v err=%v", paths(got), err)
		}
	})

	t.Run("ls はパスがないと KindNotFound", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		seedTree(d)
		u := List{Remote: newRemote(d)}

		// Act
		_, err := u.Run(context.Background(), mustPath(t, "/nope"), false)

		// Assert
		if domain.KindOf(err) != domain.KindNotFound {
			t.Errorf("err=%v", err)
		}
	})
}
