package usecase

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

const (
	defaultConcurrency = 4
	dsStoreName        = ".DS_Store"
	msgParentFailed    = "親フォルダを作れなかったので送っていません"
	msgCancelled       = "中断したので送っていません"
)

// Upload は、手元のファイルやフォルダをドライブへ送る。
type Upload struct {
	Remote      Remote
	Concurrency int // 同時に送るファイルの数。0 なら 4
}

// UploadInput は、Upload の入力。
type UploadInput struct {
	Sources      []string          // 手元のファイルやフォルダのパス
	Dest         domain.RemotePath // 送り先のフォルダ
	Parents      bool              // 送り先がなければ作る
	SkipExisting bool              // 同じ名前のファイルがあれば飛ばす（中身は比べない）
	Progress     ProgressFunc      // nil なら進捗を知らせない
}

// uploadNode は、計画の 1 項目。
type uploadNode struct {
	item   TransferItem
	path   domain.RemotePath
	parent *uploadNode   // nil なら送り先の直下
	folder *domain.Entry // フォルダのとき、合流先か作ったもの。作れなければ nil
}

// Run は、計画を立ててから送る。
//
// 計画（手元をたどり、ドライブと照らし合わせる）の間は何も書かない。衝突が 1 件でもあれば、
// 何も書かずに KindConflict を返す。手元だけで分かる誤り（ソースがない、送り先がぶつかる）は、
// ドライブに触る前に返すので、Parents でも送り先のフォルダを作らない。
// 送り始めたあとの失敗は項目の Error に入れて残りを続け、最後に結果と一緒に KindUnexpected を返す。
func (u Upload) Run(ctx context.Context, in UploadInput) (TransferResult, error) {
	nodes, warnings, err := scanSources(in.Sources, in.Dest)
	if err != nil {
		return TransferResult{Items: []TransferItem{}, Warnings: warnings}, err
	}
	s, err := u.Remote.Connect(ctx)
	if err != nil {
		return TransferResult{Items: []TransferItem{}, Warnings: warnings}, err
	}
	items, err := u.run(ctx, s, in, nodes)
	return TransferResult{Items: items, Warnings: append(warnings, s.Resolver.Warnings()...)}, err
}

func (u Upload) run(ctx context.Context, s *Session, in UploadInput, nodes []*uploadNode) ([]TransferItem, error) {
	dest, err := u.destination(ctx, s, in)
	if err != nil {
		return []TransferItem{}, err
	}
	mode := domain.ConflictError
	if in.SkipExisting {
		mode = domain.ConflictSkip
	}
	if err := check(ctx, s, dest, nodes, mode); err != nil {
		return []TransferItem{}, err
	}
	u.createFolders(ctx, s, dest, nodes)
	u.uploadFiles(ctx, s, dest, nodes, in.Progress)
	return collect(ctx, nodes)
}

// scanSources は、手元のソースをたどって計画の項目を作る（親が子より先に並ぶ）。ドライブには触れない。
// 2 つの項目が同じ送り先になるときは KindConflict を返す。
func scanSources(sources []string, dest domain.RemotePath) ([]*uploadNode, []string, error) {
	var nodes []*uploadNode
	var warnings []string
	for _, src := range sources {
		found, warns, err := scanSource(src, dest)
		warnings = append(warnings, warns...)
		if err != nil {
			return nil, warnings, err
		}
		nodes = append(nodes, found...)
	}
	count := map[string]int{}
	var dups []string
	for _, n := range nodes {
		count[n.item.RemotePath]++
		if count[n.item.RemotePath] == 2 {
			dups = append(dups, n.item.RemotePath)
		}
	}
	if len(dups) > 0 {
		return nil, warnings, conflictError(dups)
	}
	return nodes, warnings, nil
}

// scanSource は、1 つのソースを filepath.WalkDir でたどる。
// .DS_Store は黙って、シンボリックリンクと通常でないファイル（名前付きパイプなど）は警告を出して飛ばす。
func scanSource(src string, dest domain.RemotePath) ([]*uploadNode, []string, error) {
	root := filepath.Clean(src)
	info, err := os.Lstat(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil, domain.Errorf(domain.KindNotFound, "%s が見つかりません", src)
	case err != nil:
		return nil, nil, domain.Errorf(domain.KindUnexpected, "%s を読めません", src).Wrap(err)
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, []string{symlinkWarning(root)}, nil
	}
	top, err := sourceName(root)
	if err != nil {
		return nil, nil, err
	}

	var nodes []*uploadNode
	var warnings []string
	dirs := map[string]*uploadNode{} // 手元のパス → そのフォルダの項目
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return domain.Errorf(domain.KindUnexpected, "%s を読めません", p).Wrap(err)
		}
		switch {
		case d.Name() == dsStoreName:
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			warnings = append(warnings, symlinkWarning(p))
			return nil
		case !d.IsDir() && !d.Type().IsRegular():
			warnings = append(warnings, "通常のファイルではないので飛ばしました: "+p)
			return nil
		}
		n := &uploadNode{}
		if p == root {
			n.path = dest.Join(domain.NormalizeName(top))
		} else {
			n.parent = dirs[filepath.Dir(p)]
			n.path = n.parent.path.Join(domain.NormalizeName(d.Name()))
		}
		n.item = TransferItem{LocalPath: p, RemotePath: n.path.String(), Type: domain.TypeFile}
		if d.IsDir() {
			n.item.Type = domain.TypeFolder
			dirs[p] = n
		} else {
			fi, err := d.Info()
			if err != nil {
				return domain.Errorf(domain.KindUnexpected, "%s を読めません", p).Wrap(err)
			}
			n.item.Size = fi.Size()
		}
		nodes = append(nodes, n)
		return nil
	})
	return nodes, warnings, err
}

