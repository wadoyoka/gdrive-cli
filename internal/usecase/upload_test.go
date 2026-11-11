package usecase

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

// newUploadFixture は、ルートの下に /dest フォルダがある偽の Drive と、手元の作業ディレクトリを返す。
func newUploadFixture(t *testing.T) (*fakeDrive, domain.Entry, string) {
	t.Helper()
	d := newFakeDrive(t)
	root := d.addRootFolder()
	dest := d.addFolder(root.ID, "dest")
	return d, dest, t.TempDir()
}

// writeLocal は、手元にファイルを作る。途中のディレクトリも作る。
func writeLocal(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mkdirLocal(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
}

func remotePaths(items []TransferItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.RemotePath)
	}
	return out
}

func findItem(items []TransferItem, remotePath string) *TransferItem {
	for i := range items {
		if items[i].RemotePath == remotePath {
			return &items[i]
		}
	}
	return nil
}

// childNamed は、偽の Drive の parentID の下にある name を返す。なければ nil。2 つ以上なら t.Fatalf する。
func childNamed(t *testing.T, d *fakeDrive, parentID, name string) *domain.Entry {
	t.Helper()
	found, err := d.FindChildren(context.Background(), parentID, name)
	if err != nil {
		t.Fatal(err)
	}
	switch len(found) {
	case 0:
		return nil
	case 1:
		return &found[0]
	}
	t.Fatalf("%s の下に %s が %d 個ある", parentID, name, len(found))
	return nil
}

func detailsOf(err error) []string {
	var de *domain.Error
	if errors.As(err, &de) {
		return de.Details
	}
	return nil
}

