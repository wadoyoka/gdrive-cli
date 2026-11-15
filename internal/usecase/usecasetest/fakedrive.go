// Package usecasetest は、usecase の各パッケージのテストが使い回す偽物（Drive・状態・ロック・
// 認証・設定ディレクトリ・拒否リスト）と、テストの小さな部品を持つ。テストからだけ使う。
package usecasetest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

// Drive は、メモリ上に木を持つ偽の Drive。
//
// 使い方:
//   - AddFolder / AddFile / AddRootFolder で木を作る（ID は id-1 から順に振る）。
//   - SetTrashed で項目にゴミ箱の印を付ける。
//   - CallCount で、メソッド名ごとの呼び出し回数を数える。
//   - FailUpload / FailDownload に名前を入れると、その名前の Upload / Download が失敗する。
//     Download は、中身の前半を w に書いてから失敗する（途中で切れた状況を作る）。
//   - SetMimeType で項目の MIME タイプを変える（Google ドキュメントなどを作る）。
//   - AddFile / AddFolder の名前は検証しない（"../evil" や "a/b" のような手元で使えない名前も置ける）。
//
// 契約: Get は KindNotFound を返す。CreateFolder / Upload は親がなければ、Move は移動先がなければ、
// KindNotFound を返す。Move は fromParentID が今の親と違えば KindUnexpected を返す。
// SetTrashed / SetMimeType / Remove は未知の ID なら t.Fatalf する。
//
// 本物の Drive との違い: ctx は見ない。作った直後に一覧へ出ない（結果整合）は再現しない。
// CreatedTime は、固定の時刻から 1 秒ずつ増やす。
type Drive struct {
	t            *testing.T
	mu           sync.Mutex
	items        map[string]*fakeItem
	order        []string // 作った順の ID
	nextID       int
	calls        map[string]int
	FailUpload   map[string]error
	FailDownload map[string]error
	email        string
}

type fakeItem struct {
	entry    domain.Entry
	parentID string
	isRoot   bool
	content  []byte
}

var fakeEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// NewDrive は、空の偽の Drive を作る。
func NewDrive(t *testing.T) *Drive {
	t.Helper()
	return &Drive{
		t:            t,
		items:        map[string]*fakeItem{},
		calls:        map[string]int{},
		FailUpload:   map[string]error{},
		FailDownload: map[string]error{},
		email:        "user@example.com",
	}
}

func (f *Drive) add(parentID, name string, typ domain.EntryType, content []byte, isRoot bool) domain.Entry {
	f.nextID++
	id := fmt.Sprintf("id-%d", f.nextID)
	created := fakeEpoch.Add(time.Duration(f.nextID) * time.Second)
	mime := "application/octet-stream"
	if typ == domain.TypeFolder {
		mime = domain.FolderMimeType
	}
	e := domain.Entry{
		ID: id, Name: name, Type: typ, Size: int64(len(content)), MimeType: mime,
		ModifiedTime: created, CreatedTime: created, WebViewLink: "https://example.com/" + id,
	}
	f.items[id] = &fakeItem{entry: e, parentID: parentID, isRoot: isRoot, content: content}
	f.order = append(f.order, id)
	return e
}

// AddRootFolder は、ルートフォルダ（名前は gdrive-cli、印つき）を足す。
func (f *Drive) AddRootFolder() domain.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.add("", usecase.RootFolderName, domain.TypeFolder, nil, true)
}

// AddFolder は、parentID の下にフォルダを足す。
func (f *Drive) AddFolder(parentID, name string) domain.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.add(parentID, name, domain.TypeFolder, nil, false)
}

// AddFile は、parentID の下にファイルを足す。
func (f *Drive) AddFile(parentID, name string, content []byte) domain.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.add(parentID, name, domain.TypeFile, content, false)
}

// SetTrashed は、項目のゴミ箱の印を切り替える。
func (f *Drive) SetTrashed(id string, trashed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[id]
	if !ok {
		f.t.Fatalf("SetTrashed: 未知の ID %q", id)
	}
	it.entry.InTrash = trashed
}

// SetMimeType は、項目の MIME タイプを変える。
func (f *Drive) SetMimeType(id, mimeType string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[id]
	if !ok {
		f.t.Fatalf("SetMimeType: 未知の ID %q", id)
	}
	it.entry.MimeType = mimeType
}

// Remove は、項目を完全になくす（ブラウザで完全に削除された状況を作る）。
func (f *Drive) Remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[id]; !ok {
		f.t.Fatalf("Remove: 未知の ID %q", id)
	}
	delete(f.items, id)
}

// CallCount は、メソッド名（"Get" など）の呼び出し回数を返す。
func (f *Drive) CallCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

// ChildrenOf は、parentID の下の（ゴミ箱でない）項目を作った順に返す。
func (f *Drive) ChildrenOf(parentID string) []domain.Entry {
	var out []domain.Entry
	for _, id := range f.order {
		it, ok := f.items[id]
		if ok && it.parentID == parentID && !it.isRoot && !it.entry.InTrash {
			out = append(out, it.entry)
		}
	}
	return out
}

// AccountEmail は、固定のメールアドレスを返す。
func (f *Drive) AccountEmail(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["AccountEmail"]++
	return f.email, nil
}

