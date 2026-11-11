package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
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

	t.Run("エラーには URL の問い合わせ部分を残さない", func(t *testing.T) {
		// Arrange
		err := &url.Error{Op: "Put", URL: "https://example.com/upload/drive/v3/files?uploadType=resumable&upload_id=SESSION", Err: io.ErrUnexpectedEOF}
		cause := domain.Errorf(domain.KindConflict, "a.txt を送れません").Wrap(&url.Error{Op: "Post", URL: "https://example.com/files?upload_id=SESSION2", Err: io.EOF})

		// Act
		got1 := mapError(err)
		got2 := mapError(cause)

		// Assert
		if s := got1.Error(); strings.Contains(s, "SESSION") || !strings.Contains(s, `Put "https://example.com/upload/drive/v3/files"`) {
			t.Errorf("got1 = %q", s)
		}
		if s := got2.Error(); strings.Contains(s, "SESSION2") || domain.KindOf(got2) != domain.KindConflict {
			t.Errorf("got2 = %q", s)
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

// uploadServer は、Upload を試すための偽の Drive。
// アップロードの要求には uploads の順に応え、一覧の要求には listed を返す。
type uploadServer struct {
	mu      sync.Mutex
	uploads []func(w http.ResponseWriter, r *http.Request) // n 回目のアップロードの応え方。尽きたら最後のものを使う
	listed  []map[string]any                               // FindChildren に返すファイル
	bodies  []string                                       // 受け取ったアップロードの本文
	lists   int                                            // 一覧の要求の回数
}

func (s *uploadServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/files") {
		s.lists++
		files := s.listed
		if files == nil {
			files = []map[string]any{}
		}
		writeJSON(w, map[string]any{"files": files})
		return
	}
	if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/upload/") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	b, _ := io.ReadAll(r.Body)
	s.bodies = append(s.bodies, string(b))
	respond := s.uploads[min(len(s.bodies), len(s.uploads))-1]
	respond(w, r)
}

func uploadStatus(code int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = fmt.Fprintf(w, `{"error":{"code":%d,"message":"x"}}`, code)
	}
}

func uploadCreated(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"id": "F1", "name": "a.txt", "size": "5", "mimeType": "text/plain"})
}

// onlySeq は、io.Seeker を隠した Reader。
type onlySeq struct{ r io.Reader }

func (o onlySeq) Read(p []byte) (int, error) { return o.r.Read(p) }

