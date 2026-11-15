package transfer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/domain/plan"
	"github.com/wadoyoka/gdrive-cli/internal/usecase/usecasetest"
)

const nativeDocMime = "application/vnd.google-apps.document"

// newDownloadFixture は、ルートの下に /src フォルダがある偽の Drive と、手元の保存先ディレクトリを返す。
func newDownloadFixture(t *testing.T) (*usecasetest.Drive, domain.Entry, string) {
	t.Helper()
	d := usecasetest.NewDrive(t)
	root := d.AddRootFolder()
	src := d.AddFolder(root.ID, "src")
	return d, src, t.TempDir()
}

func readLocal(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// listTree は、dir の下の全てのパス（dir からの相対）を並べて返す。
func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel != "." {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func TestDownload(t *testing.T) {
	t.Run("ファイルの中身が一致する", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		f := d.AddFile(src.ID, "a.mp4", []byte("video-data"))
		u := Download{Remote: usecasetest.NewRemote(d)}

		// Act
		res, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src/a.mp4")}, DestDir: dir})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := readLocal(t, filepath.Join(dir, "a.mp4")); got != "video-data" {
			t.Errorf("content=%q", got)
		}
		want := Item{LocalPath: filepath.Join(dir, "a.mp4"), RemotePath: "/src/a.mp4", ID: f.ID, Type: domain.TypeFile, Size: 10, WebViewLink: f.WebViewLink, Action: plan.ActionCreate}
		if len(res.Items) != 1 || res.Items[0] != want {
			t.Errorf("items=%+v", res.Items)
		}
		t.Run("権限は 0644", func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("Windows ではファイルの権限の数字が効かない")
			}
			if info, _ := os.Stat(filepath.Join(dir, "a.mp4")); info.Mode().Perm() != 0o644 {
				t.Errorf("mode=%v", info.Mode())
			}
		})
	})

	t.Run("フォルダの木をそのまま作る", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		sub := d.AddFolder(src.ID, "sub")
		d.AddFolder(sub.ID, "empty")
		d.AddFile(src.ID, "a.txt", []byte("a"))
		d.AddFile(sub.ID, "b.txt", []byte("b"))
		u := Download{Remote: usecasetest.NewRemote(d), Concurrency: 2}

		// Act
		res, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src")}, DestDir: dir})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"src", "src/a.txt", "src/sub", "src/sub/b.txt", "src/sub/empty"}
		if got := listTree(t, dir); !slices.Equal(got, want) {
			t.Errorf("tree=%v", got)
		}
		if readLocal(t, filepath.Join(dir, "src", "sub", "b.txt")) != "b" {
			t.Errorf("b.txt の中身が違う")
		}
		wantPaths := []string{"/src", "/src/a.txt", "/src/sub", "/src/sub/b.txt", "/src/sub/empty"}
		if got := remotePaths(res.Items); !slices.Equal(got, wantPaths) {
			t.Errorf("paths=%v", got)
		}
	})

	t.Run("手元に同じ名前があれば何も書かない", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a.txt", []byte("new"))
		d.AddFile(src.ID, "b.txt", []byte("b"))
		writeLocal(t, filepath.Join(dir, "a.txt"), "old")
		u := Download{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(context.Background(), DownloadInput{
			Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src/b.txt"), usecasetest.MustPath(t, "/src/a.txt")}, DestDir: dir,
		})

		// Assert
		if domain.KindOf(err) != domain.KindConflict {
			t.Fatalf("err=%v", err)
		}
		if got := detailsOf(err); !slices.Equal(got, []string{"/src/a.txt"}) {
			t.Errorf("details=%v", got)
		}
		if got := listTree(t, dir); !slices.Equal(got, []string{"a.txt"}) || readLocal(t, filepath.Join(dir, "a.txt")) != "old" {
			t.Errorf("tree=%v", got)
		}
		if d.CallCount("Download") != 0 {
			t.Errorf("Download=%d", d.CallCount("Download"))
		}
	})

	t.Run("--overwrite なら置き換える", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a.txt", []byte("new"))
		writeLocal(t, filepath.Join(dir, "a.txt"), "old")
		u := Download{Remote: usecasetest.NewRemote(d)}

		// Act
		res, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src/a.txt")}, DestDir: dir, Overwrite: true})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if readLocal(t, filepath.Join(dir, "a.txt")) != "new" || res.Items[0].Action != plan.ActionOverwrite {
			t.Errorf("items=%+v", res.Items)
		}
		if got := listTree(t, dir); !slices.Equal(got, []string{"a.txt"}) {
			t.Errorf("tree=%v", got)
		}
	})

	t.Run("--skip-existing なら飛ばす", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a.txt", []byte("new"))
		d.AddFile(src.ID, "b.txt", []byte("b"))
		writeLocal(t, filepath.Join(dir, "src", "a.txt"), "old")
		u := Download{Remote: usecasetest.NewRemote(d)}

		// Act
		res, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src")}, DestDir: dir, SkipExisting: true})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if readLocal(t, filepath.Join(dir, "src", "a.txt")) != "old" {
			t.Errorf("a.txt が書き換わった")
		}
		a := findItem(res.Items, "/src/a.txt")
		b := findItem(res.Items, "/src/b.txt")
		if a == nil || a.Action != plan.ActionSkip || b == nil || b.Action != plan.ActionCreate {
			t.Errorf("items=%+v", res.Items)
		}
		if d.CallCount("Download") != 1 {
			t.Errorf("Download=%d", d.CallCount("Download"))
		}
	})

	t.Run("../evil という名前は警告して飛ばし、保存先の外に書かない", func(t *testing.T) {
		// Arrange
		d, src, parent := newDownloadFixture(t)
		dest := filepath.Join(parent, "dest")
		mkdirLocal(t, dest)
		d.AddFile(src.ID, "../evil", []byte("x"))
		d.AddFile(src.ID, "a/b", []byte("x"))
		d.AddFile(src.ID, "ok.txt", []byte("ok"))
		inner := d.AddFolder(src.ID, "..")
		d.AddFile(inner.ID, "deep.txt", []byte("x"))
		u := Download{Remote: usecasetest.NewRemote(d)}

		// Act
		res, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src")}, DestDir: dest})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(dest, "..", "evil")); err == nil {
			t.Errorf("保存先の外に書いた")
		}
		if got := listTree(t, parent); !slices.Equal(got, []string{"dest", "dest/src", "dest/src/ok.txt"}) {
			t.Errorf("tree=%v", got)
		}
		for _, w := range []string{"/src/../evil", "/src/a/b", "/src/.."} {
			if !slices.Contains(res.Warnings, "手元で使えない名前なので飛ばしました: "+w) {
				t.Errorf("警告がない: %s in %v", w, res.Warnings)
			}
		}
		if d.CallCount("Download") != 1 {
			t.Errorf("Download=%d", d.CallCount("Download"))
		}
	})

	t.Run("Windows の決まりを効かせると、Windows で使えない名前は警告して飛ばし、ほかは受け取る", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a:b.txt", []byte("x"))
		d.AddFile(src.ID, "CON.txt", []byte("x"))
		d.AddFile(src.ID, "ok.txt", []byte("ok"))
		u := Download{Remote: usecasetest.NewRemote(d), WindowsNames: true}

		// Act
		res, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src")}, DestDir: dir})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := listTree(t, dir); !slices.Equal(got, []string{"src", "src/ok.txt"}) {
			t.Errorf("tree=%v", got)
		}
		for _, w := range []string{"/src/a:b.txt", "/src/CON.txt"} {
			if !slices.Contains(res.Warnings, "手元で使えない名前なので飛ばしました: "+w) {
				t.Errorf("警告がない: %s in %v", w, res.Warnings)
			}
		}
		if len(res.Warnings) != 2 {
			t.Errorf("warnings=%v", res.Warnings)
		}
	})

	t.Run("Windows の決まりを効かせなければ a:b.txt も受け取る", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows では a:b.txt というファイルを作れない")
		}
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a:b.txt", []byte("x"))
		u := Download{Remote: usecasetest.NewRemote(d), WindowsNames: false}

		// Act
		res, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src")}, DestDir: dir})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := listTree(t, dir); !slices.Equal(got, []string{"src", "src/a:b.txt"}) {
			t.Errorf("tree=%v", got)
		}
		if len(res.Warnings) != 0 {
			t.Errorf("warnings=%v", res.Warnings)
		}
	})

	t.Run("Google ドキュメントは警告して飛ばす", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		doc := d.AddFile(src.ID, "memo", nil)
		d.SetMimeType(doc.ID, nativeDocMime)
		d.AddFile(src.ID, "a.txt", []byte("a"))
		u := Download{Remote: usecasetest.NewRemote(d)}

		// Act
		res, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src")}, DestDir: dir})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := listTree(t, dir); !slices.Equal(got, []string{"src", "src/a.txt"}) {
			t.Errorf("tree=%v", got)
		}
		if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "/src/memo") {
			t.Errorf("warnings=%v", res.Warnings)
		}
	})

	t.Run("途中で失敗したら、完成品も .part も残らない", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a.txt", []byte("abcdef"))
		d.AddFile(src.ID, "b.txt", []byte("b"))
		d.FailDownload["a.txt"] = errors.New("通信が切れました")
		u := Download{Remote: usecasetest.NewRemote(d), Concurrency: 1}

		// Act
		res, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src")}, DestDir: dir})

		// Assert
		if domain.KindOf(err) != domain.KindUnexpected {
			t.Fatalf("err=%v", err)
		}
		if got := listTree(t, dir); !slices.Equal(got, []string{"src", "src/b.txt"}) {
			t.Errorf("tree=%v", got)
		}
		a := findItem(res.Items, "/src/a.txt")
		if a == nil || !strings.Contains(a.Error, "通信が切れました") {
			t.Errorf("items=%+v", res.Items)
		}
	})

	t.Run("--overwrite と --skip-existing は同時に使えない", func(t *testing.T) {
		// Arrange
		d, _, dir := newDownloadFixture(t)
		u := Download{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src")}, DestDir: dir, Overwrite: true, SkipExisting: true})

		// Assert
		if domain.KindOf(err) != domain.KindUsage {
			t.Errorf("err=%v", err)
		}
	})

	t.Run("保存先がないと KindNotFound、-p なら作る", func(t *testing.T) {
		// Arrange
		d, src, parent := newDownloadFixture(t)
		d.AddFile(src.ID, "a.txt", []byte("a"))
		dest := filepath.Join(parent, "x", "y")
		u := Download{Remote: usecasetest.NewRemote(d)}
		in := DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src/a.txt")}, DestDir: dest}

		// Act
		_, errNo := u.Run(context.Background(), in)
		existsAfterFail := len(listTree(t, parent))
		in.Parents = true
		_, errP := u.Run(context.Background(), in)

		// Assert
		if domain.KindOf(errNo) != domain.KindNotFound || existsAfterFail != 0 {
			t.Errorf("err=%v tree=%d", errNo, existsAfterFail)
		}
		if errP != nil || readLocal(t, filepath.Join(dest, "a.txt")) != "a" {
			t.Errorf("err=%v", errP)
		}
	})

	t.Run("保存先がファイルなら KindConflict", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a.txt", []byte("a"))
		f := filepath.Join(dir, "file")
		writeLocal(t, f, "x")
		u := Download{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src/a.txt")}, DestDir: f})

		// Assert
		if domain.KindOf(err) != domain.KindConflict {
			t.Errorf("err=%v", err)
		}
	})

	t.Run("既にあるシンボリックリンクは辿らず、衝突になる", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a.txt", []byte("new"))
		outside := filepath.Join(t.TempDir(), "target")
		writeLocal(t, outside, "keep")
		if err := os.Symlink(outside, filepath.Join(dir, "a.txt")); err != nil {
			if runtime.GOOS == "windows" {
				t.Skip("シンボリックリンクを作れない（Windows で権限がない）")
			}
			t.Fatal(err)
		}
		u := Download{Remote: usecasetest.NewRemote(d)}
		in := DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src/a.txt")}, DestDir: dir}

		// Act
		_, errConflict := u.Run(context.Background(), in)
		in.Overwrite = true
		_, errOverwrite := u.Run(context.Background(), in)

		// Assert
		if domain.KindOf(errConflict) != domain.KindConflict {
			t.Errorf("err=%v", errConflict)
		}
		if errOverwrite != nil {
			t.Fatal(errOverwrite)
		}
		if readLocal(t, outside) != "keep" {
			t.Errorf("リンク先に書いた")
		}
		if info, _ := os.Lstat(filepath.Join(dir, "a.txt")); info.Mode()&os.ModeSymlink != 0 || readLocal(t, filepath.Join(dir, "a.txt")) != "new" {
			t.Errorf("リンクそのものが置き換わっていない")
		}
	})

	t.Run("種類が違うと --overwrite でも衝突", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a.txt", []byte("a"))
		mkdirLocal(t, filepath.Join(dir, "a.txt"))
		u := Download{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src/a.txt")}, DestDir: dir, Overwrite: true})

		// Assert
		if domain.KindOf(err) != domain.KindConflict {
			t.Errorf("err=%v", err)
		}
	})

	t.Run("ドライブにないパスは KindNotFound で何も書かない", func(t *testing.T) {
		// Arrange
		d, _, parent := newDownloadFixture(t)
		dest := filepath.Join(parent, "dest")
		u := Download{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/nothing")}, DestDir: dest, Parents: true})

		// Assert
		if domain.KindOf(err) != domain.KindNotFound {
			t.Errorf("err=%v", err)
		}
		if got := listTree(t, parent); len(got) != 0 {
			t.Errorf("tree=%v", got)
		}
	})

	t.Run("進捗を知らせる", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a.txt", []byte("abcd"))
		u := Download{Remote: usecasetest.NewRemote(d)}
		var last int64 = -1
		progress := func(path string, done, total int64) {
			if path == "/src/a.txt" && total == 4 {
				last = done
			}
		}

		// Act
		_, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src/a.txt")}, DestDir: dir, Progress: progress})

		// Assert
		if err != nil || last != 4 {
			t.Errorf("err=%v last=%d", err, last)
		}
	})

	t.Run("中断したら何も残らず、残りは中断として残る", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a.txt", []byte("a"))
		u := Download{Remote: usecasetest.NewRemote(d)}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Act
		res, err := u.Run(ctx, DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src/a.txt")}, DestDir: dir})

		// Assert
		if domain.KindOf(err) != domain.KindUnexpected {
			t.Errorf("err=%v", err)
		}
		if got := listTree(t, dir); len(got) != 0 {
			t.Errorf("tree=%v items=%+v", got, res.Items)
		}
	})

	t.Run("同じ保存先になる名前が 2 つあると衝突で、何も書かない", func(t *testing.T) {
		cases := []struct {
			name          string
			first, second string
		}{
			{"同じ名前", "a.txt", "a.txt"},
			{"大文字小文字だけの違い", "Report.txt", "report.txt"},
			{"NFC と NFD", "caf\u00e9.txt", "cafe\u0301.txt"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				// Arrange
				d, src, dir := newDownloadFixture(t)
				d.AddFile(src.ID, c.first, []byte("1"))
				d.AddFile(src.ID, c.second, []byte("2"))
				u := Download{Remote: usecasetest.NewRemote(d)}

				// Act
				_, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src")}, DestDir: dir})

				// Assert
				if domain.KindOf(err) != domain.KindConflict || len(detailsOf(err)) != 2 {
					t.Fatalf("err=%v details=%v", err, detailsOf(err))
				}
				if d.CallCount("Download") != 0 || len(listTree(t, dir)) != 0 {
					t.Errorf("Download=%d tree=%v", d.CallCount("Download"), listTree(t, dir))
				}
			})
		}
	})

	t.Run("一部の失敗の案内は、--overwrite のときは --skip-existing を勧めない", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		d.AddFile(src.ID, "a.txt", []byte("abcdef"))
		d.FailDownload["a.txt"] = errors.New("通信が切れました")
		u := Download{Remote: usecasetest.NewRemote(d)}
		in := DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src")}, DestDir: dir}

		// Act
		_, errPlain := u.Run(context.Background(), in)
		in.Overwrite = true
		_, errOver := u.Run(context.Background(), in)

		// Assert
		if errPlain == nil || !strings.Contains(errPlain.Error(), "--skip-existing") {
			t.Errorf("errPlain=%v", errPlain)
		}
		if errOver == nil || strings.Contains(errOver.Error(), "--skip-existing") {
			t.Errorf("errOver=%v", errOver)
		}
	})

	t.Run("長い名前（日本語 80 文字）も受け取れる", func(t *testing.T) {
		// Arrange
		d, src, dir := newDownloadFixture(t)
		name := strings.Repeat("あ", 80) + ".txt" // 244 バイト
		d.AddFile(src.ID, name, []byte("long"))
		u := Download{Remote: usecasetest.NewRemote(d)}

		// Act
		_, err := u.Run(context.Background(), DownloadInput{Sources: []domain.RemotePath{usecasetest.MustPath(t, "/src/"+name)}, DestDir: dir})
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := listTree(t, dir); !slices.Equal(got, []string{name}) || readLocal(t, filepath.Join(dir, name)) != "long" {
			t.Errorf("tree=%v", got)
		}
	})
}
