package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

func TestMkdirCommand(t *testing.T) {
	t.Run("--json で途中で失敗したら、作れたものを標準出力に、エラーを標準エラーに出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withPartial(d, "y")
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"--json", "mkdir", "/x", "/y", "/z"})

		// Assert
		var entries []domain.Entry
		if err := json.Unmarshal(out.Bytes(), &entries); err != nil {
			t.Fatalf("json: %v: %q", err, out.String())
		}
		if code != 1 || len(entries) != 1 || entries[0].Path != "/x" || entries[0].ID != "new-x" {
			t.Errorf("code=%d entries=%+v", code, entries)
		}
		if !strings.Contains(errb.String(), `"kind":"unexpected"`) || !strings.Contains(errb.String(), "backend error") {
			t.Errorf("err=%q", errb.String())
		}
	})

	t.Run("途中で失敗したら、作れたものを 1 行ずつ出す", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withPartial(d, "y")
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"mkdir", "/x", "/y"})

		// Assert
		if want := "/x\thttps://example.com/new-x\n"; code != 1 || out.String() != want {
			t.Errorf("code=%d out=%q", code, out.String())
		}
	})

	t.Run("--json で最初に失敗したら、標準出力には何も出さない", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withPartial(d, "x")
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"--json", "mkdir", "/x", "/y"})

		// Assert
		if code != 1 || out.Len() != 0 || !strings.Contains(errb.String(), `"kind":"unexpected"`) {
			t.Errorf("code=%d out=%q err=%q", code, out.String(), errb.String())
		}
	})
}
