package repository_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/repository"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

const e2eBigSize = 20 << 20 // 16 MiB の分割を 2 回以上起こす大きさ

func mustRemote(t *testing.T, s string) domain.RemotePath {
	t.Helper()
	p, err := domain.ParseRemotePath(s)
	if err != nil {
		t.Fatalf("ParseRemotePath(%q): %v", s, err)
	}
	return p
}

func sha256File(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // テストの一時ファイル
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

func writeRandomFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path) //nolint:gosec // テストの一時ファイル
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.CopyN(f, rand.Reader, size); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// e2eDeleteGuard は、workID を作業用フォルダとして消してよいかを確かめる。消してはいけなければ、その理由を返す。
// ID が空でないこと、CLI のルートフォルダではないこと、名前が e2e- で始まることを、Drive に問い合わせて確かめる。
func e2eDeleteGuard(ctx context.Context, remote usecase.Remote, workID string) string {
	if workID == "" {
		return "ID が空"
	}
	s, err := remote.Connect(ctx)
	if err != nil {
		return fmt.Sprintf("Drive に接続できない: %v", err)
	}
	root, err := s.Resolver.Root(ctx)
	if err != nil {
		return fmt.Sprintf("ルートフォルダを確かめられない: %v", err)
	}
	if workID == root.ID {
		return "ルートフォルダの ID と同じ"
	}
	e, err := s.Drive.Get(ctx, workID)
	if err != nil {
		return fmt.Sprintf("作業用フォルダを確かめられない: %v", err)
	}
	if !strings.HasPrefix(e.Name, "e2e-") {
		return fmt.Sprintf("名前 %q が e2e- で始まらない", e.Name)
	}
	return ""
}

