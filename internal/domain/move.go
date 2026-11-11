package domain

// MoveTarget は、1 つの移動の計画。Src を DestParent の下へ Name で置く。
type MoveTarget struct {
	Src        RemotePath
	DestParent RemotePath
	Name       string
}

// PlanMove は、srcs を dest へ移す計画を立てる。destType は dest の種類で、dest がなければ nil。
func PlanMove(srcs []RemotePath, dest RemotePath, destType *EntryType) ([]MoveTarget, error) {
	for _, s := range srcs {
		if s.IsRoot() {
			return nil, Errorf(KindUsage, "ルートフォルダは移せません")
		}
	}
	var targets []MoveTarget
	switch {
	case destType == nil:
		if len(srcs) != 1 {
			return nil, Errorf(KindNotFound, "%s が見つかりません", dest)
		}
		if dest.IsRoot() {
			return nil, Errorf(KindUsage, "ルートフォルダは移せません")
		}
		targets = []MoveTarget{{Src: srcs[0], DestParent: dest.Parent(), Name: dest.Base()}}
	case *destType == TypeFolder:
		for _, s := range srcs {
			targets = append(targets, MoveTarget{Src: s, DestParent: dest, Name: s.Base()})
		}
	default:
		return nil, Errorf(KindConflict, "%s は既にあります", dest)
	}
	for _, t := range targets {
		if t.Src.Contains(t.DestParent) {
			return nil, Errorf(KindUsage, "%s を自分自身の中へは移せません", t.Src)
		}
		if t.DestParent.String() == t.Src.Parent().String() && t.Name == t.Src.Base() {
			return nil, Errorf(KindUsage, "移動元と移動先が同じです")
		}
	}
	return targets, nil
}
