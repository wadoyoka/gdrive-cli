package cli

import (
	"context"
	"testing"
)

func TestMvCommand(t *testing.T) {
	t.Run("mv に引数が 1 つだと 2", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withRemote(d)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"mv", "/a"})

		// Assert
		if code != 2 {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("mv に不正なパスを渡すと 2", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withRemote(d)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"mv", "/a/../b", "/c"})

		// Assert
		if code != 2 {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})

	t.Run("mv の移動元がなければ 4", func(t *testing.T) {
		// Arrange
		d, out, errb, _ := newDeps(t)
		d = withRemote(d)
		initAndClear(t, d, out, errb)

		// Act
		code := Execute(context.Background(), d, []string{"mv", "/nope", "/c"})

		// Assert
		if code != 4 {
			t.Errorf("code=%d err=%q", code, errb.String())
		}
	})
}
