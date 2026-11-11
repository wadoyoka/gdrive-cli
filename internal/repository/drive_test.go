package repository

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

func newTestDrive(t *testing.T, h http.HandlerFunc) (*DriveClient, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	svc, err := drive.NewService(context.Background(),
		option.WithEndpoint(srv.URL+"/"), option.WithHTTPClient(srv.Client()), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("drive.NewService: %v", err)
	}
	c := NewDriveClient(svc)
	slept := &[]time.Duration{}
	c.sleep = func(d time.Duration) { *slept = append(*slept, d) }
	return c, slept
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestEscapeQuery(t *testing.T) {
	t.Run("アポストロフィとバックスラッシュを退避する", func(t *testing.T) {
		// Arrange
		cases := map[string]string{`it's`: `it\'s`, `a\b`: `a\\b`, `plain`: `plain`}
		for in, want := range cases {
			// Act
			got := escapeQuery(in)
			// Assert
			if got != want {
				t.Errorf("escapeQuery(%q) = %q, want %q", in, got, want)
			}
		}
	})
}

func TestDriveClient(t *testing.T) {
	t.Run("FindChildren は名前を正しく包む", func(t *testing.T) {
		// Arrange
		var q string
		c, _ := newTestDrive(t, func(w http.ResponseWriter, r *http.Request) {
			q = r.URL.Query().Get("q")
			writeJSON(w, map[string]any{"files": []any{}})
		})
		// Act
		_, err := c.FindChildren(context.Background(), "P1", "it's.mp4")
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if want := `'P1' in parents and name='it\'s.mp4' and trashed=false`; q != want {
			t.Errorf("q = %q, want %q", q, want)
		}
	})

	t.Run("FindRootFolders は印で探す", func(t *testing.T) {
		// Arrange
		var q, order string
		c, _ := newTestDrive(t, func(w http.ResponseWriter, r *http.Request) {
			q, order = r.URL.Query().Get("q"), r.URL.Query().Get("orderBy")
			writeJSON(w, map[string]any{"files": []any{}})
		})
		// Act
		_, err := c.FindRootFolders(context.Background())
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		want := `appProperties has { key='gdriveCliRoot' and value='1' } and mimeType='application/vnd.google-apps.folder'`
		if q != want || order != "createdTime" {
			t.Errorf("q = %q, orderBy = %q", q, order)
		}
	})

	t.Run("CreateRootFolder は印を付けて直下に作る", func(t *testing.T) {
		// Arrange
		var body drive.File
		c, _ := newTestDrive(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSON(w, map[string]any{"id": "R1", "name": "gdrive-cli", "mimeType": domain.FolderMimeType})
		})
		// Act
		e, err := c.CreateRootFolder(context.Background(), "gdrive-cli")
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if body.Name != "gdrive-cli" || body.MimeType != domain.FolderMimeType ||
			len(body.Parents) != 1 || body.Parents[0] != "root" || body.AppProperties["gdriveCliRoot"] != "1" {
			t.Errorf("body = %+v", body)
		}
		if e.ID != "R1" || e.Type != domain.TypeFolder {
			t.Errorf("entry = %+v", e)
		}
	})

	t.Run("ListChildren は全ページをたどる", func(t *testing.T) {
		// Arrange
		c, _ := newTestDrive(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("pageToken") == "" {
				writeJSON(w, map[string]any{"nextPageToken": "T2", "files": []any{map[string]any{"id": "1"}, map[string]any{"id": "2"}}})
				return
			}
			writeJSON(w, map[string]any{"files": []any{map[string]any{"id": "3"}}})
		})
		// Act
		got, err := c.ListChildren(context.Background(), "P1")
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[2].ID != "3" {
			t.Errorf("got = %+v", got)
		}
	})

	t.Run("Move は親が変わるときだけ付け替える", func(t *testing.T) {
		// Arrange
		var query []url.Values
		var names []string
		c, _ := newTestDrive(t, func(w http.ResponseWriter, r *http.Request) {
			var b drive.File
			_ = json.NewDecoder(r.Body).Decode(&b)
			names = append(names, b.Name)
			query = append(query, r.URL.Query())
			writeJSON(w, map[string]any{"id": "F1"})
		})
		// Act
		_, err1 := c.Move(context.Background(), "F1", "P1", "P2", "new.txt")
		_, err2 := c.Move(context.Background(), "F1", "P1", "P1", "same.txt")
		// Assert
		if err1 != nil || err2 != nil {
			t.Fatal(err1, err2)
		}
		if query[0].Get("addParents") != "P2" || query[0].Get("removeParents") != "P1" || names[0] != "new.txt" {
			t.Errorf("1回目: %v %v", query[0], names[0])
		}
		if query[1].Has("addParents") || query[1].Has("removeParents") || names[1] != "same.txt" {
			t.Errorf("2回目: %v %v", query[1], names[1])
		}
	})

	t.Run("Get の 404 は KindNotFound", func(t *testing.T) {
		// Arrange
		c, _ := newTestDrive(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":404,"message":"File not found"}}`))
		})
		// Act
		_, err := c.Get(context.Background(), "nope")
		// Assert
		if domain.KindOf(err) != domain.KindNotFound {
			t.Errorf("kind = %v, err = %v", domain.KindOf(err), err)
		}
	})

	t.Run("503 が2回続いても3回目で成功する", func(t *testing.T) {
		// Arrange
		calls := 0
		c, slept := newTestDrive(t, func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			writeJSON(w, map[string]any{"id": "F1"})
		})
		// Act
		e, err := c.Get(context.Background(), "F1")
		// Assert
		if err != nil || e.ID != "F1" {
			t.Fatalf("e = %+v, err = %v", e, err)
		}
		if len(*slept) != 2 || (*slept)[0] != time.Second || (*slept)[1] != 2*time.Second {
			t.Errorf("slept = %v", *slept)
		}
	})

	t.Run("リフレッシュ失敗は KindSetup、それ以外は KindUnexpected になる", func(t *testing.T) {
		// Arrange
		// Act
		got1 := mapError(&url.Error{Op: "Get", URL: "x", Err: &oauth2.RetrieveError{Response: &http.Response{StatusCode: 400}, ErrorCode: "invalid_grant"}})
		got2 := mapError(io.ErrClosedPipe)
		// Assert
		if domain.KindOf(got1) != domain.KindSetup || !strings.Contains(got1.Error(), "gdrive-cli login") {
			t.Errorf("got1 = %v", got1)
		}
		if domain.KindOf(got2) != domain.KindUnexpected {
			t.Errorf("got2 = %v", got2)
		}
	})

	t.Run("フォルダ作成は 503 でもやり直さない", func(t *testing.T) {
		// Arrange
		calls := 0
		c, slept := newTestDrive(t, func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.WriteHeader(http.StatusServiceUnavailable)
		})
		// Act
		_, err1 := c.CreateFolder(context.Background(), "P1", "dir")
		_, err2 := c.CreateRootFolder(context.Background(), "gdrive-cli")
		// Assert
		if calls != 2 || len(*slept) != 0 {
			t.Errorf("calls = %d, slept = %v", calls, *slept)
		}
		if domain.KindOf(err1) != domain.KindUnexpected || domain.KindOf(err2) != domain.KindUnexpected {
			t.Errorf("err1 = %v, err2 = %v", err1, err2)
		}
	})

	t.Run("項目への変換", func(t *testing.T) {
		// Arrange
		c, _ := newTestDrive(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/d") {
				writeJSON(w, map[string]any{"id": "d", "mimeType": domain.FolderMimeType})
				return
			}
			writeJSON(w, map[string]any{
				"id": "f", "name": "a.txt", "size": "123", "mimeType": "text/plain",
				"modifiedTime": "2026-01-02T03:04:05.000Z", "trashed": true,
			})
		})
		// Act
		f, err1 := c.Get(context.Background(), "f")
		d, err2 := c.Get(context.Background(), "d")
		// Assert
		if err1 != nil || err2 != nil {
			t.Fatal(err1, err2)
		}
		if f.Size != 123 || f.Type != domain.TypeFile || !f.InTrash || f.ModifiedTime.Year() != 2026 {
			t.Errorf("f = %+v", f)
		}
		if d.Type != domain.TypeFolder {
			t.Errorf("d = %+v", d)
		}
	})

	t.Run("Download は中身を書く", func(t *testing.T) {
		// Arrange
		c, _ := newTestDrive(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("alt") != "media" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte("hello bytes"))
		})
		var sb strings.Builder
		var last int64
		// Act
		err := c.Download(context.Background(), "F1", &sb, func(n int64) { last = n })
		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if sb.String() != "hello bytes" || last != 11 {
			t.Errorf("got %q, last = %d", sb.String(), last)
		}
	})
}
