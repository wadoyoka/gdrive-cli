package winpath

import (
	"regexp"
	"strings"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

// driveLetter は、`C:/` や `c:\` で始まる文字列。
var driveLetter = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// CheckRemoteArg は、ドライブ側のパスとして受け取った引数が、Windows で起きる誤りに当たらないかを確かめる。
//
// 当たるのは、Git Bash が `/videos` を `C:/Program Files/Git/videos` に書き換えたもの（`C:/` や `C:\` で始まる）と、
// `\` を含むもの。当たれば KindUsage のエラーを返す。
func CheckRemoteArg(s string) error {
	if driveLetter.MatchString(s) {
		return domain.Errorf(domain.KindUsage, "ドライブ側のパスが Windows のパスになっています: %q。Git Bash がパスを書き換えたのかもしれません。`//videos` のように先頭の `/` を 2 つにしてください（ルートは `//`）", s)
	}
	if strings.Contains(s, `\`) {
		return domain.Errorf(domain.KindUsage, "ドライブ側のパスは `/` で区切ってください: %q", s)
	}
	return nil
}
