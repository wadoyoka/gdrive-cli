package domain

// Action は、送受信の計画で 1 項目に対して行うこと。JSON にはこの文字列で出す。
type Action string

const (
	// ActionCreate は、新しく作る。
	ActionCreate Action = "created"
	// ActionMerge は、既にあるフォルダに合流する。
	ActionMerge Action = "merged"
	// ActionSkip は、同じ名前のファイルがあるので飛ばす。
	ActionSkip Action = "skipped"
	// ActionOverwrite は、同じ名前のファイルを置き換える（手元への download だけで使う）。
	ActionOverwrite Action = "overwritten"
)

// ConflictMode は、ファイルとファイルで名前が衝突したときの扱い。
type ConflictMode int

const (
	// ConflictError は、衝突として止める。
	ConflictError ConflictMode = iota
	// ConflictSkip は、飛ばす。
	ConflictSkip
	// ConflictOverwrite は、置き換える。
	ConflictOverwrite
)

// Decide は、送るもの（src）と、送り先に同じ名前であるもの（なければ nil）から、することを決める。
//   - 送り先に何もなければ ActionCreate。
//   - フォルダとフォルダなら ActionMerge。
//   - ファイルとファイルなら mode で決める（ConflictError なら KindConflict）。
//   - 種類が違えば、mode に関係なく KindConflict。
func Decide(src EntryType, existing *EntryType, mode ConflictMode) (Action, error) {
	switch {
	case existing == nil:
		return ActionCreate, nil
	case src != *existing:
		return "", Errorf(KindConflict, "種類の違う同じ名前のものがあります")
	case src == TypeFolder:
		return ActionMerge, nil
	}
	switch mode {
	case ConflictSkip:
		return ActionSkip, nil
	case ConflictOverwrite:
		return ActionOverwrite, nil
	default:
		return "", Errorf(KindConflict, "同じ名前のファイルがあります")
	}
}
