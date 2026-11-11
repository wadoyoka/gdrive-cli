package domain

import (
	"strings"
	"testing"
)

func TestPolicy(t *testing.T) {
	t.Run("拒否リストにあるコマンドは拒否される", func(t *testing.T) {
		// Arrange
		p := Policy{Deny: []string{"mv"}}

		// Act
		err := p.Check("mv")

		// Assert
		if KindOf(err) != KindDenied || !strings.Contains(err.Error(), "mv") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("拒否リストにないコマンドは通る", func(t *testing.T) {
		// Arrange
		p := Policy{Deny: []string{"mv"}}

		// Act
		err := p.Check("ls")
		// Assert
		if err != nil {
			t.Errorf("got %v", err)
		}
	})

	t.Run("知らないコマンド名は検証で弾かれる", func(t *testing.T) {
		// Arrange
		p := Policy{Deny: []string{"uplaod"}}

		// Act
		err := p.Validate()

		// Assert
		if KindOf(err) != KindDenied || !strings.Contains(err.Error(), "uplaod") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("正しいコマンド名は検証を通る", func(t *testing.T) {
		// Arrange
		p := Policy{Deny: CommandNames}

		// Act
		err := p.Validate()
		// Assert
		if err != nil {
			t.Errorf("got %v", err)
		}
	})

	t.Run("既定の内容にすべてのコマンド名が書かれている", func(t *testing.T) {
		// Arrange / Act / Assert
		for _, n := range CommandNames {
			if !strings.Contains(DefaultPolicyTOML, n) {
				t.Errorf("%q がない", n)
			}
		}
	})
}
