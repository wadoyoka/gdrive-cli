package domain

import "testing"

func TestMimeTypeFor(t *testing.T) {
	cases := map[string]string{
		"a.html": "text/html", "a.htm": "text/html",
		"a.mp4": "video/mp4", "a.MP4": "video/mp4",
		"a.mov": "video/quicktime", "a.webm": "video/webm",
		"a.m4a": "audio/mp4", "a.mp3": "audio/mpeg", "a.wav": "audio/wav",
		"a.png": "image/png", "a.jpg": "image/jpeg", "a.jpeg": "image/jpeg",
		"a.gif": "image/gif", "a.svg": "image/svg+xml", "a.webp": "image/webp",
		"a.pdf": "application/pdf", "a.json": "application/json",
		"a.txt": "text/plain", "a.md": "text/markdown", "a.css": "text/css",
		"a.js": "text/javascript", "a.zip": "application/zip",
		"a.xyz123": "application/octet-stream", "noext": "application/octet-stream",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			// Act
			got := MimeTypeFor(name)

			// Assert
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}