func TestUpload(t *testing.T) {
	t.Run("1 ファイルを送る", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		writeLocal(t, filepath.Join(dir, "a.mp4"), "video")
		writeLocal(t, filepath.Join(dir, "x.html"), "<p>hi</p>")
		u := Upload{Remote: newRemote(d)}

		// Act
		resA, errA := u.Run(context.Background(), UploadInput{Sources: []string{filepath.Join(dir, "a.mp4")}, Dest: mustPath(t, "/dest")})
		resX, errX := u.Run(context.Background(), UploadInput{Sources: []string{filepath.Join(dir, "x.html")}, Dest: mustPath(t, "/dest")})

		// Assert
		if errA != nil || errX != nil {
			t.Fatalf("errA=%v errX=%v", errA, errX)
		}
		want := TransferItem{LocalPath: filepath.Join(dir, "a.mp4"), RemotePath: "/dest/a.mp4", Type: domain.TypeFile, Size: 5, Action: domain.ActionCreate}
		got := resA.Items
		if len(got) != 1 || got[0].ID == "" || got[0].WebViewLink == "" {
			t.Fatalf("items=%+v", got)
		}
		got[0].ID, got[0].WebViewLink = "", ""
		if got[0] != want {
			t.Errorf("got=%+v want=%+v", got[0], want)
		}
		a, x := childNamed(t, d, dest.ID, "a.mp4"), childNamed(t, d, dest.ID, "x.html")
		if a == nil || a.MimeType != "video/mp4" || x == nil || x.MimeType != "text/html" {
			t.Errorf("a=%+v x=%+v", a, x)
		}
		if len(resX.Items) != 1 || resX.Items[0].RemotePath != "/dest/x.html" || resX.Items[0].Action != domain.ActionCreate {
			t.Errorf("x items=%+v", resX.Items)
		}
	})

	t.Run("フォルダごと送り、.DS_Store は送らない", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		src := filepath.Join(dir, "dir")
		writeLocal(t, filepath.Join(src, "x.html"), "<p>")
		writeLocal(t, filepath.Join(src, "sub", "y.mp4"), "yy")
		mkdirLocal(t, filepath.Join(src, "empty"))
		writeLocal(t, filepath.Join(src, ".DS_Store"), "junk")
		u := Upload{Remote: newRemote(d)}

		// Act
		res, err := u.Run(context.Background(), UploadInput{Sources: []string{src}, Dest: mustPath(t, "/dest")})

		// Assert
		wantPaths := []string{"/dest/dir", "/dest/dir/empty", "/dest/dir/sub", "/dest/dir/sub/y.mp4", "/dest/dir/x.html"}
		if got := remotePaths(res.Items); err != nil || !slices.Equal(got, wantPaths) {
			t.Fatalf("paths=%v want=%v err=%v", got, wantPaths, err)
		}
		wantTypes := []domain.EntryType{domain.TypeFolder, domain.TypeFolder, domain.TypeFolder, domain.TypeFile, domain.TypeFile}
		for i, it := range res.Items {
			if it.Type != wantTypes[i] || it.Action != domain.ActionCreate || it.ID == "" || it.Error != "" {
				t.Errorf("item %d=%+v", i, it)
			}
		}
		remoteDir := childNamed(t, d, dest.ID, "dir")
		if remoteDir == nil {
			t.Fatal("dir ができていない")
		}
		sub := childNamed(t, d, remoteDir.ID, "sub")
		if sub == nil || childNamed(t, d, sub.ID, "y.mp4") == nil || childNamed(t, d, remoteDir.ID, "empty") == nil ||
			childNamed(t, d, remoteDir.ID, "x.html") == nil || childNamed(t, d, remoteDir.ID, ".DS_Store") != nil {
			t.Errorf("木が違う: children=%+v", d.childrenOf(remoteDir.ID))
		}
		if d.callCount("Upload") != 2 || d.callCount("CreateFolder") != 3 {
			t.Errorf("Upload=%d CreateFolder=%d", d.callCount("Upload"), d.callCount("CreateFolder"))
		}
	})

	t.Run("0 バイトのファイルも作る", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		writeLocal(t, filepath.Join(dir, "empty.txt"), "")
		u := Upload{Remote: newRemote(d)}

		// Act
		res, err := u.Run(context.Background(), UploadInput{Sources: []string{filepath.Join(dir, "empty.txt")}, Dest: mustPath(t, "/dest")})

		// Assert
		if err != nil || len(res.Items) != 1 || res.Items[0].Size != 0 || res.Items[0].Action != domain.ActionCreate {
			t.Fatalf("items=%+v err=%v", res.Items, err)
		}
		if e := childNamed(t, d, dest.ID, "empty.txt"); e == nil || e.Size != 0 {
			t.Errorf("remote=%+v", e)
		}
	})

	t.Run("衝突があれば何も送らない", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		d.addFile(dest.ID, "a.mp4", []byte("old"))
		writeLocal(t, filepath.Join(dir, "a.mp4"), "new")
		writeLocal(t, filepath.Join(dir, "b.mp4"), "b")
		writeLocal(t, filepath.Join(dir, "newdir", "c.mp4"), "c")
		u := Upload{Remote: newRemote(d)}

		// Act
		res, err := u.Run(context.Background(), UploadInput{
			Sources: []string{filepath.Join(dir, "a.mp4"), filepath.Join(dir, "b.mp4"), filepath.Join(dir, "newdir")},
			Dest:    mustPath(t, "/dest"),
		})

		// Assert
		if domain.KindOf(err) != domain.KindConflict || !strings.Contains(err.Error(), "同じ名前のものがあります") {
			t.Fatalf("err=%v", err)
		}
		if got := detailsOf(err); !slices.Equal(got, []string{"/dest/a.mp4"}) {
			t.Errorf("details=%v", got)
		}
		if d.callCount("Upload") != 0 || d.callCount("CreateFolder") != 0 {
			t.Errorf("Upload=%d CreateFolder=%d", d.callCount("Upload"), d.callCount("CreateFolder"))
		}
		if len(res.Items) != 0 {
			t.Errorf("items=%+v", res.Items)
		}
	})

	t.Run("--skip-existing で続きから送れる", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		old := d.addFile(dest.ID, "a.mp4", []byte("old"))
		writeLocal(t, filepath.Join(dir, "a.mp4"), "new")
		writeLocal(t, filepath.Join(dir, "b.mp4"), "b")
		u := Upload{Remote: newRemote(d)}

		// Act
		res, err := u.Run(context.Background(), UploadInput{
			Sources:      []string{filepath.Join(dir, "a.mp4"), filepath.Join(dir, "b.mp4")},
			Dest:         mustPath(t, "/dest"),
			SkipExisting: true,
		})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		a, b := findItem(res.Items, "/dest/a.mp4"), findItem(res.Items, "/dest/b.mp4")
		if a == nil || a.Action != domain.ActionSkip || a.ID != old.ID || b == nil || b.Action != domain.ActionCreate {
			t.Errorf("items=%+v", res.Items)
		}
		if d.callCount("Upload") != 1 {
			t.Errorf("Upload=%d", d.callCount("Upload"))
		}
	})

	t.Run("既にあるフォルダに合流する", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		remoteDir := d.addFolder(dest.ID, "dir")
		d.addFile(remoteDir.ID, "a.mp4", []byte("old"))
		remoteSub := d.addFolder(remoteDir.ID, "sub")
		src := filepath.Join(dir, "dir")
		writeLocal(t, filepath.Join(src, "a.mp4"), "new")
		writeLocal(t, filepath.Join(src, "b.mp4"), "b")
		writeLocal(t, filepath.Join(src, "sub", "c.mp4"), "c")
		u := Upload{Remote: newRemote(d)}

		// Act
		res, err := u.Run(context.Background(), UploadInput{Sources: []string{src}, Dest: mustPath(t, "/dest"), SkipExisting: true})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]domain.Action{
			"/dest/dir": domain.ActionMerge, "/dest/dir/a.mp4": domain.ActionSkip, "/dest/dir/b.mp4": domain.ActionCreate,
			"/dest/dir/sub": domain.ActionMerge, "/dest/dir/sub/c.mp4": domain.ActionCreate,
		}
		if len(res.Items) != len(want) {
			t.Fatalf("items=%+v", res.Items)
		}
		for _, it := range res.Items {
			if want[it.RemotePath] != it.Action {
				t.Errorf("%s: action=%q want=%q", it.RemotePath, it.Action, want[it.RemotePath])
			}
		}
		if it := findItem(res.Items, "/dest/dir"); it.ID != remoteDir.ID {
			t.Errorf("dir の ID=%q want=%q", it.ID, remoteDir.ID)
		}
		if d.callCount("CreateFolder") != 0 {
			t.Errorf("CreateFolder=%d", d.callCount("CreateFolder"))
		}
		childNamed(t, d, dest.ID, "dir") // 二重にできていれば t.Fatalf する
		if childNamed(t, d, remoteDir.ID, "b.mp4") == nil || childNamed(t, d, remoteSub.ID, "c.mp4") == nil {
			t.Errorf("合流先に送られていない")
		}
	})

	t.Run("種類が違うと --skip-existing でも衝突", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		d.addFolder(dest.ID, "a.mp4")
		d.addFile(dest.ID, "photos", []byte("x"))
		writeLocal(t, filepath.Join(dir, "a.mp4"), "a")
		writeLocal(t, filepath.Join(dir, "photos", "p.png"), "p")
		writeLocal(t, filepath.Join(dir, "b.mp4"), "b")
		u := Upload{Remote: newRemote(d)}

		// Act
		_, err := u.Run(context.Background(), UploadInput{
			Sources:      []string{filepath.Join(dir, "a.mp4"), filepath.Join(dir, "photos"), filepath.Join(dir, "b.mp4")},
			Dest:         mustPath(t, "/dest"),
			SkipExisting: true,
		})

		// Assert
		if domain.KindOf(err) != domain.KindConflict {
			t.Fatalf("err=%v", err)
		}
		if got := detailsOf(err); !slices.Equal(got, []string{"/dest/a.mp4", "/dest/photos"}) {
			t.Errorf("details=%v", got)
		}
		if d.callCount("Upload") != 0 || d.callCount("CreateFolder") != 0 {
			t.Errorf("Upload=%d CreateFolder=%d", d.callCount("Upload"), d.callCount("CreateFolder"))
		}
	})

	t.Run("送り先がないと KindNotFound、-p なら作る", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		writeLocal(t, filepath.Join(dir, "a.mp4"), "a")
		u := Upload{Remote: newRemote(d)}
		in := UploadInput{Sources: []string{filepath.Join(dir, "a.mp4")}, Dest: mustPath(t, "/dest/new/deep")}

		// Act
		_, errMissing := u.Run(context.Background(), in)
		uploadsBefore, foldersBefore := d.callCount("Upload"), d.callCount("CreateFolder")
		in.Parents = true
		res, errParents := u.Run(context.Background(), in)

		// Assert
		if domain.KindOf(errMissing) != domain.KindNotFound || uploadsBefore != 0 || foldersBefore != 0 {
			t.Errorf("err=%v Upload=%d CreateFolder=%d", errMissing, uploadsBefore, foldersBefore)
		}
		if errParents != nil || len(res.Items) != 1 || res.Items[0].RemotePath != "/dest/new/deep/a.mp4" || res.Items[0].Action != domain.ActionCreate {
			t.Fatalf("items=%+v err=%v", res.Items, errParents)
		}
		newDir := childNamed(t, d, dest.ID, "new")
		if newDir == nil {
			t.Fatal("new ができていない")
		}
		deep := childNamed(t, d, newDir.ID, "deep")
		if deep == nil || childNamed(t, d, deep.ID, "a.mp4") == nil {
			t.Errorf("deep=%+v", deep)
		}
	})

	t.Run("送り先がファイルなら KindConflict", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		d.addFile(dest.ID, "file.txt", []byte("x"))
		writeLocal(t, filepath.Join(dir, "a.mp4"), "a")
		u := Upload{Remote: newRemote(d)}

		// Act
		_, err := u.Run(context.Background(), UploadInput{Sources: []string{filepath.Join(dir, "a.mp4")}, Dest: mustPath(t, "/dest/file.txt")})

		// Assert
		if domain.KindOf(err) != domain.KindConflict || d.callCount("Upload") != 0 {
			t.Errorf("err=%v Upload=%d", err, d.callCount("Upload"))
		}
	})

	t.Run("手元のパスがないと KindNotFound で、何も送らない", func(t *testing.T) {
		// Arrange
		d, _, dir := newUploadFixture(t)
		writeLocal(t, filepath.Join(dir, "a.mp4"), "a")
		u := Upload{Remote: newRemote(d)}

		// Act
		_, err := u.Run(context.Background(), UploadInput{
			Sources: []string{filepath.Join(dir, "a.mp4"), filepath.Join(dir, "nope.mp4")},
			Dest:    mustPath(t, "/missing"),
			Parents: true,
		})

		// Assert
		if domain.KindOf(err) != domain.KindNotFound || !strings.Contains(err.Error(), "nope.mp4") {
			t.Errorf("err=%v", err)
		}
		if d.callCount("Upload") != 0 || d.callCount("CreateFolder") != 0 {
			t.Errorf("Upload=%d CreateFolder=%d", d.callCount("Upload"), d.callCount("CreateFolder"))
		}
	})

	t.Run("シンボリックリンクは警告して飛ばす", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		src := filepath.Join(dir, "dir")
		writeLocal(t, filepath.Join(src, "a.mp4"), "a")
		inner := filepath.Join(src, "link.mp4")
		top := filepath.Join(dir, "toplink")
		if err := os.Symlink("a.mp4", inner); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(src, top); err != nil {
			t.Fatal(err)
		}
		u := Upload{Remote: newRemote(d)}

		// Act
		res, err := u.Run(context.Background(), UploadInput{Sources: []string{src, top}, Dest: mustPath(t, "/dest")})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := remotePaths(res.Items); !slices.Equal(got, []string{"/dest/dir", "/dest/dir/a.mp4"}) {
			t.Errorf("paths=%v", got)
		}
		for _, p := range []string{inner, top} {
			if !slices.Contains(res.Warnings, "シンボリックリンクは飛ばしました: "+p) {
				t.Errorf("warnings=%v に %s がない", res.Warnings, p)
			}
		}
		if d.callCount("Upload") != 1 || childNamed(t, d, dest.ID, "toplink") != nil {
			t.Errorf("Upload=%d", d.callCount("Upload"))
		}
	})

	t.Run("NFD のファイル名は NFC で送る", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		nfd, nfc := "ダ", "ダ" // ダ
		writeLocal(t, filepath.Join(dir, nfd, nfd+".mp4"), "v")
		u := Upload{Remote: newRemote(d)}

		// Act
		res, err := u.Run(context.Background(), UploadInput{Sources: []string{filepath.Join(dir, nfd)}, Dest: mustPath(t, "/dest")})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got, want := remotePaths(res.Items), []string{"/dest/" + nfc, "/dest/" + nfc + "/" + nfc + ".mp4"}; !slices.Equal(got, want) {
			t.Errorf("paths=%q want=%q", got, want)
		}
		folder := childNamed(t, d, dest.ID, nfc)
		if folder == nil || childNamed(t, d, folder.ID, nfc+".mp4") == nil || childNamed(t, d, dest.ID, nfd) != nil {
			t.Errorf("NFC の名前で作られていない: %+v", d.childrenOf(dest.ID))
		}
	})

	t.Run("一部が失敗しても残りは送る", func(t *testing.T) {
		// Arrange
		d, dest, dir := newUploadFixture(t)
		d.failUpload["b.mp4"] = errors.New("通信が切れました")
		for _, n := range []string{"a.mp4", "b.mp4", "c.mp4"} {
			writeLocal(t, filepath.Join(dir, n), n)
		}
		u := Upload{Remote: newRemote(d)}

		// Act
		res, err := u.Run(context.Background(), UploadInput{
			Sources: []string{filepath.Join(dir, "a.mp4"), filepath.Join(dir, "b.mp4"), filepath.Join(dir, "c.mp4")},
			Dest:    mustPath(t, "/dest"),
		})

		// Assert
		if domain.KindOf(err) != domain.KindUnexpected || !strings.Contains(err.Error(), "1 件のアップロードに失敗しました") {
			t.Fatalf("err=%v", err)
		}
		a, b, c := findItem(res.Items, "/dest/a.mp4"), findItem(res.Items, "/dest/b.mp4"), findItem(res.Items, "/dest/c.mp4")
		if a == nil || a.Action != domain.ActionCreate || a.Error != "" || a.ID == "" || c == nil || c.Error != "" || c.ID == "" {
			t.Errorf("a=%+v c=%+v", a, c)
		}
		if b == nil || !strings.Contains(b.Error, "通信が切れました") || b.ID != "" {
			t.Errorf("b=%+v", b)
		}
		if got := detailsOf(err); len(got) != 1 || !strings.Contains(got[0], "/dest/b.mp4") {
			t.Errorf("details=%v", got)
		}
		if childNamed(t, d, dest.ID, "a.mp4") == nil || childNamed(t, d, dest.ID, "c.mp4") == nil {
			t.Errorf("a と c が送られていない")
		}
	})

	t.Run("同じ名前のソースが 2 つなら衝突", func(t *testing.T) {
		// Arrange
		d, _, dir := newUploadFixture(t)
		writeLocal(t, filepath.Join(dir, "one", "a.mp4"), "1")
		writeLocal(t, filepath.Join(dir, "two", "a.mp4"), "2")
		u := Upload{Remote: newRemote(d)}
		sources := []string{filepath.Join(dir, "one", "a.mp4"), filepath.Join(dir, "two", "a.mp4")}

		// Act
		_, errExisting := u.Run(context.Background(), UploadInput{Sources: sources, Dest: mustPath(t, "/dest")})
		_, errParents := u.Run(context.Background(), UploadInput{Sources: sources, Dest: mustPath(t, "/new"), Parents: true})

		// Assert
		if domain.KindOf(errExisting) != domain.KindConflict || !slices.Equal(detailsOf(errExisting), []string{"/dest/a.mp4"}) {
			t.Errorf("err=%v details=%v", errExisting, detailsOf(errExisting))
		}
		if domain.KindOf(errParents) != domain.KindConflict {
			t.Errorf("-p: err=%v", errParents)
		}
		if d.callCount("Upload") != 0 || d.callCount("CreateFolder") != 0 {
			t.Errorf("Upload=%d CreateFolder=%d", d.callCount("Upload"), d.callCount("CreateFolder"))
		}
	})

	t.Run("決めた本数まで同時に送り、進捗を知らせる", func(t *testing.T) {
		// Arrange
		fd, dest, dir := newUploadFixture(t)
		g := &gatedDrive{fakeDrive: fd, want: 3, ready: make(chan struct{})}
		var sources []string
		for i := range 12 {
			p := filepath.Join(dir, string(rune('a'+i))+".mp4")
			writeLocal(t, p, strings.Repeat("x", i))
			sources = append(sources, p)
		}
		var mu sync.Mutex
		finished := map[string]bool{}
		progress := func(path string, done, total int64) {
			mu.Lock()
			defer mu.Unlock()
			if done == total {
				finished[path] = true
			}
		}
		u := Upload{Remote: newRemote(g), Concurrency: 3}

		// Act
		res, err := u.Run(context.Background(), UploadInput{Sources: sources, Dest: mustPath(t, "/dest"), Progress: progress})

		// Assert
		if err != nil || len(res.Items) != 12 {
			t.Fatalf("items=%d err=%v", len(res.Items), err)
		}
		if g.maxInFlight() != 3 {
			t.Errorf("同時に送った数=%d want=3", g.maxInFlight())
		}
		if len(fd.childrenOf(dest.ID)) != 12 || len(finished) != 12 {
			t.Errorf("送れた数=%d 進捗が 100%% になった数=%d", len(fd.childrenOf(dest.ID)), len(finished))
		}
	})

	t.Run("中断したら、残りのファイルは送らない", func(t *testing.T) {
		// Arrange
		fd, _, dir := newUploadFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c := &cancelDrive{fakeDrive: fd, cancel: cancel}
		var sources []string
		for _, n := range []string{"a.mp4", "b.mp4", "c.mp4"} {
			writeLocal(t, filepath.Join(dir, n), n)
			sources = append(sources, filepath.Join(dir, n))
		}
		u := Upload{Remote: newRemote(c), Concurrency: 1}

		// Act
		res, err := u.Run(ctx, UploadInput{Sources: sources, Dest: mustPath(t, "/dest")})

		// Assert
		if domain.KindOf(err) != domain.KindUnexpected || !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
		if fd.callCount("Upload") != 1 {
			t.Errorf("Upload=%d", fd.callCount("Upload"))
		}
		failed := 0
		for _, it := range res.Items {
			if it.Error != "" {
				failed++
			}
		}
		if len(res.Items) != 3 || failed != 2 {
			t.Errorf("items=%+v", res.Items)
		}
	})

	t.Run("フォルダはロックを持った状態で作り、ロックを入れ子にしない", func(t *testing.T) {
		// Arrange
		fd, _, dir := newUploadFixture(t)
		l := &heldLocker{}
		ld := &lockCheckDrive{fakeDrive: fd, locker: l}
		writeLocal(t, filepath.Join(dir, "dir", "sub", "x.mp4"), "x")
		u := Upload{Remote: Remote{Open: func(context.Context) (Drive, error) { return ld, nil }, State: &fakeState{}, Locker: l}}

		// Act
		_, err := u.Run(context.Background(), UploadInput{Sources: []string{filepath.Join(dir, "dir")}, Dest: mustPath(t, "/dest/new"), Parents: true})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if fd.callCount("CreateFolder") != 3 || ld.unlockedCreates != 0 || l.nested != 0 {
			t.Errorf("CreateFolder=%d ロックの外で作った数=%d 入れ子=%d", fd.callCount("CreateFolder"), ld.unlockedCreates, l.nested)
		}
		if ld.lockedUploads != 0 {
			t.Errorf("ロックを持ったまま送った数=%d", ld.lockedUploads)
		}
	})

	t.Run("計画のあとにほかの実行がフォルダを作っていたら、二重に作らず合流する", func(t *testing.T) {
		// Arrange
		fd, dest, dir := newUploadFixture(t)
		ld := &lockCheckDrive{fakeDrive: fd, locker: &heldLocker{}, injectName: "dir"}
		writeLocal(t, filepath.Join(dir, "dir", "x.mp4"), "x")
		u := Upload{Remote: newRemote(ld)}

		// Act
		res, err := u.Run(context.Background(), UploadInput{Sources: []string{filepath.Join(dir, "dir")}, Dest: mustPath(t, "/dest")})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		remoteDir := childNamed(t, fd, dest.ID, "dir") // 二重にできていれば t.Fatalf する
		it := findItem(res.Items, "/dest/dir")
		if remoteDir == nil || it == nil || it.Action != domain.ActionMerge || it.ID != remoteDir.ID || fd.callCount("CreateFolder") != 0 {
			t.Fatalf("item=%+v remote=%+v CreateFolder=%d", it, remoteDir, fd.callCount("CreateFolder"))
		}
		if childNamed(t, fd, remoteDir.ID, "x.mp4") == nil {
			t.Errorf("合流先に送られていない")
		}
	})

	t.Run("フォルダを作れなければ、その中は送らず、ほかは送る", func(t *testing.T) {
		// Arrange
		fd, dest, dir := newUploadFixture(t)
		ld := &lockCheckDrive{fakeDrive: fd, locker: &heldLocker{}, failFolder: "bad"}
		writeLocal(t, filepath.Join(dir, "bad", "x.mp4"), "x")
		writeLocal(t, filepath.Join(dir, "b.mp4"), "b")
		u := Upload{Remote: newRemote(ld)}

		// Act
		res, err := u.Run(context.Background(), UploadInput{Sources: []string{filepath.Join(dir, "bad"), filepath.Join(dir, "b.mp4")}, Dest: mustPath(t, "/dest")})

		// Assert
		if domain.KindOf(err) != domain.KindUnexpected || !strings.Contains(err.Error(), "2 件") {
			t.Fatalf("err=%v", err)
		}
		bad, x, b := findItem(res.Items, "/dest/bad"), findItem(res.Items, "/dest/bad/x.mp4"), findItem(res.Items, "/dest/b.mp4")
		if bad == nil || bad.Error == "" || x == nil || x.Error == "" || b == nil || b.Error != "" || b.ID == "" {
			t.Errorf("items=%+v", res.Items)
		}
		if fd.callCount("Upload") != 1 || childNamed(t, fd, dest.ID, "b.mp4") == nil {
			t.Errorf("Upload=%d", fd.callCount("Upload"))
		}
	})
}

