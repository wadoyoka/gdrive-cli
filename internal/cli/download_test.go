package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/domain/plan"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/transfer"
)

// downloadDrive は、ルートの直下にファイル（名前 → 中身）がある状態から download を受け付ける偽の Drive。
// fail の名前は Download が途中で失敗する。
type downloadDrive struct {
	usecase.Drive
	files map[string]string
	fail  map[string]bool
}

func (*downloadDrive) Get(context.Context, string) (domain.Entry, error) {
	return domain.Entry{ID: "root-1", Name: "gdrive-cli", Type: domain.TypeFolder}, nil
}

func (d *downloadDrive) FindChildren(_ context.Context, _, name string) ([]domain.Entry, error) {
	content, ok := d.files[name]
	if !ok {
		return nil, nil
	}
	return []domain.Entry{{ID: "id-" + name, Name: name, Type: domain.TypeFile, Size: int64(len(content)), WebViewLink: "https://example.com/id-" + name}}, nil
}

func (d *downloadDrive) Download(_ context.Context, id string, w io.Writer, progress func(int64)) error {
	name := strings.TrimPrefix(id, "id-")
	if d.fail[name] {
		_, _ = io.WriteString(w, "par")
		return errors.New("通信が切れました")
	}
	n, err := io.WriteString(w, d.files[name])
	if progress != nil {
		progress(int64(n))
	}
	return err
}

func withDownload(d Deps, drive *downloadDrive) Deps {
	r := usecase.Remote{
		Open:   func(context.Context) (usecase.Drive, error) { return drive, nil },
		State:  stubState{},
		Locker: stubLocker{},
	}
	d.Download = transfer.Download{Remote: r, Concurrency: 1}
	return d
}

func TestDownloadCommand(t *testing.T) {
	t.Run("download は 1 件ずつ処理、パス、リンクをタブ区切りで出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withDownload(d, &downloadDrive{files: map[string]string{"a.mp4": "video"}})
		initAndClear(t, d, out, errb)
		dest := t.TempDir()

		// Act
		code := Execute(context.Background(), d, []string{"download", "/a.mp4", dest})

		// Assert
		want := "created\t/a.mp4\thttps://example.com/id-a.mp4\n"
		got, _ := os.ReadFile(filepath.Join(dest, "a.mp4")) //nolint:gosec // テストの一時ディレクトリ
		if code != 0 || out.String() != want || string(got) != "video" {
			t.Errorf("code=%d out=%q err=%q file=%q", code, out.String(), errb.String(), got)
		}
	})

	t.Run("--json は items を JSON で出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withDownload(d, &downloadDrive{files: map[string]string{"a.mp4": "video"}})
		initAndClear(t, d, out, errb)
		dest := t.TempDir()

		// Act
		code := Execute(context.Background(), d, []string{"--json", "download", "/a.mp4", dest})

		// Assert
		var got transfer.Result
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("json: %v: %q", err, out.String())
		}
		want := transfer.Item{
			LocalPath: filepath.Join(dest, "a.mp4"), RemotePath: "/a.mp4", ID: "id-a.mp4", Type: domain.TypeFile, Size: 5,
			WebViewLink: "https://example.com/id-a.mp4", Action: plan.ActionCreate,
		}
		if code != 0 || len(got.Items) != 1 || got.Items[0] != want {
			t.Errorf("code=%d got=%+v", code, got)
		}
	})

	t.Run("手元に同じ名前があると 5 で何も書かない", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withDownload(d, &downloadDrive{files: map[string]string{"a.mp4": "new"}})
		initAndClear(t, d, out, errb)
		dest := t.TempDir()
		old := localFile(t, "a.mp4", "old")
		if err := os.Rename(old, filepath.Join(dest, "a.mp4")); err != nil {
			t.Fatal(err)
		}

		// Act
		code := Execute(context.Background(), d, []string{"download", "/a.mp4", dest})

		// Assert
		got, _ := os.ReadFile(filepath.Join(dest, "a.mp4")) //nolint:gosec // テストの一時ディレクトリ
		if code != 5 || out.String() != "" || string(got) != "old" || !strings.Contains(errb.String(), "  - /a.mp4") {
			t.Errorf("code=%d out=%q err=%q file=%q", code, out.String(), errb.String(), got)
		}
	})

	t.Run("--overwrite と --skip-existing を一緒に付けると 2", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withDownload(d, &downloadDrive{files: map[string]string{"a.mp4": "new"}})
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"download", "--overwrite", "--skip-existing", "/a.mp4", t.TempDir()})

		// Assert
		if code != 2 {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("一部が失敗すると 1 で、受け取れたものは標準出力に、失敗したものは標準エラーに出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withDownload(d, &downloadDrive{files: map[string]string{"a.mp4": "a", "b.mp4": "b"}, fail: map[string]bool{"b.mp4": true}})
		initAndClear(t, d, out, errb)
		dest := t.TempDir()

		// Act
		code := Execute(context.Background(), d, []string{"download", "/a.mp4", "/b.mp4", dest})

		// Assert
		if want := "created\t/a.mp4\thttps://example.com/id-a.mp4\n"; code != 1 || out.String() != want {
			t.Errorf("code=%d out=%q", code, out.String())
		}
		if !strings.Contains(errb.String(), "1 件のダウンロードに失敗しました") || !strings.Contains(errb.String(), "/b.mp4: 通信が切れました") {
			t.Errorf("err=%q", errb.String())
		}
		if _, err := os.Stat(filepath.Join(dest, "b.mp4")); err == nil {
			t.Errorf("失敗したファイルが残っている")
		}
	})

	t.Run("引数が 1 つだと 2", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withDownload(d, &downloadDrive{})
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"download", "/a.mp4"})

		// Assert
		if code != 2 {
			t.Errorf("code=%d", code)
		}
	})

	t.Run("ドライブ側のパスが不正だと 2", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withDownload(d, &downloadDrive{})
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"download", "/a/../b", t.TempDir()})

		// Assert
		if code != 2 || !strings.Contains(errb.String(), "エラー") {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("保存先がないと 4、-p なら作る", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withDownload(d, &downloadDrive{files: map[string]string{"a.mp4": "a"}})
		initAndClear(t, d, out, errb)
		dest := filepath.Join(t.TempDir(), "x")

		// Act
		codeNo := Execute(context.Background(), d, []string{"download", "/a.mp4", dest})
		codeP := Execute(context.Background(), d, []string{"download", "-p", "/a.mp4", dest})

		// Assert
		if _, err := os.Stat(filepath.Join(dest, "a.mp4")); codeNo != 4 || codeP != 0 || err != nil {
			t.Errorf("codeNo=%d codeP=%d err=%v", codeNo, codeP, err)
		}
	})

	t.Run("Progress が true なら標準エラーに進捗を出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withDownload(d, &downloadDrive{files: map[string]string{"a.mp4": "video"}})
		d.Progress = true
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"download", "/a.mp4", t.TempDir()})

		// Assert
		if code != 0 || !strings.Contains(errb.String(), "/a.mp4  100%\n") {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})
}
