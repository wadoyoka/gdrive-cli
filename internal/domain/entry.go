package domain

import (
	"strings"
	"time"
)

// EntryType は、項目の種類。
type EntryType string

const (
	// TypeFile は、ファイル。
	TypeFile EntryType = "file"
	// TypeFolder は、フォルダ。
	TypeFolder EntryType = "folder"
)

// FolderMimeType は、Drive でフォルダを表す MIME タイプ。
const FolderMimeType = "application/vnd.google-apps.folder"

const googleAppsPrefix = "application/vnd.google-apps."

// Entry は、ドライブ上のファイルまたはフォルダ。
type Entry struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	Type         EntryType `json:"type"`
	Size         int64     `json:"size"`
	MimeType     string    `json:"mimeType"`
	ModifiedTime time.Time `json:"modifiedTime"`
	WebViewLink  string    `json:"webViewLink"`
	CreatedTime  time.Time `json:"-"`
	InTrash      bool      `json:"-"`
}

// IsGoogleNative は、Google ドキュメントなどの独自形式（フォルダを除く）かどうかを返す。
func IsGoogleNative(mimeType string) bool {
	return strings.HasPrefix(mimeType, googleAppsPrefix) && mimeType != FolderMimeType
}
