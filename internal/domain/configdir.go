package domain

import "path/filepath"

// ResolveConfigDir は、設定ディレクトリを決める。
// GDRIVE_CLI_CONFIG_DIR、$XDG_CONFIG_HOME/gdrive-cli、~/.config/gdrive-cli の順。
func ResolveConfigDir(getenv func(string) string, home string) string {
	if d := getenv("GDRIVE_CLI_CONFIG_DIR"); d != "" {
		return d
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "gdrive-cli")
	}
	return filepath.Join(home, ".config", "gdrive-cli")
}
