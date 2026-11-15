package config

import (
	"os"
	"runtime"
	"testing"
)

func TestStateFile(t *testing.T) {
	t.Run("保存して読み直すと同じ値で、権限は0600", func(t *testing.T) {
		// Arrange
		dir := t.TempDir()
		sf := NewStateFile(dir)

		// Act
		err := sf.SetRootFolderID("root-1")
		got, err2 := sf.RootFolderID()
		info, statErr := os.Stat(dir + "/state.json")

		// Assert
		if err != nil || err2 != nil || statErr != nil {
			t.Fatalf("errors: %v %v %v", err, err2, statErr)
		}
		if got != "root-1" {
			t.Errorf("got = %q", got)
		}
		t.Run("権限は 0600", func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("Windows ではファイルの権限の数字が効かない")
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("perm = %v", info.Mode().Perm())
			}
		})
	})

	t.Run("ファイルがなければ空文字", func(t *testing.T) {
		// Arrange
		sf := NewStateFile(t.TempDir())

		// Act
		got, err := sf.RootFolderID()

		// Assert
		if err != nil || got != "" {
			t.Errorf("got=%q err=%v", got, err)
		}
	})

	t.Run("壊れた JSON はエラー", func(t *testing.T) {
		// Arrange
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/state.json", []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Act
		_, err := NewStateFile(dir).RootFolderID()

		// Assert
		if err == nil {
			t.Error("エラーになるはず")
		}
	})
}
