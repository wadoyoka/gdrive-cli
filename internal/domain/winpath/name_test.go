package winpath

import "testing"

func TestUnsafeName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"コロン", "a:b.txt", true},
		{"小なり", "a<b", true},
		{"大なり", "a>b", true},
		{"二重引用符", `a"b`, true},
		{"縦線", "a|b", true},
		{"疑問符", "a?b", true},
		{"アスタリスク", "a*b", true},
		{"制御文字 0x01", "a\x01b", true},
		{"制御文字 0x1f", "a\x1fb", true},
		{"末尾がドット", "a.", true},
		{"末尾が空白", "a ", true},
		{"CON", "CON", true},
		{"拡張子つきの con", "con.TXT", true},
		{"複数の拡張子つきの Nul", "Nul.tar.gz", true},
		{"PRN", "PRN", true},
		{"AUX", "AUX", true},
		{"CONIN$", "CONIN$", true},
		{"拡張子つきの conout$", "conout$.txt", true},
		{"COM0", "COM0", true},
		{"COM9", "COM9", true},
		{"LPT1", "LPT1", true},
		{"上付きの COM1", "COM¹", true},
		{"上付きの LPT3", "LPT³", true},
		{"予約名の後ろの空白", "CON .txt", true},
		{"普通の名前", "report.txt", false},
		{"予約名で始まるだけ", "CONSOLE.txt", false},
		{"CON2", "CON2.txt", false},
		{"LPT10", "LPT10", false},
		{"nul_x", "nul_x", false},
		{"ドットで始まる", ".gitignore", false},
		{"日本語", "日本語.txt", false},
		{"途中に空白", "a b.txt", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange
			// Act
			got := UnsafeName(c.in)
			// Assert
			if got != c.want {
				t.Errorf("UnsafeName(%q)=%v, want %v", c.in, got, c.want)
			}
		})
	}
}
