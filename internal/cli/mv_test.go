package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/remotefs"
)

// partialDrive は、ルートの直下に a.txt、b.txt、フォルダ dir がある偽の Drive。
// failName という名前でのフォルダの作成と移動は失敗する。
type partialDrive struct {
	usecase.Drive
	failName string
}

func (partialDrive) Get(context.Context, string) (domain.Entry, error) {
	return domain.Entry{ID: "root-1", Name: "gdrive-cli", Type: domain.TypeFolder}, nil
}

func (partialDrive) FindChildren(_ context.Context, parentID, name string) ([]domain.Entry, error) {
	if parentID != "root-1" {
		return nil, nil
	}
	switch name {
	case "a.txt", "b.txt":
		return []domain.Entry{{ID: "id-" + name, Name: name, Type: domain.TypeFile}}, nil
	case "dir":
		return []domain.Entry{{ID: "dir-1", Name: name, Type: domain.TypeFolder}}, nil
	}
	return nil, nil
}

func (d partialDrive) fail(name string) error {
	if name != d.failName {
		return nil
	}
	return domain.Errorf(domain.KindUnexpected, "Drive の操作に失敗しました").Wrap(errors.New("googleapi: Error 503: backend error"))
}

func (d partialDrive) CreateFolder(_ context.Context, _, name string) (domain.Entry, error) {
	if err := d.fail(name); err != nil {
		return domain.Entry{}, err
	}
	return domain.Entry{ID: "new-" + name, Name: name, Type: domain.TypeFolder, WebViewLink: "https://example.com/new-" + name}, nil
}

func (d partialDrive) Move(_ context.Context, id, _, _, newName string) (domain.Entry, error) {
	if err := d.fail(newName); err != nil {
		return domain.Entry{}, err
	}
	return domain.Entry{ID: id, Name: newName, Type: domain.TypeFile, WebViewLink: "https://example.com/" + id}, nil
}

func withPartial(d Deps, failName string) Deps {
	r := usecase.Remote{
		Open:   func(context.Context) (usecase.Drive, error) { return partialDrive{failName: failName}, nil },
		State:  stubState{},
		Locker: stubLocker{},
	}
	d.Mkdir = remotefs.Mkdir{Remote: r}
	d.Move = remotefs.Move{Remote: r}
	return d
}

func TestMvCommand(t *testing.T) {
	t.Run("mv に引数が 1 つだと 2", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withRemote(d)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"mv", "/a"})

		// Assert
		if code != 2 {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("mv に不正なパスを渡すと 2", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withRemote(d)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"mv", "/a/../b", "/c"})

		// Assert
		if code != 2 {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("mv の移動元がなければ 4", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withRemote(d)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"mv", "/nope", "/c"})

		// Assert
		if code != 4 {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("--json で途中で失敗したら、移せたものを標準出力に、エラーを標準エラーに出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withPartial(d, "b.txt")
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"--json", "mv", "/a.txt", "/b.txt", "/dir"})

		// Assert
		var items []remotefs.MoveItem
		if err := json.Unmarshal(out.Bytes(), &items); err != nil {
			t.Fatalf("json: %v: %q", err, out.String())
		}
		if code != 1 || len(items) != 1 || items[0].From != "/a.txt" || items[0].To != "/dir/a.txt" {
			t.Errorf("code=%d items=%+v", code, items)
		}
		if !strings.Contains(errb.String(), `"kind":"unexpected"`) || !strings.Contains(errb.String(), "backend error") {
			t.Errorf("err=%q", errb.String())
		}
	})

	t.Run("--json で最初に失敗したら、標準出力には何も出さない", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withPartial(d, "a.txt")
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"--json", "mv", "/a.txt", "/b.txt", "/dir"})

		// Assert
		if code != 1 || out.Len() != 0 || !strings.Contains(errb.String(), `"kind":"unexpected"`) {
			t.Errorf("code=%d out=%q err=%q", code, out.String(), errb.String())
		}
	})
}
