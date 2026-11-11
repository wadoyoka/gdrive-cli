package usecase

import (
	"context"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

// Move は、ドライブ上のファイルやフォルダを移す（名前の変更も含む）。
type Move struct{ Remote Remote }

// MoveItem は、移した 1 件。
type MoveItem struct {
	From        string `json:"from"`
	To          string `json:"to"`
	ID          string `json:"id"`
	WebViewLink string `json:"webViewLink"`
}

type plannedMove struct {
	src    domain.Entry
	target domain.MoveTarget
	fromID string
	toID   string
}

// Run は、srcs を dest へ移す。すべてを確かめてから移し始めるので、確かめで失敗すれば 1 つも移さない。
func (u Move) Run(ctx context.Context, srcs []domain.RemotePath, dest domain.RemotePath) ([]MoveItem, error) {
	s, err := u.Remote.Connect(ctx)
	if err != nil {
		return nil, err
	}
	plan, err := u.plan(ctx, s, srcs, dest)
	if err != nil {
		return nil, err
	}
	out := make([]MoveItem, 0, len(plan))
	for _, m := range plan {
		e, err := s.Drive.Move(ctx, m.src.ID, m.fromID, m.toID, m.target.Name)
		if err != nil {
			return out, err
		}
		out = append(out, MoveItem{
			From:        m.target.Src.String(),
			To:          m.target.DestParent.Join(m.target.Name).String(),
			ID:          e.ID,
			WebViewLink: e.WebViewLink,
		})
	}
	return out, nil
}

// plan は、移す前の確かめをすべて済ませる。
func (u Move) plan(ctx context.Context, s *Session, srcs []domain.RemotePath, dest domain.RemotePath) ([]plannedMove, error) {
	entries := make([]domain.Entry, 0, len(srcs))
	for _, p := range srcs {
		e, err := s.Resolver.Resolve(ctx, p)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	var destType *domain.EntryType
	d, err := s.Resolver.Resolve(ctx, dest)
	switch {
	case err == nil:
		destType = &d.Type
	case domain.KindOf(err) != domain.KindNotFound:
		return nil, err
	}
	targets, err := domain.PlanMove(srcs, dest, destType)
	if err != nil {
		return nil, err
	}
	plan := make([]plannedMove, 0, len(targets))
	seen := map[string]bool{}
	for i, t := range targets {
		parent, err := s.Resolver.Resolve(ctx, t.DestParent)
		if err != nil {
			return nil, err
		}
		if parent.Type != domain.TypeFolder {
			return nil, domain.Errorf(domain.KindNotFound, "%s はフォルダではありません", t.DestParent)
		}
		to := t.DestParent.Join(t.Name).String()
		if seen[to] {
			return nil, domain.Errorf(domain.KindConflict, "%s に移すものが重なっています", to)
		}
		seen[to] = true
		existing, err := s.Resolver.Lookup(ctx, parent, t.Name)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return nil, domain.Errorf(domain.KindConflict, "%s は既にあります", to)
		}
		from, err := s.Resolver.Resolve(ctx, t.Src.Parent())
		if err != nil {
			return nil, err
		}
		plan = append(plan, plannedMove{src: entries[i], target: t, fromID: from.ID, toID: parent.ID})
	}
	return plan, nil
}
