package remotefs

import (
	"context"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

// Mkdir は、ドライブ上にフォルダを作る。
type Mkdir struct{ Remote usecase.Remote }

// Run は、paths を引数の順に作り、最初のエラーで止まる。作れたものを返す。
// parents が true なら、途中のフォルダも作り、既にあってもエラーにしない。
func (u Mkdir) Run(ctx context.Context, paths []domain.RemotePath, parents bool) ([]domain.Entry, error) {
	s, err := u.Remote.Connect(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Entry, 0, len(paths))
	for _, p := range paths {
		var e domain.Entry
		if parents {
			e, _, err = s.Resolver.EnsureFolder(ctx, p)
		} else {
			e, err = u.mkdirOne(ctx, s, p)
		}
		if err != nil {
			return out, err
		}
		out = append(out, e)
	}
	return out, nil
}

// mkdirOne は、親を解決してからロックを取り、確かめてから作る。
// Root と Resolve はロックを取るので、ロックの前に済ませる。
func (u Mkdir) mkdirOne(ctx context.Context, s *usecase.Session, p domain.RemotePath) (domain.Entry, error) {
	if p.IsRoot() {
		return domain.Entry{}, domain.Errorf(domain.KindConflict, "%s は既にあります", p)
	}
	parent, err := s.Resolver.Resolve(ctx, p.Parent())
	if err != nil {
		return domain.Entry{}, err
	}
	if parent.Type != domain.TypeFolder {
		return domain.Entry{}, domain.Errorf(domain.KindNotFound, "%s はフォルダではありません", p.Parent())
	}
	unlock, err := u.Remote.Locker.Lock(ctx)
	if err != nil {
		return domain.Entry{}, err
	}
	defer unlock()
	existing, err := s.Resolver.Lookup(ctx, parent, p.Base())
	if err != nil {
		return domain.Entry{}, err
	}
	if existing != nil {
		return domain.Entry{}, domain.Errorf(domain.KindConflict, "%s は既にあります", p)
	}
	e, err := s.Drive.CreateFolder(ctx, parent.ID, domain.NormalizeName(p.Base()))
	if err != nil {
		return domain.Entry{}, err
	}
	e.Path = p.String()
	return e, nil
}
