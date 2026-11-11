package usecase

import (
	"context"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

func TestMkdir(t *testing.T) {
	t.Run("mkdir は親がないと KindNotFound", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		u := Mkdir{Remote: newRemote(d)}

		// Act
		_, err := u.Run(context.Background(), []domain.RemotePath{mustPath(t, "/a/b")}, false)

		// Assert
		if domain.KindOf(err) != domain.KindNotFound {
			t.Errorf("err=%v", err)
		}
		if d.callCount("CreateFolder") != 0 {
			t.Errorf("CreateFolder が呼ばれた")
		}
	})

	t.Run("mkdir は既にあると KindConflict", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		root := d.addRootFolder()
		d.addFolder(root.ID, "a")
		u := Mkdir{Remote: newRemote(d)}

		// Act
		_, errExisting := u.Run(context.Background(), []domain.RemotePath{mustPath(t, "/a")}, false)
		_, errRoot := u.Run(context.Background(), []domain.RemotePath{domain.RootPath()}, false)

		// Assert
		if domain.KindOf(errExisting) != domain.KindConflict || domain.KindOf(errRoot) != domain.KindConflict {
			t.Errorf("existing=%v root=%v", errExisting, errRoot)
		}
	})

	t.Run("mkdir は親があれば作って項目を返す", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		root := d.addRootFolder()
		d.addFolder(root.ID, "a")
		u := Mkdir{Remote: newRemote(d)}

		// Act
		got, err := u.Run(context.Background(), []domain.RemotePath{mustPath(t, "/a/b")}, false)

		// Assert
		if err != nil || len(got) != 1 || got[0].Path != "/a/b" || got[0].Type != domain.TypeFolder {
			t.Errorf("got=%v err=%v", got, err)
		}
	})

	t.Run("mkdir -p は親ごと作り、2 回目もエラーにしない", func(t *testing.T) {
		// Arrange
		d := newFakeDrive(t)
		d.addRootFolder()
		u := Mkdir{Remote: newRemote(d)}
		paths := []domain.RemotePath{mustPath(t, "/a/b/c")}

		// Act
		first, err1 := u.Run(context.Background(), paths, true)
		second, err2 := u.Run(context.Background(), paths, true)

		// Assert
		if err1 != nil || err2 != nil || len(first) != 1 || len(second) != 1 || first[0].ID != second[0].ID {
			t.Errorf("first=%v second=%v err1=%v err2=%v", first, second, err1, err2)
		}
		if d.callCount("CreateFolder") != 3 {
			t.Errorf("CreateFolder=%d", d.callCount("CreateFolder"))
		}
	})
}