func TestDriveClientUpload(t *testing.T) {
	const content = "hello"

	t.Run("429 で断られたら、先頭から送り直して 1 つだけ作る", func(t *testing.T) {
		// Arrange
		s := &uploadServer{uploads: []func(http.ResponseWriter, *http.Request){uploadStatus(http.StatusTooManyRequests), uploadCreated}}
		c, slept := newTestDrive(t, s.handle)
		var reported []int64
		// Act
		e, err := c.Upload(context.Background(), "P1", "a.txt", "text/plain", strings.NewReader(content), 5, func(n int64) { reported = append(reported, n) })
		// Assert
		if err != nil || e.ID != "F1" {
			t.Fatalf("e = %+v, err = %v", e, err)
		}
		if len(s.bodies) != 2 || !strings.Contains(s.bodies[1], content) {
			t.Errorf("bodies = %q", s.bodies)
		}
		if s.lists != 0 {
			t.Errorf("断られたときは探さないはず: lists = %d", s.lists)
		}
		if len(*slept) != 1 || (*slept)[0] != time.Second {
			t.Errorf("slept = %v", *slept)
		}
		if len(reported) == 0 || reported[0] != 0 {
			t.Errorf("送り直すときは進捗を 0 に戻すはず: reported = %v", reported)
		}
	})

	t.Run("503 でもファイルができていれば、送り直さずにそれを返す", func(t *testing.T) {
		// Arrange
		s := &uploadServer{uploads: []func(http.ResponseWriter, *http.Request){uploadStatus(http.StatusServiceUnavailable)}}
		s.listed = []map[string]any{{"id": "F9", "name": "a.txt", "size": "5", "mimeType": "text/plain"}}
		c, _ := newTestDrive(t, s.handle)
		// Act
		e, err := c.Upload(context.Background(), "P1", "a.txt", "text/plain", strings.NewReader(content), 5, nil)
		// Assert
		if err != nil || e.ID != "F9" {
			t.Fatalf("e = %+v, err = %v", e, err)
		}
		if len(s.bodies) != 1 || s.lists != 1 {
			t.Errorf("uploads = %d, lists = %d", len(s.bodies), s.lists)
		}
	})

	t.Run("503 でファイルができていなければ、送り直して成功する", func(t *testing.T) {
		// Arrange
		s := &uploadServer{uploads: []func(http.ResponseWriter, *http.Request){uploadStatus(http.StatusServiceUnavailable), uploadCreated}}
		c, _ := newTestDrive(t, s.handle)
		// Act
		e, err := c.Upload(context.Background(), "P1", "a.txt", "text/plain", strings.NewReader(content), 5, nil)
		// Assert
		if err != nil || e.ID != "F1" {
			t.Fatalf("e = %+v, err = %v", e, err)
		}
		if len(s.bodies) != 2 || !strings.Contains(s.bodies[1], content) || s.lists != 1 {
			t.Errorf("bodies = %q, lists = %d", s.bodies, s.lists)
		}
	})

	t.Run("503 が続けば 5 回で諦める", func(t *testing.T) {
		// Arrange
		s := &uploadServer{uploads: []func(http.ResponseWriter, *http.Request){uploadStatus(http.StatusServiceUnavailable)}}
		c, slept := newTestDrive(t, s.handle)
		// Act
		_, err := c.Upload(context.Background(), "P1", "a.txt", "text/plain", strings.NewReader(content), 5, nil)
		// Assert
		if domain.KindOf(err) != domain.KindUnexpected {
			t.Fatalf("err = %v", err)
		}
		want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
		if len(s.bodies) != 5 || !slices.Equal(*slept, want) {
			t.Errorf("uploads = %d, slept = %v", len(s.bodies), *slept)
		}
	})

	t.Run("送り直す前に見つかったものが送ったものか分からなければ、送り直さずに衝突にする", func(t *testing.T) {
		cases := []struct {
			name   string
			listed []map[string]any
		}{
			{"大きさが違う", []map[string]any{{"id": "F9", "name": "a.txt", "size": "4"}}},
			{"2 つある", []map[string]any{{"id": "F8", "name": "a.txt", "size": "5"}, {"id": "F9", "name": "a.txt", "size": "5"}}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				// Arrange
				s := &uploadServer{uploads: []func(http.ResponseWriter, *http.Request){uploadStatus(http.StatusBadGateway)}, listed: tc.listed}
				c, _ := newTestDrive(t, s.handle)
				// Act
				_, err := c.Upload(context.Background(), "P1", "a.txt", "text/plain", strings.NewReader(content), 5, nil)
				// Assert
				if domain.KindOf(err) != domain.KindConflict || !strings.Contains(err.Error(), "F9") {
					t.Errorf("err = %v", err)
				}
				if len(s.bodies) != 1 {
					t.Errorf("uploads = %d", len(s.bodies))
				}
			})
		}
	})

	t.Run("応答の前に接続が切れたら、できていないことを確かめて送り直す", func(t *testing.T) {
		// Arrange
		hangUp := func(w http.ResponseWriter, _ *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}
		s := &uploadServer{uploads: []func(http.ResponseWriter, *http.Request){hangUp, uploadCreated}}
		c, _ := newTestDrive(t, s.handle)
		// Act
		e, err := c.Upload(context.Background(), "P1", "a.txt", "text/plain", strings.NewReader(content), 5, nil)
		// Assert
		if err != nil || e.ID != "F1" {
			t.Fatalf("e = %+v, err = %v", e, err)
		}
		if len(s.bodies) != 2 || s.lists != 1 {
			t.Errorf("uploads = %d, lists = %d", len(s.bodies), s.lists)
		}
	})

	t.Run("先頭に戻せない本文なら、やり直さない", func(t *testing.T) {
		// Arrange
		s := &uploadServer{uploads: []func(http.ResponseWriter, *http.Request){uploadStatus(http.StatusServiceUnavailable), uploadCreated}}
		c, slept := newTestDrive(t, s.handle)
		// Act
		_, err := c.Upload(context.Background(), "P1", "a.txt", "text/plain", onlySeq{strings.NewReader(content)}, 5, nil)
		// Assert
		if err == nil {
			t.Fatal("err = nil")
		}
		if len(s.bodies) != 1 || s.lists != 0 || len(*slept) != 0 {
			t.Errorf("uploads = %d, lists = %d, slept = %v", len(s.bodies), s.lists, *slept)
		}
	})
}