// sourceName は、ソースを送り先で呼ぶ名前を返す。"." や "dir/" でも、そのディレクトリの名前になる。
func sourceName(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", domain.Errorf(domain.KindUnexpected, "%s の場所が分かりません", root).Wrap(err)
	}
	if filepath.Dir(abs) == abs {
		return "", domain.Errorf(domain.KindUsage, "%s は最上位のディレクトリなので送れません", root)
	}
	return filepath.Base(abs), nil
}

func symlinkWarning(p string) string {
	return "シンボリックリンクは飛ばしました: " + p
}

func conflictError(paths []string) error {
	return domain.Errorf(domain.KindConflict, "同じ名前のものがあります").WithDetails(paths...)
}

// destination は、送り先のフォルダを返す。Parents なら、なければ作る。
// どちらも内部でロックを取るので、ロックを持たずに呼ぶ。
func (u Upload) destination(ctx context.Context, s *Session, in UploadInput) (domain.Entry, error) {
	if in.Parents {
		e, _, err := s.Resolver.EnsureFolder(ctx, in.Dest)
		return e, err
	}
	e, err := s.Resolver.Resolve(ctx, in.Dest)
	if err != nil {
		return domain.Entry{}, err
	}
	if e.Type != domain.TypeFolder {
		return domain.Entry{}, domain.Errorf(domain.KindConflict, "%s はファイルです", in.Dest)
	}
	return e, nil
}

// check は、送り先と照らし合わせて各項目の Action を決める。ドライブには書かない。
// 既にあるフォルダの中だけを Lookup で確かめる（新しく作るフォルダの中は空なので確かめない）。
// 衝突があれば、その送り先パスの一覧を KindConflict で返す。
func check(ctx context.Context, s *Session, dest domain.Entry, nodes []*uploadNode, mode domain.ConflictMode) error {
	var conflicts []string
	for _, n := range nodes {
		parent := dest
		if n.parent != nil {
			switch n.parent.item.Action {
			case domain.ActionCreate:
				n.item.Action = domain.ActionCreate
				continue
			case domain.ActionMerge:
				parent = *n.parent.folder
			default:
				continue // 親が衝突しているので、中は照らし合わせられない
			}
		}
		existing, err := s.Resolver.Lookup(ctx, parent, n.path.Base())
		if err != nil {
			return err
		}
		var existingType *domain.EntryType
		if existing != nil {
			existingType = &existing.Type
		}
		action, err := domain.Decide(n.item.Type, existingType, mode)
		if err != nil {
			conflicts = append(conflicts, n.item.RemotePath)
			continue
		}
		n.item.Action = action
		if existing != nil {
			n.item.ID, n.item.WebViewLink = existing.ID, existing.WebViewLink
			if action == domain.ActionMerge {
				n.folder = existing
			}
		}
	}
	if len(conflicts) > 0 {
		return conflictError(conflicts)
	}
	return nil
}

// parentFolder は、n の親フォルダ（送り先の直下なら dest）を返す。親を作れなかったときは false。
func parentFolder(n *uploadNode, dest domain.Entry) (domain.Entry, bool) {
	if n.parent == nil {
		return dest, true
	}
	if n.parent.folder == nil {
		return domain.Entry{}, false
	}
	return *n.parent.folder, true
}

// createFolders は、作るフォルダを親から順に作る。作れなかったフォルダの中は送らない。
func (u Upload) createFolders(ctx context.Context, s *Session, dest domain.Entry, nodes []*uploadNode) {
	for _, n := range nodes {
		if n.item.Type != domain.TypeFolder || n.item.Action != domain.ActionCreate {
			continue
		}
		parent, ok := parentFolder(n, dest)
		switch {
		case ctx.Err() != nil:
			n.item.Error = msgCancelled
			continue
		case !ok:
			n.item.Error = msgParentFailed
			continue
		}
		e, action, err := u.createFolder(ctx, s, parent, n.path)
		if err != nil {
			n.item.Error = err.Error()
			continue
		}
		n.folder = &e
		n.item.Action = action
		n.item.ID, n.item.WebViewLink = e.ID, e.WebViewLink
	}
}

