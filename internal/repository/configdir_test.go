package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDir(t *testing.T) {
	t.Run("初回は作成して0700、2回目は作成しない", func(t *testing.T) {
		// Arrange
		path := filepath.Join(t.TempDir(), "cfg")
		d := NewConfigDir(path)

		// Act
		first, err1 := d.Ensure()
		info, statErr := os.Stat(path)
		second, err2 := d.Ensure()

		// Assert
		if err1 != nil || err2 != nil || statErr != nil {
			t.Fatalf("errors: %v %v %v", err1, err2, statErr)
		}
		if !first || second {
			t.Errorf("created: first=%v second=%v", first, second)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("perm = %v", info.Mode().Perm())
		}
		if d.Path() != path {
			t.Errorf("Path = %q", d.Path())
		}
	})
}