// FindRootFolders は、ルートフォルダの印のある項目を、ゴミ箱のものも含めて作った順に返す。
func (f *Drive) FindRootFolders(context.Context) ([]domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["FindRootFolders"]++
	var out []domain.Entry
	for _, id := range f.order {
		if it, ok := f.items[id]; ok && it.isRoot {
			out = append(out, it.entry)
		}
	}
	return out, nil
}

// CreateRootFolder は、ルートフォルダの印のあるフォルダを作る。
func (f *Drive) CreateRootFolder(_ context.Context, name string) (domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["CreateRootFolder"]++
	return f.add("", name, domain.TypeFolder, nil, true), nil
}

// Get は、id の項目を返す。ないときは KindNotFound。
func (f *Drive) Get(_ context.Context, id string) (domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Get"]++
	it, ok := f.items[id]
	if !ok {
		return domain.Entry{}, domain.Errorf(domain.KindNotFound, "%s が見つかりません", id)
	}
	return it.entry, nil
}

// ListChildren は、parentID の下の（ゴミ箱でない）項目を返す。
func (f *Drive) ListChildren(_ context.Context, parentID string) ([]domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["ListChildren"]++
	return f.ChildrenOf(parentID), nil
}

// FindChildren は、parentID の下の（ゴミ箱でない）項目のうち、名前が name のものを返す。
func (f *Drive) FindChildren(_ context.Context, parentID, name string) ([]domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["FindChildren"]++
	var out []domain.Entry
	for _, e := range f.ChildrenOf(parentID) {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out, nil
}

// checkParent は、parentID が存在するフォルダか確かめる。呼び出し側が f.mu を持っていること。
func (f *Drive) checkParent(parentID string) error {
	if it, ok := f.items[parentID]; !ok || it.entry.Type != domain.TypeFolder {
		return domain.Errorf(domain.KindNotFound, "親フォルダ %s が見つかりません", parentID)
	}
	return nil
}

// CreateFolder は、parentID の下にフォルダを作る。親がなければ KindNotFound。
func (f *Drive) CreateFolder(_ context.Context, parentID, name string) (domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["CreateFolder"]++
	if err := f.checkParent(parentID); err != nil {
		return domain.Entry{}, err
	}
	return f.add(parentID, name, domain.TypeFolder, nil, false), nil
}

// Upload は、body を読んで parentID の下にファイルを作る。親がなければ KindNotFound。
// FailUpload に name があれば、そのエラーを返す。
func (f *Drive) Upload(_ context.Context, parentID, name, mimeType string, body io.Reader, _ int64, progress func(int64)) (domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Upload"]++
	if err := f.checkParent(parentID); err != nil {
		return domain.Entry{}, err
	}
	if err := f.FailUpload[name]; err != nil {
		return domain.Entry{}, err
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return domain.Entry{}, err
	}
	e := f.add(parentID, name, domain.TypeFile, data, false)
	f.items[e.ID].entry.MimeType = mimeType
	if progress != nil {
		progress(int64(len(data)))
	}
	return f.items[e.ID].entry, nil
}

// Download は、id の中身を w に書く。ないときは KindNotFound。
// FailDownload に項目の名前があれば、中身の前半だけを書いてから、そのエラーを返す。
func (f *Drive) Download(_ context.Context, id string, w io.Writer, progress func(int64)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Download"]++
	it, ok := f.items[id]
	if !ok {
		return domain.Errorf(domain.KindNotFound, "%s が見つかりません", id)
	}
	if err := f.FailDownload[it.entry.Name]; err != nil {
		_, _ = w.Write(it.content[:len(it.content)/2])
		return err
	}
	n, err := io.Copy(w, bytes.NewReader(it.content))
	if progress != nil {
		progress(n)
	}
	return err
}

// Move は、id を fromParentID から toParentID の下へ移す。newName が空でなければ名前も変える。
func (f *Drive) Move(_ context.Context, id, fromParentID, toParentID, newName string) (domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Move"]++
	it, ok := f.items[id]
	if !ok {
		return domain.Entry{}, domain.Errorf(domain.KindNotFound, "%s が見つかりません", id)
	}
	if it.parentID != fromParentID {
		return domain.Entry{}, domain.Errorf(domain.KindUnexpected, "%s の親は %s ではありません", id, fromParentID)
	}
	if err := f.checkParent(toParentID); err != nil {
		return domain.Entry{}, err
	}
	it.parentID = toParentID
	if newName != "" {
		it.entry.Name = newName
	}
	return it.entry, nil
}

// State は、メモリ上の StateStore。ID が保存したルートフォルダの ID。
type State struct{ ID string }

// RootFolderID は、ID を返す。
func (s *State) RootFolderID() (string, error) { return s.ID, nil }

// SetRootFolderID は、ID に保存する。
func (s *State) SetRootFolderID(id string) error {
	s.ID = id
	return nil
}

// Locker は、何もしない Locker。ロックの回数を数える。
type Locker struct{ locks int }

// Lock は、回数を数えるだけで、すぐに返す。
func (l *Locker) Lock(context.Context) (func(), error) {
	l.locks++
	return func() {}, nil
}