// TestE2E は、実際の Drive に対する通し試験。GDRIVE_CLI_E2E=1 のときだけ動く。
// 事前に init と login を済ませておくこと。ルートフォルダの下に /e2e-<時刻> を作り、終わると消す。
func TestE2E(t *testing.T) {
	if os.Getenv("GDRIVE_CLI_E2E") != "1" {
		t.Skip("GDRIVE_CLI_E2E=1 のときだけ実行する")
	}
	ctx := context.Background()

	dir, err := repository.DefaultConfigDir()
	if err != nil {
		t.Fatalf("DefaultConfigDir: %v", err)
	}
	auth := repository.NewAuth(dir)
	remote := usecase.Remote{
		Open:   repository.NewDriveOpener(auth),
		State:  repository.NewStateFile(dir),
		Locker: repository.NewLockFile(dir),
	}

	work := fmt.Sprintf("/e2e-%d", time.Now().UnixNano())
	workPath := mustRemote(t, work)
	made, err := usecase.Mkdir{Remote: remote}.Run(ctx, []domain.RemotePath{workPath}, true)
	if err != nil || len(made) != 1 {
		t.Fatalf("作業用フォルダ %s を作れない: %v", work, err)
	}
	workID := made[0].ID

	// 後片付け。削除は本体のコードには置かない決まり（archtest が本体の Files.Delete を禁じる）。
	// このファイルは _test.go なので archtest の対象外であり、ここだけは生の drive.Service で消してよい。
	// 消すのは、いま作った作業用フォルダの ID だけ。ルートや、ほかのものは消さない。
	t.Cleanup(func() {
		cctx := context.Background()
		if reason := e2eDeleteGuard(cctx, remote, workID); reason != "" {
			t.Errorf("後片付け: %s を消さない（%s）。手で確かめてから消すこと", work, reason)
			return
		}
		ts, err := auth.TokenSource(cctx)
		if err != nil {
			t.Errorf("後片付け: トークンを得られない（%s を手で消すこと）: %v", work, err)
			return
		}
		svc, err := drive.NewService(cctx, option.WithTokenSource(ts))
		if err != nil {
			t.Errorf("後片付け: drive.NewService（%s を手で消すこと）: %v", work, err)
			return
		}
		if err := svc.Files.Delete(workID).Context(cctx).Do(); err != nil {
			t.Errorf("後片付け: %s を消せない: %v", work, err)
		}
	})

	// 手元の材料。src/ の中に、大きいファイル、HTML、0 バイト、空のフォルダを置く。
	local := t.TempDir()
	src := filepath.Join(local, "src")
	if err := os.MkdirAll(filepath.Join(src, "emptydir"), 0o750); err != nil {
		t.Fatal(err)
	}
	bigPath := filepath.Join(src, "big.bin")
	writeRandomFile(t, bigPath, e2eBigSize)
	bigSum := sha256File(t, bigPath)
	if err := os.WriteFile(filepath.Join(src, "page.html"), []byte("<html><body>e2e</body></html>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "empty.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	dirA := workPath.Join("a")
	up := usecase.Upload{Remote: remote}

	t.Run("mkdir -p で下の階層まで作る", func(t *testing.T) {
		// Act
		got, err := usecase.Mkdir{Remote: remote}.Run(ctx, []domain.RemotePath{dirA}, true)

		// Assert
		if err != nil || len(got) != 1 || got[0].Type != domain.TypeFolder {
			t.Fatalf("mkdir -p %s = %v, %v", dirA, got, err)
		}
	})

	t.Run("upload でファイルとフォルダを送る", func(t *testing.T) {
		// Act
		res, err := up.Run(ctx, usecase.UploadInput{Sources: []string{src}, Dest: dirA})
		// Assert
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		for _, it := range res.Items {
			if it.Error != "" {
				t.Errorf("%s: %s", it.RemotePath, it.Error)
			}
		}
		if len(res.Items) != 5 { // src, emptydir, big.bin, page.html, empty.txt
			t.Errorf("項目数 = %d, want 5: %+v", len(res.Items), res.Items)
		}
	})

	t.Run("ls -R の結果が送ったものと一致する", func(t *testing.T) {
		// Arrange
		want := map[string]struct {
			typ  domain.EntryType
			size int64
		}{
			dirA.Join("src").String():                   {domain.TypeFolder, 0},
			dirA.Join("src").Join("emptydir").String():  {domain.TypeFolder, 0},
			dirA.Join("src").Join("big.bin").String():   {domain.TypeFile, e2eBigSize},
			dirA.Join("src").Join("page.html").String(): {domain.TypeFile, 30},
			dirA.Join("src").Join("empty.txt").String(): {domain.TypeFile, 0},
		}

		// Act
		entries, err := usecase.List{Remote: remote}.Run(ctx, dirA, true)
		// Assert
		if err != nil {
			t.Fatalf("ls -R: %v", err)
		}
		if len(entries) != len(want) {
			t.Fatalf("件数 = %d, want %d: %+v", len(entries), len(want), entries)
		}
		for _, e := range entries {
			w, ok := want[e.Path]
			if !ok {
				t.Errorf("想定外の項目: %s", e.Path)
				continue
			}
			if e.Type != w.typ || e.Size != w.size {
				t.Errorf("%s = (%s, %d), want (%s, %d)", e.Path, e.Type, e.Size, w.typ, w.size)
			}
		}
	})

	t.Run("mv で大きいファイルの名前を変えて移す", func(t *testing.T) {
		// Arrange
		from := dirA.Join("src").Join("big.bin")
		to := dirA.Join("renamed.mp4")

		// Act
		items, err := usecase.Move{Remote: remote}.Run(ctx, []domain.RemotePath{from}, to)

		// Assert
		if err != nil || len(items) != 1 {
			t.Fatalf("mv = %v, %v", items, err)
		}
		if items[0].To != to.String() {
			t.Errorf("To = %q, want %q", items[0].To, to)
		}
	})

	// 手元も同じ形にそろえる（以後の upload で big.bin が新規扱いにならないように）。
	renamed := filepath.Join(local, "renamed.mp4")
	if err := os.Rename(bigPath, renamed); err != nil {
		t.Fatal(err)
	}

	t.Run("download して sha256 が一致する", func(t *testing.T) {
		// Arrange
		dl := t.TempDir()

		// Act
		res, err := usecase.Download{Remote: remote}.Run(ctx, usecase.DownloadInput{Sources: []domain.RemotePath{dirA}, DestDir: dl})
		// Assert
		if err != nil {
			t.Fatalf("download: %v", err)
		}
		for _, it := range res.Items {
			if it.Error != "" {
				t.Errorf("%s: %s", it.RemotePath, it.Error)
			}
		}
		if got := sha256File(t, filepath.Join(dl, "a", "renamed.mp4")); got != bigSum {
			t.Errorf("renamed.mp4 の sha256 が一致しない")
		}
		if fi, err := os.Stat(filepath.Join(dl, "a", "src", "empty.txt")); err != nil || fi.Size() != 0 {
			t.Errorf("empty.txt = %v, %v", fi, err)
		}
		if fi, err := os.Stat(filepath.Join(dl, "a", "src", "emptydir")); err != nil || !fi.IsDir() {
			t.Errorf("emptydir = %v, %v", fi, err)
		}
	})

	again := []string{src, renamed}

	t.Run("もう一度 upload すると衝突になる", func(t *testing.T) {
		// Act
		_, err := up.Run(ctx, usecase.UploadInput{Sources: again, Dest: dirA})

		// Assert
		if got := domain.KindOf(err); got != domain.KindConflict {
			t.Fatalf("KindOf = %v, want KindConflict (err=%v)", got, err)
		}
	})

	t.Run("--skip-existing を付けるとファイルはすべて飛ばされる", func(t *testing.T) {
		// Act
		res, err := up.Run(ctx, usecase.UploadInput{Sources: again, Dest: dirA, SkipExisting: true})
		// Assert
		if err != nil {
			t.Fatalf("upload --skip-existing: %v", err)
		}
		for _, it := range res.Items {
			want := domain.ActionSkip
			if it.Type == domain.TypeFolder {
				want = domain.ActionMerge // フォルダは既存のものに合流する
			}
			if it.Action != want || it.Error != "" {
				t.Errorf("%s: action=%s error=%q, want %s", it.RemotePath, it.Action, it.Error, want)
			}
		}
	})
}

// guardDrive は、e2eDeleteGuard を試すための偽の Drive。entries にあるものだけを Get で返す。
type guardDrive struct {
	usecase.Drive
	entries map[string]domain.Entry
}

func (d guardDrive) Get(_ context.Context, id string) (domain.Entry, error) {
	e, ok := d.entries[id]
	if !ok {
		return domain.Entry{}, domain.Errorf(domain.KindNotFound, "見つかりません")
	}
	return e, nil
}

type guardState struct{}

func (guardState) RootFolderID() (string, error) { return "root-1", nil }
func (guardState) SetRootFolderID(string) error  { return nil }

type guardLocker struct{}

func (guardLocker) Lock(context.Context) (func(), error) { return func() {}, nil }

func TestE2EDeleteGuard(t *testing.T) {
	drive := guardDrive{entries: map[string]domain.Entry{
		// ルートの判定が名前の判定とは別に効くことを確かめるため、ルートにも e2e- で始まる名前を付ける。
		"root-1": {ID: "root-1", Name: "e2e-root", Type: domain.TypeFolder},
		"w1":     {ID: "w1", Name: "e2e-123", Type: domain.TypeFolder},
		"w2":     {ID: "w2", Name: "videos", Type: domain.TypeFolder},
	}}
	remote := usecase.Remote{
		Open:   func(context.Context) (usecase.Drive, error) { return drive, nil },
		State:  guardState{},
		Locker: guardLocker{},
	}
	cases := []struct {
		name   string
		workID string
		ok     bool
	}{
		{"e2e- で始まる作業用フォルダなら消してよい", "w1", true},
		{"ID が空なら消さない", "", false},
		{"ルートフォルダの ID なら消さない", "root-1", false},
		{"名前が e2e- で始まらなければ消さない", "w2", false},
		{"Drive で確かめられなければ消さない", "nope", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			ctx := context.Background()

			// Act
			reason := e2eDeleteGuard(ctx, remote, tc.workID)

			// Assert
			if (reason == "") != tc.ok {
				t.Errorf("reason = %q, want ok = %v", reason, tc.ok)
			}
		})
	}
}
