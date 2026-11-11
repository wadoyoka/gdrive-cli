// Package usecase は、各コマンドの手順と、それが頼る外部の約束（ポート）を持つ。
package usecase

import "github.com/wadoyoka/gdrive-cli/internal/domain"

// ConfigDir は、設定ディレクトリ。
type ConfigDir interface {
	Path() string
	// Ensure は、なければ作る。作ったときだけ true を返す。
	Ensure() (created bool, err error)
}

// PolicyStore は、拒否リストの置き場。
type PolicyStore interface {
	Path() string
	// CreateIfAbsent は、なければ既定の内容で作る。作ったときだけ true を返す。
	CreateIfAbsent() (created bool, err error)
	Load() (domain.Policy, error)
}
