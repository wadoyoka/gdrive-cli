// Package browser は、既定のブラウザで URL を開く。
package browser

import (
	"os/exec"
	"runtime"
)

// Open は、url を既定のブラウザで開く。darwin は open、それ以外は xdg-open。
func Open(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, url) //nolint:gosec // 固定のコマンド名で、url は自分で組み立てた値
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
