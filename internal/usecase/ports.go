// Package usecase は、各コマンドの手順と、それが頼る外部の約束（ポート）を持つ。
package usecase

import (
	"context"
	"io"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

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

// LoginOptions は、ログインの入出力。
type LoginOptions struct {
	NoBrowser bool
	Prompt    io.Writer // URL と案内を書く先（標準エラー）
	Input     io.Reader // --no-browser のとき、貼り付けを読む元（標準入力）。Login が戻ったあとも読み続けることがあるので、同じ Reader を使い回さないこと
}

// Authenticator は、Google へのログインとトークンの置き場。
type Authenticator interface {
	HasClientSecret() bool
	ImportClientSecret(path string) error
	Login(ctx context.Context, opt LoginOptions) error
	Logout(ctx context.Context) error
	HasToken() bool
}

// Drive は、CLI が使う Google Drive の操作だけを集めたもの。削除と共有の操作は持たない。
type Drive interface {
	AccountEmail(ctx context.Context) (string, error)
	FindRootFolders(ctx context.Context) ([]domain.Entry, error) // ゴミ箱のものも含む。作成日時の古い順
	CreateRootFolder(ctx context.Context, name string) (domain.Entry, error)
	Get(ctx context.Context, id string) (domain.Entry, error)                  // ないときは KindNotFound
	ListChildren(ctx context.Context, parentID string) ([]domain.Entry, error) // ゴミ箱のものを除く。全ページ
	FindChildren(ctx context.Context, parentID, name string) ([]domain.Entry, error)
	CreateFolder(ctx context.Context, parentID, name string) (domain.Entry, error)
	Upload(ctx context.Context, parentID, name, mimeType string, body io.Reader, size int64, progress func(sent int64)) (domain.Entry, error)
	Download(ctx context.Context, id string, w io.Writer, progress func(received int64)) error
	Move(ctx context.Context, id, fromParentID, toParentID, newName string) (domain.Entry, error)
}

// DriveOpener は、ログイン済みの Drive を開く。
type DriveOpener func(ctx context.Context) (Drive, error)

// StateStore は、リポジトリの状態（ルートフォルダの ID）の置き場。
type StateStore interface {
	// RootFolderID は、保存した ID を返す。なければ空文字。
	RootFolderID() (string, error)
	SetRootFolderID(id string) error
}

// Locker は、ほかの gdrive-cli と同時に書き込まないための排他ロック。
type Locker interface {
	Lock(ctx context.Context) (unlock func(), err error)
}