// createFolder は、ロックを持った状態でもう一度確かめてから、フォルダを作る。
// 計画のあとにほかの実行が同じ名前のフォルダを作っていたら、作らずにそれへ合流する（ActionMerge）。
// Root や EnsureFolder はロックを取るので、ロックの中では呼ばない（flock は入れ子にできず、止まる）。
func (u Upload) createFolder(ctx context.Context, s *Session, parent domain.Entry, p domain.RemotePath) (domain.Entry, domain.Action, error) {
	unlock, err := u.Remote.Locker.Lock(ctx)
	if err != nil {
		return domain.Entry{}, "", err
	}
	defer unlock()
	existing, err := s.Resolver.Lookup(ctx, parent, p.Base())
	if err != nil {
		return domain.Entry{}, "", err
	}
	if existing != nil {
		if _, err := domain.Decide(domain.TypeFolder, &existing.Type, domain.ConflictError); err != nil {
			return domain.Entry{}, "", err
		}
		return *existing, domain.ActionMerge, nil
	}
	e, err := s.Drive.CreateFolder(ctx, parent.ID, p.Base())
	if err != nil {
		return domain.Entry{}, "", err
	}
	e.Path = p.String()
	return e, domain.ActionCreate, nil
}

// uploadFiles は、作るファイルを最大 Concurrency 本ずつ同時に送る。失敗は項目の Error に入れて、ほかは続ける。
// ctx が取り消されたら、まだ渡していないファイルは送らない（送っている途中のものは取り消された ctx で終わる）。
func (u Upload) uploadFiles(ctx context.Context, s *Session, dest domain.Entry, nodes []*uploadNode, progress ProgressFunc) {
	var jobs []*uploadNode
	for _, n := range nodes {
		if n.item.Type != domain.TypeFile || n.item.Action != domain.ActionCreate {
			continue
		}
		if _, ok := parentFolder(n, dest); !ok {
			n.item.Error = msgParentFailed
			continue
		}
		jobs = append(jobs, n)
	}
	workers := u.Concurrency
	if workers <= 0 {
		workers = defaultConcurrency
	}

	ch := make(chan *uploadNode)
	var mu sync.Mutex // 結果を項目に書き込むとき
	var wg sync.WaitGroup
	for range min(workers, len(jobs)) {
		wg.Go(func() {
			for n := range ch {
				parent, _ := parentFolder(n, dest)
				e, err := uploadOne(ctx, s, parent.ID, n, progress)
				mu.Lock()
				if err != nil {
					n.item.Error = err.Error()
				} else {
					n.item.ID, n.item.WebViewLink = e.ID, e.WebViewLink
				}
				mu.Unlock()
			}
		})
	}
	sent := 0
feed:
	for _, n := range jobs {
		select {
		case <-ctx.Done():
			break feed
		case ch <- n:
			sent++
		}
	}
	close(ch)
	wg.Wait()
	for _, n := range jobs[sent:] {
		n.item.Error = msgCancelled
	}
}

// uploadOne は、1 ファイルを送る。progress があれば、始めと終わりにも知らせる。
func uploadOne(ctx context.Context, s *Session, parentID string, n *uploadNode, progress ProgressFunc) (domain.Entry, error) {
	if ctx.Err() != nil {
		return domain.Entry{}, errors.New(msgCancelled)
	}
	f, err := os.Open(n.item.LocalPath)
	if err != nil {
		return domain.Entry{}, domain.Errorf(domain.KindUnexpected, "%s を開けません", n.item.LocalPath).Wrap(err)
	}
	defer func() { _ = f.Close() }()

	path, total := n.item.RemotePath, n.item.Size
	var report func(int64)
	if progress != nil {
		progress(path, 0, total)
		report = func(sent int64) { progress(path, sent, total) }
	}
	name := n.path.Base()
	e, err := s.Drive.Upload(ctx, parentID, name, domain.MimeTypeFor(name), f, total, report)
	if err != nil {
		return domain.Entry{}, err
	}
	if progress != nil {
		progress(path, total, total)
	}
	return e, nil
}

// collect は、項目を RemotePath の順（フォルダの中身はそのフォルダのすぐあと）に並べる。
// 失敗が 1 件でもあれば、結果と一緒に KindUnexpected を返す。補足は、失敗した送り先パスと理由。
func collect(ctx context.Context, nodes []*uploadNode) ([]TransferItem, error) {
	slices.SortStableFunc(nodes, func(a, b *uploadNode) int {
		return slices.Compare(a.path.Segments(), b.path.Segments())
	})
	items := make([]TransferItem, 0, len(nodes))
	var failed []string
	for _, n := range nodes {
		items = append(items, n.item)
		if n.item.Error != "" {
			failed = append(failed, fmt.Sprintf("%s: %s", n.item.RemotePath, n.item.Error))
		}
	}
	if len(failed) == 0 {
		return items, nil
	}
	err := domain.Errorf(domain.KindUnexpected, "%d 件のアップロードに失敗しました。--skip-existing を付けて同じコマンドをもう一度実行すると、続きから送れます", len(failed)).
		WithDetails(failed...)
	if ctx.Err() != nil {
		err = err.Wrap(ctx.Err())
	}
	return items, err
}
