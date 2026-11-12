package config

import (
	"context"
	"testing"
	"time"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

func TestLockFile(t *testing.T) {
	t.Run("1つ目を持っている間は2つ目がエラーになり、解放すると取れる", func(t *testing.T) {
		// Arrange
		dir := t.TempDir()
		first, err := NewLockFile(dir).Lock(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()

		// Act
		_, errBusy := NewLockFile(dir).Lock(ctx)
		first()
		second, errFree := NewLockFile(dir).Lock(context.Background())

		// Assert
		if domain.KindOf(errBusy) != domain.KindUnexpected || errBusy == nil {
			t.Errorf("errBusy = %v", errBusy)
		}
		if errFree != nil {
			t.Fatalf("errFree = %v", errFree)
		}
		second()
	})
}
