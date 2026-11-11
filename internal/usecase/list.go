package usecase

import (
	"context"
	"sort"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

// List は、ドライブ上のフォルダの中身を返す。
type List struct{ Remote Remote }

// Run は、p の項目を Path の順に返す。
// ファイルならそのファイルだけ。フォルダなら中身（recursive なら下の階層もすべて）。
func (u List) Run(ctx context.Context, p domain.RemotePath, recursive bool) ([]domain.Entry, error) {
	s, err := u.Remote.Connect(ctx)
	if err != nil {
		return nil, err
	}
	top, err := s.Resolver.Resolve(ctx, p)
	if err != nil {
		return nil, err
	}
	if top.Type != domain.TypeFolder {
		return []domain.Entry{top}, nil
	}
	out := []domain.Entry{}
	if err := u.walk(ctx, s, top, p, recursive, &out); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (u List) walk(ctx context.Context, s *Session, folder domain.Entry, at domain.RemotePath, recursive bool, out *[]domain.Entry) error {
	children, err := s.Drive.ListChildren(ctx, folder.ID)
	if err != nil {
		return err
	}
	for _, c := range children {
		cp := at.Join(c.Name)
		c.Path = cp.String()
		*out = append(*out, c)
		if recursive && c.Type == domain.TypeFolder {
			if err := u.walk(ctx, s, c, cp, true, out); err != nil {
				return err
			}
		}
	}
	return nil
}
