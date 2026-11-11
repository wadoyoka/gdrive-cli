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

func TestDefaultConfigDir(t *testing.T) {
	t.Run("GDRIVE_CLI_CONFIG_DIR があれば HOME がなくても決まる", func(t *testing.T) {
		// Arrange
		t.Setenv("HOME", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("GDRIVE_CLI_CONFIG_DIR", "/tmp/gdrive-cli-test")

		// Act
		got, err := DefaultConfigDir()

		// Assert
		if err != nil || got != "/tmp/gdrive-cli-test" {
			t.Errorf("got = %q, err = %v", got, err)
		}
	})

	t.Run("XDG_CONFIG_HOME があれば HOME がなくても決まる", func(t *testing.T) {
		// Arrange
		t.Setenv("HOME", "")
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
		t.Setenv("GDRIVE_CLI_CONFIG_DIR", "")

		// Act
		got, err := DefaultConfigDir()

		// Assert
		if err != nil || got != filepath.Join("/tmp/xdg", "gdrive-cli") {
			t.Errorf("got = %q, err = %v", got, err)
		}
	})

	t.Run("どれもなく HOME もなければエラー", func(t *testing.T) {
		// Arrange
		t.Setenv("HOME", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("GDRIVE_CLI_CONFIG_DIR", "")

		// Act
		_, err := DefaultConfigDir()

		// Assert
		if err == nil {
			t.Error("err = nil")
		}
	})
}
