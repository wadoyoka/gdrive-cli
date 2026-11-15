// Package winpath は、Windows のファイル名とパスの決まり。OS を見ない。
package winpath

import "strings"

// reserved は、Windows が予約している名前（大文字）。
var reserved = map[string]bool{
	"CON": true, "CONIN$": true, "CONOUT$": true, "PRN": true, "AUX": true, "NUL": true,
	"COM0": true, "COM1": true, "COM2": true, "COM3": true, "COM4": true,
	"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT0": true, "LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
	"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	"COM¹": true, "COM²": true, "COM³": true,
	"LPT¹": true, "LPT²": true, "LPT³": true,
}

// UnsafeName は、name が Windows のファイル名として使えなければ true を返す。
//
// 使えないのは、`< > : " | ? *` と 0x01–0x1F の文字を含む名前、末尾が `.` か空白の名前、
// 予約された名前（CON・NUL・COM1 など。大文字小文字を区別しない。最初の `.` より前の、
// 末尾の空白を除いた部分で比べるので、con.txt も当たる）。
// `/`・`\`・0x00 は、どの OS でも使えないので、ここでは見ない。
func UnsafeName(name string) bool {
	if strings.ContainsAny(name, `<>:"|?*`) || strings.ContainsFunc(name, func(r rune) bool { return r >= 0x01 && r <= 0x1f }) {
		return true
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return true
	}
	stem, _, _ := strings.Cut(name, ".")
	return reserved[strings.ToUpper(strings.TrimRight(stem, " "))]
}
