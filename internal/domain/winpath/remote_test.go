package winpath

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

func TestCheckRemoteArg(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
		want    string
	}{
		{"Git Bash が書き換えたパス", "C:/Program Files/Git/videos", true, "//videos"},
		{"小文字のドライブと \\", `c:\x`, true, "//videos"},
		{"ドライブ文字だけ", "C:/", true, "//videos"},
		{"先頭が \\", `\videos\2026`, true, "`/` で区切って"},
		{"途中の \\", `videos\2026`, true, "`/` で区切って"},
		{"通常のパス", "/videos", false, ""},
		{"先頭が // のパス", "//videos", false, ""},
		{"// の下のパス", "//videos/2026", false, ""},
		{"ルート //", "//", false, ""},
		{"ルート /", "/", false, ""},
		{"相対パス", "videos/2026", false, ""},
		{"C: という名前のフォルダ", "/C:/x", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange

			// Act
			err := CheckRemoteArg(tc.in)

			// Assert
			if !tc.wantErr {
				if err != nil {
					t.Errorf("err=%v", err)
				}
				return
			}
			var de *domain.Error
			if !errors.As(err, &de) || de.Kind != domain.KindUsage {
				t.Fatalf("err=%v", err)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), fmt.Sprintf("%q", tc.in)) {
				t.Errorf("err=%q", err.Error())
			}
		})
	}
}
