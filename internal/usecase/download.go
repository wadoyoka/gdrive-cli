package usecase

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

const (
	msgDirFailed = "手元のフォルダを作れなかったので受け取っていません"
	msgNotSaved  = "中断したので受け取っていません"
)

// Download は、ドライブのファイルやフォルダを手元へ受け取る。
type Download struct {
	Remote      Remote
	Concurrency int // 同時に受け取るファイルの数。0 なら 4
}

// DownloadInput は、Download の入力。
type DownloadInput struct {
	Sources      []domain.RemotePath // 受け取るファイルやフォルダ
	DestDir      string              // 保存先のディレクトリ
	Parents      bool                // 保存先がなければ作る
	Overwrite    bool                // 同じ名前のファイルを置き換える
	SkipExisting bool                // 同じ名前のファイルがあれば飛ばす（中身は比べない）
	Progress     ProgressFunc        // nil なら進捗を知らせない
}

// downloadNode は、計画の 1 項目。
type downloadNode struct {
	item   TransferItem
	path   domain.RemotePath
	parent *downloadNode // nil なら保存先の直下
}

// Run は、計画を立ててから受け取る。
//
// 計画（ドライブをたどり、手元と照らし合わせる）の間は何も書かない。手元と衝突すれば、何も書かずに
// KindConflict を返す。保存先を -p で作るのも、計画が通ってからにする。
// 手元で使えない名前（"../x" など）と Google ドキュメントの類は、警告を出して飛ばす。
// 受け取り始めたあとの失敗は項目の Error に入れて残りを続け、最後に結果と一緒に KindUnexpected を返す。
func (u Download) Run(ctx context.Context, in DownloadInput) (TransferResult, error) {
	if in.Overwrite && in.SkipExisting {
		return emptyTransfer(nil), domain.Errorf(domain.KindUsage, "--overwrite と --skip-existing は同時に使えません")
	}
	dest := filepath.Clean(in.DestDir)
	if err := checkDestDir(dest, in.Parents); err != nil {
		return emptyTransfer(nil), err
	}
	s, err := u.Remote.Connect(ctx)
	if err != nil {
		return emptyTransfer(nil), err
	}
	mode := domain.ConflictError
	switch {
	case in.Overwrite:
		mode = domain.ConflictOverwrite
	case in.SkipExisting:
		mode = domain.ConflictSkip
	}
	nodes, warnings, err := plan(ctx, s, in.Sources, dest, mode)
	warnings = append(warnings, s.Resolver.Warnings()...)
	if err != nil {
		return emptyTransfer(warnings), err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil { //nolint:gosec // 受け取ったものは他の人が読めてよい
		return emptyTransfer(warnings), domain.Errorf(domain.KindUnexpected, "%s を作れません", in.DestDir).Wrap(err)
	}
	u.createDirs(ctx, nodes)
	u.downloadFiles(ctx, s, nodes, in.Progress)
	items, err := collectDownload(ctx, nodes)
	return TransferResult{Items: items, Warnings: warnings}, err
}

func emptyTransfer(warnings []string) TransferResult {
	return TransferResult{Items: []TransferItem{}, Warnings: warnings}
}

// checkDestDir は、保存先を確かめる。ここでは何も作らない。
// ないときは、Parents でなければ KindNotFound。ファイルなら KindConflict。
func checkDestDir(dest string, parents bool) error {
	info, err := os.Stat(dest)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if parents {
			return nil
		}
		return domain.Errorf(domain.KindNotFound, "保存先の %s が見つかりません。-p を付けると作ります", dest)
	case err != nil:
		return domain.Errorf(domain.KindUnexpected, "%s を読めません", dest).Wrap(err)
	case !info.IsDir():
		return domain.Errorf(domain.KindConflict, "保存先の %s はファイルです", dest)
	}
	return nil
}

