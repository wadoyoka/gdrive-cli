package domain

import "testing"

func TestIsGoogleNative(t *testing.T) {
	cases := map[string]bool{
		"application/vnd.google-apps.document": true,
		FolderMimeType:                         false,
		"video/mp4":                            false,
	}
	for mt, want := range cases {
		t.Run(mt, func(t *testing.T) {
			// Arrange
			// Act
			got := IsGoogleNative(mt)

			// Assert
			if got != want {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}
