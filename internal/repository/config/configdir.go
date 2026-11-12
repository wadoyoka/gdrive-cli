// Package config は、設定ディレクトリと、その中のファイル（policy.toml・状態・ロック）の読み書きを持つ。
package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/wadoyoka/gdrive-cli/internal/domain/configdir"
)

// DefaultDir は、環境から設定ディレクトリの場所を決める。
// ホームディレクトリは、GDRIVE_CLI_CONFIG_DIR と XDG_CONFIG_HOME がどちらも空のときだけ調べる。
func DefaultDir() (string, error) {
	home := ""
	if os.Getenv("GDRIVE_CLI_CONFIG_DIR") == "" && os.Getenv("XDG_CONFIG_HOME") == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("ホームディレクトリを調べられません: %w", err)
		}
		home = h
	}
	return configdir.Resolve(os.Getenv, home), nil
}

// Dir は、ディスク上の設定ディレクトリ。
type Dir struct{ path string }

// NewDir は、path の設定ディレクトリを扱う ConfigDir を返す。
func NewDir(path string) *Dir { return &Dir{path: path} }

// Path は、ディレクトリの場所を返す。
func (d *Dir) Path() string { return d.path }

// Ensure は、なければ 0700 で作る。作ったときだけ true を返す。
func (d *Dir) Ensure() (bool, error) {
	if _, err := os.Stat(d.path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("設定ディレクトリを調べられません: %w", err)
	}
	if err := os.MkdirAll(d.path, 0o700); err != nil {
		return false, fmt.Errorf("設定ディレクトリを作れません: %w", err)
	}
	if err := os.Chmod(d.path, 0o700); err != nil { //nolint:gosec // 設定ディレクトリは 0700 にする
		return false, fmt.Errorf("設定ディレクトリの権限を直せません: %w", err)
	}
	return true, nil
}