// gatedDrive は、Upload が同時に何本走ったかを数える偽の Drive。
// 最初の want 本は、want 本がそろうまで待つ（そろわなければ 5 秒で諦める）。順に 1 本ずつ送る実装なら、最大が want に届かない。
type gatedDrive struct {
	*fakeDrive
	want  int
	ready chan struct{}

	mu       sync.Mutex
	arrived  int
	inFlight int
	max      int
}

func (g *gatedDrive) Upload(ctx context.Context, parentID, name, mimeType string, body io.Reader, size int64, progress func(int64)) (domain.Entry, error) {
	g.mu.Lock()
	g.arrived++
	g.inFlight++
	g.max = max(g.max, g.inFlight)
	if g.arrived == g.want {
		close(g.ready)
	}
	g.mu.Unlock()
	select {
	case <-g.ready:
	case <-time.After(5 * time.Second):
	}
	e, err := g.fakeDrive.Upload(ctx, parentID, name, mimeType, body, size, progress)
	g.mu.Lock()
	g.inFlight--
	g.mu.Unlock()
	return e, err
}

func (g *gatedDrive) maxInFlight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.max
}

// cancelDrive は、最初の Upload で ctx を取り消す偽の Drive（その Upload 自体は成功させる）。
type cancelDrive struct {
	*fakeDrive
	cancel context.CancelFunc
}

