package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/remotefs"
)

// emptyDrive は、ルートだけがあって中身が空の偽の Drive。
type emptyDrive struct{ usecase.Drive }

func (emptyDrive) Get(context.Context, string) (domain.Entry, error) {
	return domain.Entry{ID: "root-1", Name: "gdrive-cli", Type: domain.TypeFolder}, nil
}

func (emptyDrive) FindChildren(context.Context, string, string) ([]domain.Entry, error) {
	return nil, nil
}

func (emptyDrive) ListChildren(context.Context, string) ([]domain.Entry, error) {
	return []domain.Entry{{ID: "f1", Name: "a.txt", Type: domain.TypeFile, Size: 3, ModifiedTime: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}, nil
}

func withRemote(d Deps) Deps {
	r := usecase.Remote{
		Open:   func(context.Context) (usecase.Drive, error) { return emptyDrive{}, nil },
		State:  stubState{},
		Locker: stubLocker{},
	}
	d.Mkdir = remotefs.Mkdir{Remote: r}
	d.List = remotefs.List{Remote: r}
	d.Move = remotefs.Move{Remote: r}
	return d
}

func TestLsCommand(t *testing.T) {
	t.Run("ls のパスがないと終了コードは 4", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withRemote(d)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"ls", "/nope"})

		// Assert
		if code != 4 {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("ls -l は タブ区切りで出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withRemote(d)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"ls", "-l"})

		// Assert
		if want := "file\t3\t2026-01-02T03:04:05Z\tf1\t/a.txt\n"; code != 0 || out.String() != want {
			t.Errorf("code=%d out=%q", code, out.String())
		}
	})

	t.Run("ls に不正なパスを渡すと 2", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withRemote(d)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"ls", "/a/../b"})

		// Assert
		if code != 2 || !strings.Contains(errb.String(), "エラー") {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("mkdir に引数がないと 2", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withRemote(d)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"mkdir"})

		// Assert
		if code != 2 {
			t.Errorf("code=%d", code)
		}
	})
}
