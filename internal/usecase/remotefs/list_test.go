package remotefs

import (
	"context"
	"slices"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/usecasetest"
)

func seedTree(d *usecasetest.Drive) {
	root := d.AddRootFolder()
	d.AddFile(root.ID, "z.html", []byte("z"))
	a := d.AddFolder(root.ID, "a")
	b := d.AddFolder(a.ID, "b")
	d.AddFile(b.ID, "c.mp4", []byte("c"))
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
		d := usecasetest.NewDrive(t)
		seedTree(d)
		u := List{Remote: usecasetest.NewRemote(d)}

		// Act
		got, err := u.Run(context.Background(), domain.RootPath(), false)

		// Assert
		if want := []string{"/a", "/z.html"}; err != nil || !slices.Equal(paths(got), want) {
			t.Errorf("got=%v err=%v", paths(got), err)
		}
	})

	t.Run("ls -R はパスの順に全部返す", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		seedTree(d)
		u := List{Remote: usecasetest.NewRemote(d)}

		// Act
		got, err := u.Run(context.Background(), domain.RootPath(), true)

		// Assert
		if want := []string{"/a", "/a/b", "/a/b/c.mp4", "/z.html"}; err != nil || !slices.Equal(paths(got), want) {
			t.Errorf("got=%v err=%v", paths(got), err)
		}
	})

	t.Run("ls にファイルを渡すとそれだけ返す", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		seedTree(d)
		u := List{Remote: usecasetest.NewRemote(d)}

		// Act
		got, err := u.Run(context.Background(), usecasetest.MustPath(t, "/z.html"), false)

		// Assert
		if want := []string{"/z.html"}; err != nil || !slices.Equal(paths(got), want) {
			t.Errorf("got=%v err=%v", paths(got), err)
		}
	})

	t.Run("ls はパスがないと KindNotFound", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		seedTree(d)
		u := List{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(context.Background(), usecasetest.MustPath(t, "/nope"), false)

		// Assert
		if domain.KindOf(err) != domain.KindNotFound {
			t.Errorf("err=%v", err)
		}
	})
}