func (c *cancelDrive) Upload(ctx context.Context, parentID, name, mimeType string, body io.Reader, size int64, progress func(int64)) (domain.Entry, error) {
	c.cancel()
	return c.fakeDrive.Upload(ctx, parentID, name, mimeType, body, size, progress)
}

// heldLocker は、ロックを持っているかと、持ったまま取り直そうとした回数（入れ子）を記録する Locker。
type heldLocker struct {
	mu     sync.Mutex
	held   bool
	nested int
}

func (l *heldLocker) Lock(context.Context) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held {
		l.nested++
	}
	l.held = true
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.held = false
	}, nil
}

func (l *heldLocker) isHeld() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held
}

// lockCheckDrive は、CreateFolder と Upload がロックの中で呼ばれたかを数える偽の Drive。
// injectName に名前を入れると、その名前を 2 回目に探したときに、ほかの実行が作ったかのようにフォルダを足す。
// failFolder に名前を入れると、その名前の CreateFolder が失敗する。
type lockCheckDrive struct {
	*fakeDrive
	locker     *heldLocker
	injectName string
	failFolder string

	mu              sync.Mutex
	finds           int
	unlockedCreates int
	lockedUploads   int
}

func (d *lockCheckDrive) FindChildren(ctx context.Context, parentID, name string) ([]domain.Entry, error) {
	d.mu.Lock()
	if name == d.injectName {
		d.finds++
		if d.finds == 2 {
			d.addFolder(parentID, name)
		}
	}
	d.mu.Unlock()
	return d.fakeDrive.FindChildren(ctx, parentID, name)
}

func (d *lockCheckDrive) CreateFolder(ctx context.Context, parentID, name string) (domain.Entry, error) {
	d.mu.Lock()
	if !d.locker.isHeld() {
		d.unlockedCreates++
	}
	d.mu.Unlock()
	if name == d.failFolder {
		return domain.Entry{}, errors.New("フォルダを作れませんでした")
	}
	return d.fakeDrive.CreateFolder(ctx, parentID, name)
}

func (d *lockCheckDrive) Upload(ctx context.Context, parentID, name, mimeType string, body io.Reader, size int64, progress func(int64)) (domain.Entry, error) {
	d.mu.Lock()
	if d.locker.isHeld() {
		d.lockedUploads++
	}
	d.mu.Unlock()
	return d.fakeDrive.Upload(ctx, parentID, name, mimeType, body, size, progress)
}
