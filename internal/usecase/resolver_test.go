package usecase

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

func mustPath(t *testing.T, s string) domain.RemotePath {
	t.Helper()
	p, err := domain.ParseRemotePath(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newTestResolver() (*Resolver, *fakeDrive, *fakeState) {
	d := newFakeDrive()
	s := &fakeState{}
	return NewResolver(d, s, &fakeLocker{}), d, s
}

func TestResolver(t *testing.T) {
	ctx := context.Background()

	t.Run("初めてならルートを作り、ID を保存する", func(t *testing.T) {
		// Arrange
		r, d, s := newTestResolver()

		// Act
		root, err := r.Root(ctx)
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if d.callCount("CreateRootFolder") != 1 || s.id != root.ID || root.Name != RootFolderName {
			t.Errorf("create=%d state=%q root=%+v", d.callCount("CreateRootFolder"), s.id, root)
		}
	})

	t.Run("Root は結果を覚えておく", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver()

		// Act
		_, _ = r.Root(ctx)
		_, err := r.Root(ctx)

		// Assert
		if err != nil || d.callCount("FindRootFolders") != 1 {
			t.Errorf("err=%v find=%d", err, d.callCount("FindRootFolders"))
		}
	})

	t.Run("保存した ID のルートを使う", func(t *testing.T) {
		// Arrange
		r, d, s := newTestResolver()
		saved := d.addRootFolder()
		s.id = saved.ID

		// Act
		root, err := r.Root(ctx)

		// Assert
		if err != nil || root.ID != saved.ID {
			t.Fatalf("root=%+v err=%v", root, err)
		}
		if d.callCount("FindRootFolders") != 0 {
			t.Errorf("FindRootFolders = %d", d.callCount("FindRootFolders"))
		}
	})

	t.Run("保存した ID がゴミ箱なら KindSetup で、新しく作らない", func(t *testing.T) {
		// Arrange
		r, d, s := newTestResolver()
		saved := d.addRootFolder()
		d.setTrashed(saved.ID, true)
		s.id = saved.ID

		// Act
		_, err := r.Root(ctx)

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "ゴミ箱") {
			t.Errorf("err = %v", err)
		}
		if d.callCount("CreateRootFolder") != 0 {
			t.Errorf("CreateRootFolder = %d", d.callCount("CreateRootFolder"))
		}
	})

	t.Run("ID がなく、ゴミ箱のルートしかなければ KindSetup", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver()
		d.setTrashed(d.addRootFolder().ID, true)

		// Act
		_, err := r.Root(ctx)

		// Assert
		if domain.KindOf(err) != domain.KindSetup {
			t.Errorf("err = %v", err)
		}
		if d.callCount("CreateRootFolder") != 0 {
			t.Errorf("CreateRootFolder = %d", d.callCount("CreateRootFolder"))
		}
	})

	t.Run("ルートが 2 つなら古いほうを選び、警告を出す", func(t *testing.T) {
		// Arrange
		r, d, s := newTestResolver()
		older := d.addRootFolder()
		newer := d.addRootFolder()

		// Act
		root, err := r.Root(ctx)

		// Assert
		if err != nil || root.ID != older.ID || s.id != older.ID {
			t.Fatalf("root=%+v state=%q err=%v", root, s.id, err)
		}
		w := r.Warnings()
		if len(w) != 1 || !strings.Contains(w[0], newer.ID) || !strings.Contains(w[0], newer.WebViewLink) {
			t.Errorf("warnings = %v", w)
		}
	})

	t.Run("保存した ID が消えていたら探し直す", func(t *testing.T) {
		// Arrange
		r, d, s := newTestResolver()
		gone := d.addRootFolder()
		d.remove(gone.ID)
		s.id = gone.ID
		alive := d.addRootFolder()

		// Act
		root, err := r.Root(ctx)

		// Assert
		if err != nil || root.ID != alive.ID || s.id != alive.ID {
			t.Errorf("root=%+v state=%q err=%v", root, s.id, err)
		}
	})

	t.Run("Resolve はパスを埋めて返す", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver()
		root, _ := r.Root(ctx)
		a := d.addFolder(root.ID, "a")
		f := d.addFile(a.ID, "x.txt", []byte("hi"))

		// Act
		got, err := r.Resolve(ctx, mustPath(t, "/a/x.txt"))
		top, err2 := r.Resolve(ctx, domain.RootPath())

		// Assert
		if err != nil || err2 != nil {
			t.Fatal(err, err2)
		}
		if got.ID != f.ID || got.Path != "/a/x.txt" || top.ID != root.ID || top.Path != "/" {
			t.Errorf("got=%+v top=%+v", got, top)
		}
	})

	t.Run("ないパスは KindNotFound で、文言にパスを含む", func(t *testing.T) {
		// Arrange
		r, _, _ := newTestResolver()

		// Act
		_, err := r.Resolve(ctx, mustPath(t, "/nothing/here"))

		// Assert
		if domain.KindOf(err) != domain.KindNotFound || !strings.Contains(err.Error(), "/nothing/here") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("途中がファイルなら KindNotFound で、フォルダではないと伝える", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver()
		root, _ := r.Root(ctx)
		d.addFile(root.ID, "f", nil)

		// Act
		_, err := r.Resolve(ctx, mustPath(t, "/f/g"))

		// Assert
		if domain.KindOf(err) != domain.KindNotFound || !strings.Contains(err.Error(), "/f はフォルダではありません") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("同じ名前が 2 つなら KindAmbiguous で、詳細が 2 行", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver()
		root, _ := r.Root(ctx)
		d.addFolder(root.ID, "dup")
		d.addFolder(root.ID, "dup")

		// Act
		_, err := r.Resolve(ctx, mustPath(t, "/dup"))

		// Assert
		var de *domain.Error
		if domain.KindOf(err) != domain.KindAmbiguous || !errors.As(err, &de) || len(de.Details) != 2 {
			t.Fatalf("err = %v", err)
		}
		if !strings.HasPrefix(de.Details[0], "id-2  2026-01-01T00:00:02Z  https://example.com/id-2") {
			t.Errorf("details = %q", de.Details)
		}
	})

	t.Run("EnsureFolder は足りない分だけ作る", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver()
		root, _ := r.Root(ctx)
		d.addFolder(root.ID, "a")

		// Act
		got, created, err := r.EnsureFolder(ctx, mustPath(t, "/a/b/c"))
		_, again, err2 := r.EnsureFolder(ctx, mustPath(t, "/a/b/c"))

		// Assert
		if err != nil || err2 != nil {
			t.Fatal(err, err2)
		}
		want := []domain.RemotePath{mustPath(t, "/a/b"), mustPath(t, "/a/b/c")}
		if !reflect.DeepEqual(created, want) || got.Path != "/a/b/c" || got.Name != "c" {
			t.Errorf("created=%v got=%+v", created, got)
		}
		if len(again) != 0 {
			t.Errorf("again = %v", again)
		}
	})

	t.Run("EnsureFolder は途中のファイルで KindConflict", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver()
		root, _ := r.Root(ctx)
		d.addFile(root.ID, "f", nil)

		// Act
		_, _, err := r.EnsureFolder(ctx, mustPath(t, "/f/g"))

		// Assert
		if domain.KindOf(err) != domain.KindConflict || !strings.Contains(err.Error(), "/f はファイルです") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestRemote(t *testing.T) {
	t.Run("Connect は Drive と Resolver を返す", func(t *testing.T) {
		// Arrange
		d := newFakeDrive()
		remote := Remote{
			Open:   func(context.Context) (Drive, error) { return d, nil },
			State:  &fakeState{},
			Locker: &fakeLocker{},
		}

		// Act
		s, err := remote.Connect(context.Background())

		// Assert
		if err != nil || s.Drive != Drive(d) || s.Resolver == nil {
			t.Errorf("s=%+v err=%v", s, err)
		}
	})
}
