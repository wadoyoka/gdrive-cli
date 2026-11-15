//go:build windows

package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWriteWindows(t *testing.T) {
	t.Run("ほかのプロセスが読み続けていても、置き換えが失敗しない", func(t *testing.T) {
		// Arrange
		path := filepath.Join(t.TempDir(), "target.json")
		if err := Write(path, []byte("first"), 0o600); err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		var wg sync.WaitGroup
		var readErr error
		wg.Go(func() {
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := os.ReadFile(path); err != nil && !errors.Is(err, fs.ErrNotExist) { //nolint:gosec // テストが作った一時ファイル
					readErr = err
					return
				}
			}
		})

		// Act
		var writeErr error
		for range 50 {
			if writeErr = Write(path, []byte("data"), 0o600); writeErr != nil {
				break
			}
		}
		close(stop)
		wg.Wait()

		// Assert
		if writeErr != nil {
			t.Errorf("Write: %v", writeErr)
		}
		if readErr != nil {
			t.Errorf("読む側のエラー: %v", readErr)
		}
	})
}
