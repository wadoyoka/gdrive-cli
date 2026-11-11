package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

// uploadDrive は、ルートだけがある状態から upload を受け付ける偽の Drive。
// existing の名前は、ルートの直下に既にあるファイルとして見せる。fail の名前は Upload が失敗する。
type uploadDrive struct {
	usecase.Drive
	existing map[string]bool
	fail     map[string]bool

	mu sync.Mutex
	n  int
}

func (*uploadDrive) Get(context.Context, string) (domain.Entry, error) {
	return domain.Entry{ID: "root-1", Name: "gdrive-cli", Type: domain.TypeFolder}, nil
}

func (d *uploadDrive) FindChildren(_ context.Context, _, name string) ([]domain.Entry, error) {
	if d.existing[name] {
		return []domain.Entry{{ID: "old-" + name, Name: name, Type: domain.TypeFile, WebViewLink: "https://example.com/old-" + name}}, nil
	}
	return nil, nil
}

func (d *uploadDrive) Upload(_ context.Context, _, name, _ string, body io.Reader, size int64, progress func(int64)) (domain.Entry, error) {
	if _, err := io.Copy(io.Discard, body); err != nil {
		return domain.Entry{}, err
	}
	if d.fail[name] {
		return domain.Entry{}, errors.New("通信が切れました")
	}
	if progress != nil {
		progress(size)
	}
	d.mu.Lock()
	d.n++
	id := fmt.Sprintf("u%d", d.n)
	d.mu.Unlock()
	return domain.Entry{ID: id, Name: name, Type: domain.TypeFile, Size: size, WebViewLink: "https://example.com/" + id}, nil
}

func withUpload(d Deps, drive *uploadDrive) Deps {
	r := usecase.Remote{
		Open:   func(context.Context) (usecase.Drive, error) { return drive, nil },
		State:  stubState{},
		Locker: stubLocker{},
	}
	d.Upload = usecase.Upload{Remote: r, Concurrency: 1}
	return d
}

func localFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUploadCommand(t *testing.T) {
	t.Run("upload は 1 件ずつ処理、パス、リンクをタブ区切りで出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withUpload(d, &uploadDrive{})
		initAndClear(t, d, out, errb)
		a := localFile(t, "a.mp4", "video")

		// Act
		code := Execute(context.Background(), d, []string{"upload", a, "/"})

		// Assert
		if want := "created\t/a.mp4\thttps://example.com/u1\n"; code != 0 || out.String() != want {
			t.Errorf("code=%d out=%q err=%q", code, out.String(), errb.String())
		}
	})

	t.Run("--json は items を JSON で出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withUpload(d, &uploadDrive{})
		initAndClear(t, d, out, errb)
		a := localFile(t, "a.mp4", "video")

		// Act
		code := Execute(context.Background(), d, []string{"--json", "upload", a, "/"})

		// Assert
		var got usecase.TransferResult
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("json: %v: %q", err, out.String())
		}
		want := usecase.TransferItem{LocalPath: a, RemotePath: "/a.mp4", ID: "u1", Type: domain.TypeFile, Size: 5, WebViewLink: "https://example.com/u1", Action: domain.ActionCreate}
		if code != 0 || len(got.Items) != 1 || got.Items[0] != want {
			t.Errorf("code=%d got=%+v", code, got)
		}
	})

	t.Run("--skip-existing なら skipped と出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withUpload(d, &uploadDrive{existing: map[string]bool{"a.mp4": true}})
		initAndClear(t, d, out, errb)
		a := localFile(t, "a.mp4", "video")

		// Act
		code := Execute(context.Background(), d, []string{"upload", "--skip-existing", a, "/"})

		// Assert
		if want := "skipped\t/a.mp4\thttps://example.com/old-a.mp4\n"; code != 0 || out.String() != want {
			t.Errorf("code=%d out=%q err=%q", code, out.String(), errb.String())
		}
	})

	t.Run("同じ名前があると 5 で、衝突したパスを出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withUpload(d, &uploadDrive{existing: map[string]bool{"a.mp4": true}})
		initAndClear(t, d, out, errb)
		a := localFile(t, "a.mp4", "video")

		// Act
		code := Execute(context.Background(), d, []string{"upload", a, "/"})

		// Assert
		if code != 5 || out.String() != "" || !strings.Contains(errb.String(), "  - /a.mp4") {
			t.Errorf("code=%d out=%q err=%q", code, out.String(), errb.String())
		}
	})

	t.Run("一部が失敗すると 1 で、送れたものは標準出力に、失敗したものは標準エラーに出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withUpload(d, &uploadDrive{fail: map[string]bool{"b.mp4": true}})
		initAndClear(t, d, out, errb)
		a, b := localFile(t, "a.mp4", "a"), localFile(t, "b.mp4", "b")

		// Act
		code := Execute(context.Background(), d, []string{"upload", a, b, "/"})

		// Assert
		if want := "created\t/a.mp4\thttps://example.com/u1\n"; code != 1 || out.String() != want {
			t.Errorf("code=%d out=%q", code, out.String())
		}
		if !strings.Contains(errb.String(), "1 件のアップロードに失敗しました") || !strings.Contains(errb.String(), "/b.mp4: 通信が切れました") {
			t.Errorf("err=%q", errb.String())
		}
	})

	t.Run("警告は標準エラーに出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withUpload(d, &uploadDrive{})
		initAndClear(t, d, out, errb)
		a := localFile(t, "a.mp4", "a")
		link := filepath.Join(t.TempDir(), "link.mp4")
		if err := os.Symlink(a, link); err != nil {
			t.Fatal(err)
		}

		// Act
		code := Execute(context.Background(), d, []string{"upload", a, link, "/"})

		// Assert
		if code != 0 || !strings.Contains(errb.String(), "注意: シンボリックリンクは飛ばしました: "+link) {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("引数が 1 つだと 2", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withUpload(d, &uploadDrive{})
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"upload", "/"})

		// Assert
		if code != 2 {
			t.Errorf("code=%d", code)
		}
	})

	t.Run("送り先のパスが不正だと 2", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withUpload(d, &uploadDrive{})
		initAndClear(t, d, out, errb)
		a := localFile(t, "a.mp4", "a")

		// Act
		code := Execute(context.Background(), d, []string{"upload", a, "/a/../b"})

		// Assert
		if code != 2 || !strings.Contains(errb.String(), "エラー") {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("Progress が true なら標準エラーに進捗を出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withUpload(d, &uploadDrive{})
		d.Progress = true
		initAndClear(t, d, out, errb)
		a := localFile(t, "a.mp4", "video")

		// Act
		code := Execute(context.Background(), d, []string{"upload", a, "/"})

		// Assert
		if code != 0 || !strings.Contains(errb.String(), "/a.mp4  100%\n") {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("--json のときと Progress が false のときは進捗を出さない", func(t *testing.T) {
		// Arrange
		dj, outj, errj, _ := newDeps(t)
		dj = withUpload(dj, &uploadDrive{})
		dj.Progress = true
		initAndClear(t, dj, outj, errj)
		dn, outn, errn, _ := newDeps(t)
		dn = withUpload(dn, &uploadDrive{})
		initAndClear(t, dn, outn, errn)
		a := localFile(t, "a.mp4", "video")

		// Act
		codeJ := Execute(context.Background(), dj, []string{"--json", "upload", a, "/"})
		codeN := Execute(context.Background(), dn, []string{"upload", a, "/"})

		// Assert
		if codeJ != 0 || codeN != 0 || errj.String() != "" || errn.String() != "" {
			t.Errorf("codeJ=%d codeN=%d errJ=%q errN=%q", codeJ, codeN, errj.String(), errn.String())
		}
	})
}
