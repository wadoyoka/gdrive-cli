package usecase

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

// fakeDrive は、メモリ上に木を持つ偽の Drive。Task 7 以降のテストで使い回す。
//
// 使い方:
//   - addFolder / addFile / addRootFolder で木を作る（ID は id-1 から順に振る）。
//   - setTrashed で項目にゴミ箱の印を付ける。
//   - calls で、メソッド名ごとの呼び出し回数を数える。
//   - failUpload / failDownload に名前を入れると、その名前の Upload / Download が失敗する。
//
// 契約: Get は KindNotFound を返す。CreateFolder / Upload は親がなければ、Move は移動先がなければ、
// KindNotFound を返す。Move は fromParentID が今の親と違えば KindUnexpected を返す。
// setTrashed / remove は未知の ID なら t.Fatalf する。
//
// 本物の Drive との違い: ctx は見ない。作った直後に一覧へ出ない（結果整合）は再現しない。
// CreatedTime は、固定の時刻から 1 秒ずつ増やす。
type fakeDrive struct {
	t            *testing.T
	mu           sync.Mutex
	items        map[string]*fakeItem
	order        []string // 作った順の ID
	nextID       int
	calls        map[string]int
	failUpload   map[string]error
	failDownload map[string]error
	email        string
}

type fakeItem struct {
	entry    domain.Entry
	parentID string
	isRoot   bool
	content  []byte
}

var fakeEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func newFakeDrive(t *testing.T) *fakeDrive {
	t.Helper()
	return &fakeDrive{
		t:            t,
		items:        map[string]*fakeItem{},
		calls:        map[string]int{},
		failUpload:   map[string]error{},
		failDownload: map[string]error{},
		email:        "user@example.com",
	}
}

func (f *fakeDrive) add(parentID, name string, typ domain.EntryType, content []byte, isRoot bool) domain.Entry {
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

// addRootFolder は、ルートフォルダ（名前は gdrive-cli、印つき）を足す。
func (f *fakeDrive) addRootFolder() domain.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.add("", RootFolderName, domain.TypeFolder, nil, true)
}

// addFolder は、parentID の下にフォルダを足す。
func (f *fakeDrive) addFolder(parentID, name string) domain.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.add(parentID, name, domain.TypeFolder, nil, false)
}

// addFile は、parentID の下にファイルを足す。
func (f *fakeDrive) addFile(parentID, name string, content []byte) domain.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.add(parentID, name, domain.TypeFile, content, false)
}

// setTrashed は、項目のゴミ箱の印を切り替える。
func (f *fakeDrive) setTrashed(id string, trashed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[id]
	if !ok {
		f.t.Fatalf("setTrashed: 未知の ID %q", id)
	}
	it.entry.InTrash = trashed
}

// remove は、項目を完全になくす（ブラウザで完全に削除された状況を作る）。
func (f *fakeDrive) remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[id]; !ok {
		f.t.Fatalf("remove: 未知の ID %q", id)
	}
	delete(f.items, id)
}

// callCount は、メソッド名（"Get" など）の呼び出し回数を返す。
func (f *fakeDrive) callCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

// childrenOf は、parentID の下の（ゴミ箱でない）項目を作った順に返す。
func (f *fakeDrive) childrenOf(parentID string) []domain.Entry {
	var out []domain.Entry
	for _, id := range f.order {
		it, ok := f.items[id]
		if ok && it.parentID == parentID && !it.isRoot && !it.entry.InTrash {
			out = append(out, it.entry)
		}
	}
	return out
}

func (f *fakeDrive) AccountEmail(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["AccountEmail"]++
	return f.email, nil
}

func (f *fakeDrive) FindRootFolders(context.Context) ([]domain.Entry, error) {
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

func (f *fakeDrive) CreateRootFolder(_ context.Context, name string) (domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["CreateRootFolder"]++
	return f.add("", name, domain.TypeFolder, nil, true), nil
}

func (f *fakeDrive) Get(_ context.Context, id string) (domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Get"]++
	it, ok := f.items[id]
	if !ok {
		return domain.Entry{}, domain.Errorf(domain.KindNotFound, "%s が見つかりません", id)
	}
	return it.entry, nil
}

func (f *fakeDrive) ListChildren(_ context.Context, parentID string) ([]domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["ListChildren"]++
	return f.childrenOf(parentID), nil
}

func (f *fakeDrive) FindChildren(_ context.Context, parentID, name string) ([]domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["FindChildren"]++
	var out []domain.Entry
	for _, e := range f.childrenOf(parentID) {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out, nil
}

// checkParent は、parentID が存在するフォルダか確かめる。呼び出し側が f.mu を持っていること。
func (f *fakeDrive) checkParent(parentID string) error {
	if it, ok := f.items[parentID]; !ok || it.entry.Type != domain.TypeFolder {
		return domain.Errorf(domain.KindNotFound, "親フォルダ %s が見つかりません", parentID)
	}
	return nil
}

func (f *fakeDrive) CreateFolder(_ context.Context, parentID, name string) (domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["CreateFolder"]++
	if err := f.checkParent(parentID); err != nil {
		return domain.Entry{}, err
	}
	return f.add(parentID, name, domain.TypeFolder, nil, false), nil
}

func (f *fakeDrive) Upload(_ context.Context, parentID, name, mimeType string, body io.Reader, _ int64, progress func(int64)) (domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Upload"]++
	if err := f.checkParent(parentID); err != nil {
		return domain.Entry{}, err
	}
	if err := f.failUpload[name]; err != nil {
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

func (f *fakeDrive) Download(_ context.Context, id string, w io.Writer, progress func(int64)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["Download"]++
	it, ok := f.items[id]
	if !ok {
		return domain.Errorf(domain.KindNotFound, "%s が見つかりません", id)
	}
	if err := f.failDownload[it.entry.Name]; err != nil {
		return err
	}
	n, err := io.Copy(w, bytes.NewReader(it.content))
	if progress != nil {
		progress(n)
	}
	return err
}

func (f *fakeDrive) Move(_ context.Context, id, fromParentID, toParentID, newName string) (domain.Entry, error) {
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

// fakeState は、メモリ上の StateStore。
type fakeState struct{ id string }

func (s *fakeState) RootFolderID() (string, error) { return s.id, nil }
func (s *fakeState) SetRootFolderID(id string) error {
	s.id = id
	return nil
}

// fakeLocker は、何もしない Locker。ロックの回数を数える。
type fakeLocker struct{ locks int }

func (l *fakeLocker) Lock(context.Context) (func(), error) {
	l.locks++
	return func() {}, nil
}
