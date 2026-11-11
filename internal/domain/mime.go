package domain

import (
	"path"
	"strings"
)

const defaultMimeType = "application/octet-stream"

// 環境に依存しないよう、標準の mime パッケージは使わず自前の表だけを使う。
var mimeTypes = map[string]string{
	".html": "text/html", ".htm": "text/html",
	".mp4": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm",
	".m4a": "audio/mp4", ".mp3": "audio/mpeg", ".wav": "audio/wav",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".svg": "image/svg+xml", ".webp": "image/webp",
	".pdf": "application/pdf", ".json": "application/json",
	".txt": "text/plain", ".md": "text/markdown", ".css": "text/css",
	".js": "text/javascript", ".zip": "application/zip",
}

// MimeTypeFor は、ファイル名の拡張子から MIME タイプを返す。知らなければ application/octet-stream。
func MimeTypeFor(name string) string {
	if t, ok := mimeTypes[strings.ToLower(path.Ext(name))]; ok {
		return t
	}
	return defaultMimeType
}
