package domain

import "testing"

func TestResolveConfigDir(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"環境変数なし", nil, "/h/.config/gdrive-cli"},
		{"XDG_CONFIG_HOME", map[string]string{"XDG_CONFIG_HOME": "/x"}, "/x/gdrive-cli"},
		{"GDRIVE_CLI_CONFIG_DIR", map[string]string{"GDRIVE_CLI_CONFIG_DIR": "/c"}, "/c"},
		{"両方あれば GDRIVE_CLI_CONFIG_DIR", map[string]string{"GDRIVE_CLI_CONFIG_DIR": "/c", "XDG_CONFIG_HOME": "/x"}, "/c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Arrange
			getenv := func(k string) string { return c.vars[k] }

			// Act
			got := ResolveConfigDir(getenv, "/h")

			// Assert
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
