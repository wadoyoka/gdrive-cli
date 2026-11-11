package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

const rootInTrashMessage = "ルートフォルダ（gdrive-cli）がゴミ箱にあります。ブラウザでゴミ箱から戻してください"

// Resolver は、ルートフォルダを見つけ、パスを項目に解決する。
type Resolver struct {
	drive    Drive
	state    StateStore
	locker   Locker
	root     *domain.Entry
	warnings []string
}

// NewResolver は、Resolver を作る。
func NewResolver(d Drive, s StateStore, l Locker) *Resolver {
	return &Resolver{drive: d, state: s, locker: l}
}

// Warnings は、Root が見つけた注意（ルートが複数あったときなど）を返す。
func (r *Resolver) Warnings() []string { return r.warnings }

// Root は、ルートフォルダを返す。最初の結果を覚えておく。
func (r *Resolver) Root(ctx context.Context) (domain.Entry, error) {
	if r.root != nil {
		return *r.root, nil
	}
	e, err := r.findRoot(ctx)
	if err != nil {
		return domain.Entry{}, err
	}
	e.Path = domain.RootPath().String()
	r.root = &e
	return e, nil
}

func (r *Resolver) findRoot(ctx context.Context) (domain.Entry, error) {
	id, err := r.state.RootFolderID()
	if err != nil {
		return domain.Entry{}, err
	}
	if id != "" {
		e, err := r.drive.Get(ctx, id)
		switch {
		case err == nil:
			if e.InTrash {
				return domain.Entry{}, domain.Errorf(domain.KindSetup, rootInTrashMessage)
			}
			if e.Type == domain.TypeFolder {
				return e, nil
			}
		case domain.KindOf(err) != domain.KindNotFound:
			return domain.Entry{}, err
		}
	}
	return r.discoverRoot(ctx)
}

func (r *Resolver) discoverRoot(ctx context.Context) (domain.Entry, error) {
	unlock, err := r.locker.Lock(ctx)
	if err != nil {
		return domain.Entry{}, err
	}
	defer unlock()

	found, err := r.drive.FindRootFolders(ctx)
	if err != nil {
		return domain.Entry{}, err
	}
	var live []domain.Entry
	for _, e := range found {
		if !e.InTrash {
			live = append(live, e)
		}
	}
	var root domain.Entry
	switch {
	case len(live) == 0 && len(found) > 0:
		return domain.Entry{}, domain.Errorf(domain.KindSetup, rootInTrashMessage)
	case len(live) == 0:
		root, err = r.drive.CreateRootFolder(ctx, RootFolderName)
		if err != nil {
			return domain.Entry{}, err
		}
	default:
		root = live[0]
		for _, e := range live[1:] {
			if e.CreatedTime.Before(root.CreatedTime) {
				root = e
			}
		}
		for _, e := range live {
			if e.ID != root.ID {
				r.warnings = append(r.warnings, fmt.Sprintf("ルートフォルダ（gdrive-cli）が複数あります。古いもの（%s）を使い、%s は使いません: %s", root.ID, e.ID, e.WebViewLink))
			}
		}
	}
	if err := r.state.SetRootFolderID(root.ID); err != nil {
		return domain.Entry{}, err
	}
	return root, nil
}

// Resolve は、パスの項目を返す。Path を埋める。
func (r *Resolver) Resolve(ctx context.Context, p domain.RemotePath) (domain.Entry, error) {
	cur, err := r.Root(ctx)
	if err != nil {
		return domain.Entry{}, err
	}
	for _, seg := range p.Segments() {
		if cur.Type != domain.TypeFolder {
			return domain.Entry{}, domain.Errorf(domain.KindNotFound, "%s はフォルダではありません", cur.Path)
		}
		next, err := r.Lookup(ctx, cur, seg)
		if err != nil {
			return domain.Entry{}, err
		}
		if next == nil {
			return domain.Entry{}, domain.Errorf(domain.KindNotFound, "%s が見つかりません", p)
		}
		cur = *next
	}
	return cur, nil
}

// Lookup は、parent の直下の name を返す。ないときは nil。2 つ以上あれば KindAmbiguous。
func (r *Resolver) Lookup(ctx context.Context, parent domain.Entry, name string) (*domain.Entry, error) {
	name = domain.NormalizeName(name)
	found, err := r.drive.FindChildren(ctx, parent.ID, name)
	if err != nil {
		return nil, err
	}
	path := childPath(parent.Path, name)
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		e := found[0]
		e.Path = path
		return &e, nil
	}
	details := make([]string, 0, len(found))
	for _, e := range found {
		details = append(details, fmt.Sprintf("%s  %s  %s", e.ID, e.ModifiedTime.Format(time.RFC3339), e.WebViewLink))
	}
	return nil, domain.Errorf(domain.KindAmbiguous, "%s に同じ名前のものが 2 つ以上あります", path).WithDetails(details...)
}

func childPath(parentPath, name string) string {
	p, err := domain.ParseRemotePath(parentPath)
	if err != nil {
		p = domain.RootPath()
	}
	return p.Join(name).String()
}

// EnsureFolder は、パスのフォルダを（途中も含めて）なければ作る。作ったパスも返す。
func (r *Resolver) EnsureFolder(ctx context.Context, p domain.RemotePath) (domain.Entry, []domain.RemotePath, error) {
	cur, err := r.Root(ctx)
	if err != nil {
		return domain.Entry{}, nil, err
	}
	unlock, err := r.locker.Lock(ctx)
	if err != nil {
		return domain.Entry{}, nil, err
	}
	defer unlock()

	var created []domain.RemotePath
	at := domain.RootPath()
	for _, seg := range p.Segments() {
		at = at.Join(seg)
		next, err := r.Lookup(ctx, cur, seg)
		if err != nil {
			return domain.Entry{}, nil, err
		}
		switch {
		case next == nil:
			e, err := r.drive.CreateFolder(ctx, cur.ID, seg)
			if err != nil {
				return domain.Entry{}, nil, err
			}
			e.Path = at.String()
			created = append(created, at)
			cur = e
		case next.Type != domain.TypeFolder:
			return domain.Entry{}, nil, domain.Errorf(domain.KindConflict, "%s はファイルです", at)
		default:
			cur = *next
		}
	}
	return cur, created, nil
}
