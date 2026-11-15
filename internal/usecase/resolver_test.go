package usecase_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/usecasetest"
)

func newTestResolver(t *testing.T) (*usecase.Resolver, *usecasetest.Drive, *usecasetest.State) {
	d := usecasetest.NewDrive(t)
	s := &usecasetest.State{}
	return usecase.NewResolver(d, s, &usecasetest.Locker{}), d, s
}

func TestResolver(t *testing.T) {
	ctx := context.Background()

	t.Run("初めてならルートを作り、ID を保存する", func(t *testing.T) {
		// Arrange
		r, d, s := newTestResolver(t)

		// Act
		root, err := r.Root(ctx)
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if d.CallCount("CreateRootFolder") != 1 || s.ID != root.ID || root.Name != usecase.RootFolderName {
			t.Errorf("create=%d state=%q root=%+v", d.CallCount("CreateRootFolder"), s.ID, root)
		}
	})

	t.Run("Root は結果を覚えておく", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver(t)

		// Act
		_, _ = r.Root(ctx)
		_, err := r.Root(ctx)

		// Assert
		if err != nil || d.CallCount("FindRootFolders") != 1 {
			t.Errorf("err=%v find=%d", err, d.CallCount("FindRootFolders"))
		}
	})

	t.Run("保存した ID のルートを使う", func(t *testing.T) {
		// Arrange
		r, d, s := newTestResolver(t)
		saved := d.AddRootFolder()
		s.ID = saved.ID

		// Act
		root, err := r.Root(ctx)

		// Assert
		if err != nil || root.ID != saved.ID {
			t.Fatalf("root=%+v err=%v", root, err)
		}
		if d.CallCount("FindRootFolders") != 0 {
			t.Errorf("FindRootFolders = %d", d.CallCount("FindRootFolders"))
		}
	})

	t.Run("保存した ID がゴミ箱なら KindSetup で、新しく作らない", func(t *testing.T) {
		// Arrange
		r, d, s := newTestResolver(t)
		saved := d.AddRootFolder()
		d.SetTrashed(saved.ID, true)
		s.ID = saved.ID

		// Act
		_, err := r.Root(ctx)

		// Assert
		if domain.KindOf(err) != domain.KindSetup || !strings.Contains(err.Error(), "ゴミ箱") {
			t.Errorf("err = %v", err)
		}
		if d.CallCount("CreateRootFolder") != 0 {
			t.Errorf("CreateRootFolder = %d", d.CallCount("CreateRootFolder"))
		}
	})

	t.Run("ID がなく、ゴミ箱のルートしかなければ KindSetup", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver(t)
		d.SetTrashed(d.AddRootFolder().ID, true)

		// Act
		_, err := r.Root(ctx)

		// Assert
		if domain.KindOf(err) != domain.KindSetup {
			t.Errorf("err = %v", err)
		}
		if d.CallCount("CreateRootFolder") != 0 {
			t.Errorf("CreateRootFolder = %d", d.CallCount("CreateRootFolder"))
		}
	})

	t.Run("ルートが 2 つなら古いほうを選び、警告を出す", func(t *testing.T) {
		// Arrange
		r, d, s := newTestResolver(t)
		older := d.AddRootFolder()
		newer := d.AddRootFolder()

		// Act
		root, err := r.Root(ctx)

		// Assert
		if err != nil || root.ID != older.ID || s.ID != older.ID {
			t.Fatalf("root=%+v state=%q err=%v", root, s.ID, err)
		}
		w := r.Warnings()
		if len(w) != 1 || !strings.Contains(w[0], newer.ID) || !strings.Contains(w[0], newer.WebViewLink) {
			t.Errorf("warnings = %v", w)
		}
	})

	t.Run("保存した ID が消えていたら探し直す", func(t *testing.T) {
		// Arrange
		r, d, s := newTestResolver(t)
		gone := d.AddRootFolder()
		d.Remove(gone.ID)
		s.ID = gone.ID
		alive := d.AddRootFolder()

		// Act
		root, err := r.Root(ctx)

		// Assert
		if err != nil || root.ID != alive.ID || s.ID != alive.ID {
			t.Errorf("root=%+v state=%q err=%v", root, s.ID, err)
		}
	})

	t.Run("Resolve はパスを埋めて返す", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver(t)
		root, _ := r.Root(ctx)
		a := d.AddFolder(root.ID, "a")
		f := d.AddFile(a.ID, "x.txt", []byte("hi"))

		// Act
		got, err := r.Resolve(ctx, usecasetest.MustPath(t, "/a/x.txt"))
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
		r, _, _ := newTestResolver(t)

		// Act
		_, err := r.Resolve(ctx, usecasetest.MustPath(t, "/nothing/here"))

		// Assert
		if domain.KindOf(err) != domain.KindNotFound || !strings.Contains(err.Error(), "/nothing/here") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("途中がファイルなら KindNotFound で、フォルダではないと伝える", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver(t)
		root, _ := r.Root(ctx)
		d.AddFile(root.ID, "f", nil)

		// Act
		_, err := r.Resolve(ctx, usecasetest.MustPath(t, "/f/g"))

		// Assert
		if domain.KindOf(err) != domain.KindNotFound || !strings.Contains(err.Error(), "/f はフォルダではありません") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("同じ名前が 2 つなら KindAmbiguous で、詳細が 2 行", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver(t)
		root, _ := r.Root(ctx)
		d.AddFolder(root.ID, "dup")
		d.AddFolder(root.ID, "dup")

		// Act
		_, err := r.Resolve(ctx, usecasetest.MustPath(t, "/dup"))

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
		r, d, _ := newTestResolver(t)
		root, _ := r.Root(ctx)
		d.AddFolder(root.ID, "a")

		// Act
		got, created, err := r.EnsureFolder(ctx, usecasetest.MustPath(t, "/a/b/c"))
		_, again, err2 := r.EnsureFolder(ctx, usecasetest.MustPath(t, "/a/b/c"))

		// Assert
		if err != nil || err2 != nil {
			t.Fatal(err, err2)
		}
		want := []domain.RemotePath{usecasetest.MustPath(t, "/a/b"), usecasetest.MustPath(t, "/a/b/c")}
		if !reflect.DeepEqual(created, want) || got.Path != "/a/b/c" || got.Name != "c" {
			t.Errorf("created=%v got=%+v", created, got)
		}
		if len(again) != 0 {
			t.Errorf("again = %v", again)
		}
	})

	t.Run("EnsureFolder は途中のファイルで KindConflict", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver(t)
		root, _ := r.Root(ctx)
		d.AddFile(root.ID, "f", nil)

		// Act
		_, _, err := r.EnsureFolder(ctx, usecasetest.MustPath(t, "/f/g"))

		// Assert
		if domain.KindOf(err) != domain.KindConflict || !strings.Contains(err.Error(), "/f はファイルです") {
			t.Errorf("err = %v", err)
		}
	})
}

