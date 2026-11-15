package plan

import "github.com/wadoyoka/gdrive-cli/internal/domain"

// MoveTarget は、1 つの移動の計画。Src を DestParent の下へ Name で置く。
type MoveTarget struct {
	Src        domain.RemotePath
	DestParent domain.RemotePath
	Name       string
}

// Move は、srcs を dest へ移す計画を立てる。destType は dest の種類で、dest がなければ nil。
func Move(srcs []domain.RemotePath, dest domain.RemotePath, destType *domain.EntryType) ([]MoveTarget, error) {
	for _, s := range srcs {
		if s.IsRoot() {
			return nil, domain.Errorf(domain.KindUsage, "ルートフォルダは移せません")
		}
	}
	var targets []MoveTarget
	switch {
	case destType == nil:
		if len(srcs) != 1 {
			return nil, domain.Errorf(domain.KindNotFound, "%s が見つかりません", dest)
		}
		if dest.IsRoot() {
			return nil, domain.Errorf(domain.KindUsage, "ルートフォルダは移せません")
		}
		targets = []MoveTarget{{Src: srcs[0], DestParent: dest.Parent(), Name: dest.Base()}}
	case *destType == domain.TypeFolder:
		for _, s := range srcs {
			targets = append(targets, MoveTarget{Src: s, DestParent: dest, Name: s.Base()})
		}
	default:
		return nil, domain.Errorf(domain.KindConflict, "%s は既にあります", dest)
	}
	for _, t := range targets {
		if t.Src.Contains(t.DestParent) {
			return nil, domain.Errorf(domain.KindUsage, "%s を自分自身の中へは移せません", t.Src)
		}
		if t.DestParent.String() == t.Src.Parent().String() && t.Name == t.Src.Base() {
			return nil, domain.Errorf(domain.KindUsage, "移動元と移動先が同じです")
		}
	}
	return targets, nil
}
