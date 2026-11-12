// Package atomicfile は、途中で止まっても書きかけのファイルを残さない書き込みを持つ。
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write は、同じディレクトリの一時ファイルに書いてから名前を変える。
// 権限は perm にそろえる。
func Write(path string, data []byte, perm os.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("一時ファイルを作れません: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(perm); err != nil {
		return fmt.Errorf("権限を設定できません: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("書けません: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("閉じられません: %w", err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("名前を変えられません: %w", err)
	}
	return nil
}