// lockWritesState は、ロックを取るとき、ほかのプロセスが先に終えた状況（state に ID が入る）を作る。
type lockWritesState struct {
	state *usecasetest.State
	id    string
}

func (l *lockWritesState) Lock(context.Context) (func(), error) {
	l.state.ID = l.id
	return func() {}, nil
}

func TestResolverRace(t *testing.T) {
	t.Run("ロックを待つ間にほかが保存していたら、探さず作らずそれを使う", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		s := &usecasetest.State{}
		other := d.AddRootFolder()
		r := usecase.NewResolver(d, s, &lockWritesState{state: s, id: other.ID})

		// Act
		root, err := r.Root(context.Background())

		// Assert
		if err != nil || root.ID != other.ID {
			t.Fatalf("root=%+v err=%v", root, err)
		}
		if d.CallCount("CreateRootFolder") != 0 || d.CallCount("FindRootFolders") != 0 {
			t.Errorf("create=%d find=%d", d.CallCount("CreateRootFolder"), d.CallCount("FindRootFolders"))
		}
	})

	t.Run("複数の goroutine から Resolve しても競合しない", func(t *testing.T) {
		// Arrange
		r, d, _ := newTestResolver(t)
		root, _ := r.Root(context.Background())
		d.AddFolder(root.ID, "a")
		var wg sync.WaitGroup
		errs := make(chan error, 8)

		// Act
		for range 8 {
			wg.Go(func() {
				_, err := r.Resolve(context.Background(), usecasetest.MustPath(t, "/a"))
				_ = r.Warnings()
				errs <- err
			})
		}
		wg.Wait()
		close(errs)

		// Assert
		for err := range errs {
			if err != nil {
				t.Error(err)
			}
		}
	})
}

func TestRemote(t *testing.T) {
	t.Run("Connect は Drive と Resolver を返す", func(t *testing.T) {
		// Arrange
		d := usecasetest.NewDrive(t)
		remote := usecase.Remote{
			Open:   func(context.Context) (usecase.Drive, error) { return d, nil },
			State:  &usecasetest.State{},
			Locker: &usecasetest.Locker{},
		}

		// Act
		s, err := remote.Connect(context.Background())

		// Assert
		if err != nil || s.Drive != usecase.Drive(d) || s.Resolver == nil {
			t.Errorf("s=%+v err=%v", s, err)
		}
	})
}
