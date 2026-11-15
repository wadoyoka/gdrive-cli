// Package transfer は、手元とドライブの間で送受信する手順（upload・download）を持つ。
package transfer

import (
	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/domain/plan"
)

// ProgressFunc は、1 ファイルの送受信の進み具合を知らせる。path はドライブ側のパス。
// 複数のファイルを同時に送受信するので、別々のゴルーチンから同時に呼ばれることがある。
type ProgressFunc func(path string, done, total int64)

// Item は、送受信の結果の 1 項目（ファイルまたはフォルダ）。
type Item struct {
	LocalPath   string           `json:"localPath"`
	RemotePath  string           `json:"remotePath"`
	ID          string           `json:"id,omitempty"`
	Type        domain.EntryType `json:"type"`
	Size        int64            `json:"size"`
	WebViewLink string           `json:"webViewLink,omitempty"`
	Action      plan.Action      `json:"action"`
	Error       string           `json:"error,omitempty"`
}

// Result は、送受信の結果。
type Result struct {
	Items    []Item   `json:"items"` // RemotePath の順
	Warnings []string `json:"warnings,omitempty"`
}
