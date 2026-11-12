package domain

import (
	"slices"
	"strings"
)

// CommandNames は、拒否リストに書ける、実行できるコマンドの名前。
var CommandNames = []string{"setup", "login", "logout", "status", "mkdir", "upload", "download", "mv", "ls"}

// DefaultPolicyTOML は、init が作る policy.toml の初期の内容。
const DefaultPolicyTOML = `# gdrive-cli で実行を拒否するコマンドの一覧。
# ここに書いたコマンドを実行しようとすると、終了コード 7 で止まる。
# 書ける名前: setup, login, logout, status, mkdir, upload, download, mv, ls
# 例: deny = ["mv", "download"]
deny = []
`

// Policy は、実行を拒否するコマンドの一覧。
type Policy struct{ Deny []string }

// Validate は、知らないコマンド名が含まれていれば KindDenied を返す。
func (p Policy) Validate() error {
	for _, name := range p.Deny {
		if !slices.Contains(CommandNames, name) {
			return Errorf(KindDenied, "policy.toml に知らないコマンド名があるため、すべてのコマンドを止めています: %s", name).
				WithDetails("書ける名前: " + strings.Join(CommandNames, ", "))
		}
	}
	return nil
}

// Check は、command が拒否されていれば KindDenied を返す。
func (p Policy) Check(command string) error {
	if slices.Contains(p.Deny, command) {
		return Errorf(KindDenied, "%s は policy.toml で拒否されています", command)
	}
	return nil
}
