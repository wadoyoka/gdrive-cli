package domain

import (
	"slices"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// NormalizeName は、名前を NFC にそろえる。
func NormalizeName(s string) string { return norm.NFC.String(s) }

// RemotePath は、ドライブ側のパス。区切った名前の並びで持つ（NFC）。
type RemotePath struct {
	segments []string
}

// RootPath は、ルートを返す。
func RootPath() RemotePath { return RemotePath{} }

// ParseRemotePath は、"/a/b" のような文字列をパスにする。空の名前、"."、".." は KindUsage。
func ParseRemotePath(s string) (RemotePath, error) {
	s = strings.Trim(s, "/")
	if s == "" {
		return RootPath(), nil
	}
	parts := strings.Split(s, "/")
	segs := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return RemotePath{}, Errorf(KindUsage, "ドライブ側のパスが正しくありません: %q", s)
		}
		segs = append(segs, NormalizeName(part))
	}
	return RemotePath{segments: segs}, nil
}

// Segments は、名前の並びの写しを返す。
func (p RemotePath) Segments() []string { return slices.Clone(p.segments) }

// IsRoot は、ルートかどうかを返す。
func (p RemotePath) IsRoot() bool { return len(p.segments) == 0 }

// Parent は、親を返す。ルートの親はルート。
func (p RemotePath) Parent() RemotePath {
	if p.IsRoot() {
		return p
	}
	return RemotePath{segments: p.segments[: len(p.segments)-1 : len(p.segments)-1]}
}

// Base は、最後の名前を返す。ルートは空文字。
func (p RemotePath) Base() string {
	if p.IsRoot() {
		return ""
	}
	return p.segments[len(p.segments)-1]
}

// Join は、name を下に足したパスを返す。
func (p RemotePath) Join(name string) RemotePath {
	segs := make([]string, 0, len(p.segments)+1)
	segs = append(segs, p.segments...)
	segs = append(segs, NormalizeName(name))
	return RemotePath{segments: segs}
}

// String は、"/a/b" の形で返す。ルートは "/"。
func (p RemotePath) String() string {
	return "/" + strings.Join(p.segments, "/")
}

// Contains は、q が p と同じか、p の下にあるかを返す。
func (p RemotePath) Contains(q RemotePath) bool {
	if len(q.segments) < len(p.segments) {
		return false
	}
	return slices.Equal(p.segments, q.segments[:len(p.segments)])
}