// plan は、ソースをドライブからたどって計画の項目を作る（親が子より先に並ぶ）。手元には何も書かない。
func plan(ctx context.Context, s *Session, sources []domain.RemotePath, dest string, mode domain.ConflictMode) ([]*downloadNode, []string, error) {
	p := &planner{ctx: ctx, s: s, dest: dest, mode: mode}
	for _, src := range sources {
		e, err := s.Resolver.Resolve(ctx, src)
		if err != nil {
			return nil, p.warnings, err
		}
		if err := p.add(nil, src, e); err != nil {
			return nil, p.warnings, err
		}
	}
	count := map[string]int{}
	for _, n := range p.nodes {
		count[n.item.LocalPath]++
		if count[n.item.LocalPath] == 2 {
			p.conflicts = append(p.conflicts, n.item.RemotePath)
		}
	}
	if len(p.conflicts) > 0 {
		return nil, p.warnings, conflictError(p.conflicts)
	}
	return p.nodes, p.warnings, nil
}

type planner struct {
	ctx       context.Context
	s         *Session
	dest      string
	mode      domain.ConflictMode
	nodes     []*downloadNode
	warnings  []string
	conflicts []string
}

// add は、e を計画に足す。フォルダなら中身も足す。
func (p *planner) add(parent *downloadNode, path domain.RemotePath, e domain.Entry) error {
	base := p.dest
	if parent != nil {
		base = parent.item.LocalPath
	}
	if !safeName(e.Name) {
		p.warnings = append(p.warnings, unsafeNameWarning(path))
		return nil
	}
	local := filepath.Join(base, e.Name)
	if !within(p.dest, local) {
		p.warnings = append(p.warnings, unsafeNameWarning(path))
		return nil
	}
	if e.Type == domain.TypeFile && domain.IsGoogleNative(e.MimeType) {
		p.warnings = append(p.warnings, "Google ドキュメントの類は受け取れないので飛ばしました: "+path.String())
		return nil
	}
	n := &downloadNode{path: path, parent: parent}
	n.item = TransferItem{
		LocalPath: local, RemotePath: path.String(), ID: e.ID, Type: e.Type, Size: e.Size, WebViewLink: e.WebViewLink,
	}
	if e.Type == domain.TypeFolder {
		n.item.Size = 0
	}
	existing, err := localType(local)
	if err != nil {
		return err
	}
	action, err := domain.Decide(e.Type, existing, p.mode)
	if err != nil {
		// 衝突したフォルダの中は、手元と照らし合わせられないので辿らない
		p.conflicts = append(p.conflicts, n.item.RemotePath)
		return nil
	}
	n.item.Action = action
	p.nodes = append(p.nodes, n)
	if e.Type != domain.TypeFolder {
		return nil
	}
	children, err := p.s.Drive.ListChildren(p.ctx, e.ID)
	if err != nil {
		return err
	}
	for _, c := range children {
		if err := p.add(n, path.Join(domain.NormalizeName(c.Name)), c); err != nil {
			return err
		}
	}
	return nil
}

func unsafeNameWarning(path domain.RemotePath) string {
	return "手元で使えない名前なので飛ばしました: " + path.String()
}

// safeName は、name が手元のパスの 1 要素として使えるかを返す。
func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}

// within は、target が dir の中（dir 自身は含まない）にあるかを返す。
func within(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// localType は、手元の path にあるものの種類を返す。ないときは nil。
// シンボリックリンクは辿らず、ファイルとして数える（置き換えるときはリンクそのものを置き換える）。
func localType(path string) (*domain.EntryType, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, domain.Errorf(domain.KindUnexpected, "%s を読めません", path).Wrap(err)
	}
	t := domain.TypeFile
	if info.IsDir() {
		t = domain.TypeFolder
	}
	return &t, nil
}

// createDirs は、作るフォルダを親から順に作る。作れなかったフォルダの中は受け取らない。
func (u Download) createDirs(ctx context.Context, nodes []*downloadNode) {
	for _, n := range nodes {
		if n.item.Type != domain.TypeFolder || n.item.Action != domain.ActionCreate {
			continue
		}
		switch {
		case ctx.Err() != nil:
			n.item.Error = msgNotSaved
		case parentFailed(n):
			n.item.Error = msgDirFailed
		default:
			if err := os.MkdirAll(n.item.LocalPath, 0o755); err != nil { //nolint:gosec // 受け取ったものは他の人が読めてよい
				n.item.Error = err.Error()
			}
		}
	}
}

