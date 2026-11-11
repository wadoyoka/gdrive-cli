// Package repository は、ファイルや外部サービスなど、外の世界とのやりとりを持つ。
package repository

import (
	"errors"
	"fmt"
	"os"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

// DefaultConfigDir は、環境から設定ディレクトリの場所を決める。
func DefaultConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ホームディレクトリを調べられません: %w", err)
	}
	return domain.ResolveConfigDir(os.Getenv, home), nil
}

// ConfigDir は、ディスク上の設定ディレクトリ。
type ConfigDir struct{ path string }

// NewConfigDir は、path の設定ディレクトリを扱う ConfigDir を返す。
func NewConfigDir(path string) *ConfigDir { return &ConfigDir{path: path} }

// Path は、ディレクトリの場所を返す。
func (d *ConfigDir) Path() string { return d.path }

// Ensure は、なければ 0700 で作る。作ったときだけ true を返す。
func (d *ConfigDir) Ensure() (bool, error) {
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