func parentFailed(n *downloadNode) bool {
	return n.parent != nil && n.parent.item.Error != ""
}

// downloadFiles は、受け取るファイルを最大 Concurrency 本ずつ同時に受け取る。失敗は項目の Error に入れて、ほかは続ける。
// ctx が取り消されたら、まだ渡していないファイルは受け取らない。
func (u Download) downloadFiles(ctx context.Context, s *Session, nodes []*downloadNode, progress ProgressFunc) {
	var jobs []*downloadNode
	for _, n := range nodes {
		if n.item.Type != domain.TypeFile || (n.item.Action != domain.ActionCreate && n.item.Action != domain.ActionOverwrite) {
			continue
		}
		if parentFailed(n) {
			n.item.Error = msgDirFailed
			continue
		}
		jobs = append(jobs, n)
	}
	workers := u.Concurrency
	if workers <= 0 {
		workers = defaultConcurrency
	}

	ch := make(chan *downloadNode)
	var wg sync.WaitGroup
	for range min(workers, len(jobs)) {
		wg.Go(func() {
			for n := range ch {
				// 項目は 1 つのゴルーチンだけが書くので、ロックはいらない
				if err := downloadOne(ctx, s, n, progress); err != nil {
					n.item.Error = err.Error()
				}
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
		n.item.Error = msgNotSaved
	}
}

// downloadOne は、1 ファイルを同じディレクトリの一時ファイルに書いてから、最終の名前に改名する。
// 失敗したときや中断されたときは、一時ファイルを消す。
func downloadOne(ctx context.Context, s *Session, n *downloadNode, progress ProgressFunc) (err error) {
	if ctx.Err() != nil {
		return errors.New(msgNotSaved)
	}
	target := n.item.LocalPath
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".gdrive-cli-*.part")
	if err != nil {
		return domain.Errorf(domain.KindUnexpected, "%s の一時ファイルを作れません", target).Wrap(err)
	}
	closed := false
	defer func() {
		if err != nil {
			if !closed {
				_ = tmp.Close()
			}
			_ = os.Remove(tmp.Name())
		}
	}()

	path, total := n.item.RemotePath, n.item.Size
	var report func(int64)
	if progress != nil {
		progress(path, 0, total)
		report = func(received int64) { progress(path, received, total) }
	}
	if err := s.Drive.Download(ctx, n.item.ID, tmp, report); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return errors.New(msgNotSaved)
	}
	if err := tmp.Chmod(0o644); err != nil {
		return domain.Errorf(domain.KindUnexpected, "%s の権限を設定できません", target).Wrap(err)
	}
	closed = true
	if err := tmp.Close(); err != nil {
		return domain.Errorf(domain.KindUnexpected, "%s を書けません", target).Wrap(err)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return domain.Errorf(domain.KindUnexpected, "%s に改名できません", target).Wrap(err)
	}
	if progress != nil {
		progress(path, total, total)
	}
	return nil
}

// collectDownload は、項目を RemotePath の順（フォルダの中身はそのフォルダのすぐあと）に並べる。
// 失敗が 1 件でもあれば、結果と一緒に KindUnexpected を返す。補足は、失敗したパスと理由。
func collectDownload(ctx context.Context, nodes []*downloadNode) ([]TransferItem, error) {
	slices.SortStableFunc(nodes, func(a, b *downloadNode) int {
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
	err := domain.Errorf(domain.KindUnexpected, "%d 件のダウンロードに失敗しました。--skip-existing を付けて同じコマンドをもう一度実行すると、続きから受け取れます", len(failed)).
		WithDetails(failed...)
	if ctx.Err() != nil {
		err = err.Wrap(ctx.Err())
	}
	return items, err
}
