package repository

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

const (
	entryFields  = "id, name, mimeType, size, createdTime, modifiedTime, webViewLink, trashed"
	listFields   = "nextPageToken, files(" + entryFields + ")"
	rootKey      = "gdriveCliRoot"
	rootValue    = "1"
	uploadChunk  = 16 << 20
	chunkRetry   = 2 * time.Minute
	listPageSize = 1000
	maxAttempts  = 5
)

// DriveClient は、drive/v3 で usecase.Drive を実装する。
type DriveClient struct {
	svc      *drive.Service
	sleep    func(time.Duration)
	attempts int
}

var _ usecase.Drive = (*DriveClient)(nil)

// NewDriveClient は、svc を使う DriveClient を返す。
func NewDriveClient(svc *drive.Service) *DriveClient {
	return &DriveClient{svc: svc, sleep: time.Sleep, attempts: maxAttempts}
}

// NewDriveOpener は、保存済みのトークンで Drive を開く関数を返す。
func NewDriveOpener(auth *Auth) usecase.DriveOpener {
	return func(ctx context.Context) (usecase.Drive, error) {
		ts, err := auth.TokenSource(ctx)
		if err != nil {
			return nil, err
		}
		svc, err := drive.NewService(ctx, option.WithTokenSource(ts))
		if err != nil {
			return nil, domain.Errorf(domain.KindUnexpected, "Drive に接続できません").Wrap(err)
		}
		return NewDriveClient(svc), nil
	}
}

// escapeQuery は、q の文字列リテラルに入れられるように \ と ' を退避する。
func escapeQuery(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}

// mapError は、API のエラーを domain.Error に変える。
func mapError(err error) error {
	if err == nil {
		return nil
	}
	var de *domain.Error
	if errors.As(err, &de) {
		return err
	}
	var rerr *oauth2.RetrieveError
	if errors.As(err, &rerr) {
		return domain.Errorf(domain.KindSetup, "ログインが切れています。`gdrive-cli login` を実行してください").Wrap(err)
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) && gerr.Code == http.StatusNotFound {
		return domain.Errorf(domain.KindNotFound, "Drive に見つかりません").Wrap(err)
	}
	return domain.Errorf(domain.KindUnexpected, "Drive の操作に失敗しました").Wrap(err)
}

func toEntry(f *drive.File) domain.Entry {
	e := domain.Entry{
		ID:          f.Id,
		Name:        f.Name,
		Type:        domain.TypeFile,
		Size:        f.Size,
		MimeType:    f.MimeType,
		WebViewLink: f.WebViewLink,
		InTrash:     f.Trashed,
	}
	if f.MimeType == domain.FolderMimeType {
		e.Type = domain.TypeFolder
	}
	e.ModifiedTime, _ = time.Parse(time.RFC3339, f.ModifiedTime)
	e.CreatedTime, _ = time.Parse(time.RFC3339, f.CreatedTime)
	return e
}

func toEntries(files []*drive.File) []domain.Entry {
	out := make([]domain.Entry, 0, len(files))
	for _, f := range files {
		out = append(out, toEntry(f))
	}
	return out
}

// do は、fn をやり直しつきで呼び、エラーを変換する。
func (c *DriveClient) do(ctx context.Context, fn func() error) error {
	return mapError(c.retry(ctx, fn))
}

// AccountEmail は、ログイン中のアカウントのメールアドレスを返す。
func (c *DriveClient) AccountEmail(ctx context.Context) (string, error) {
	var about *drive.About
	err := c.do(ctx, func() (err error) {
		about, err = c.svc.About.Get().Fields("user(emailAddress)").Context(ctx).Do()
		return err
	})
	if err != nil {
		return "", err
	}
	if about.User == nil {
		return "", nil
	}
	return about.User.EmailAddress, nil
}

func (c *DriveClient) list(ctx context.Context, q, orderBy string, pageSize int64) ([]domain.Entry, error) {
	var out []domain.Entry
	token := ""
	for {
		var res *drive.FileList
		err := c.do(ctx, func() (err error) {
			call := c.svc.Files.List().Q(q).Fields(listFields).Context(ctx)
			if orderBy != "" {
				call = call.OrderBy(orderBy)
			}
			if pageSize > 0 {
				call = call.PageSize(pageSize)
			}
			if token != "" {
				call = call.PageToken(token)
			}
			res, err = call.Do()
			return err
		})
		if err != nil {
			return nil, err
		}
		out = append(out, toEntries(res.Files)...)
		if res.NextPageToken == "" {
			return out, nil
		}
		token = res.NextPageToken
	}
}

// FindRootFolders は、印の付いたルートフォルダを、ゴミ箱のものも含めて作成日時の古い順に返す。
func (c *DriveClient) FindRootFolders(ctx context.Context) ([]domain.Entry, error) {
	q := "appProperties has { key='" + rootKey + "' and value='" + rootValue + "' } and mimeType='" + domain.FolderMimeType + "'"
	return c.list(ctx, q, "createdTime", 0)
}

func (c *DriveClient) create(ctx context.Context, f *drive.File) (domain.Entry, error) {
	var res *drive.File
	err := c.do(ctx, func() (err error) {
		res, err = c.svc.Files.Create(f).Fields(entryFields).Context(ctx).Do()
		return err
	})
	if err != nil {
		return domain.Entry{}, err
	}
	return toEntry(res), nil
}

// CreateRootFolder は、印を付けたフォルダをマイドライブの直下に作る。
func (c *DriveClient) CreateRootFolder(ctx context.Context, name string) (domain.Entry, error) {
	return c.create(ctx, &drive.File{
		Name:          name,
		MimeType:      domain.FolderMimeType,
		Parents:       []string{"root"},
		AppProperties: map[string]string{rootKey: rootValue},
	})
}

// Get は、id の項目を返す。ないときは KindNotFound。
func (c *DriveClient) Get(ctx context.Context, id string) (domain.Entry, error) {
	var res *drive.File
	err := c.do(ctx, func() (err error) {
		res, err = c.svc.Files.Get(id).Fields(entryFields).Context(ctx).Do()
		return err
	})
	if err != nil {
		return domain.Entry{}, err
	}
	return toEntry(res), nil
}

// ListChildren は、parentID の直下（ゴミ箱を除く）を全ページ返す。
func (c *DriveClient) ListChildren(ctx context.Context, parentID string) ([]domain.Entry, error) {
	return c.list(ctx, "'"+escapeQuery(parentID)+"' in parents and trashed=false", "folder,name", listPageSize)
}

// FindChildren は、parentID の直下から、名前が一致するもの（ゴミ箱を除く）を返す。
func (c *DriveClient) FindChildren(ctx context.Context, parentID, name string) ([]domain.Entry, error) {
	q := "'" + escapeQuery(parentID) + "' in parents and name='" + escapeQuery(name) + "' and trashed=false"
	return c.list(ctx, q, "", 0)
}

// CreateFolder は、parentID の下にフォルダを作る。
func (c *DriveClient) CreateFolder(ctx context.Context, parentID, name string) (domain.Entry, error) {
	return c.create(ctx, &drive.File{Name: name, MimeType: domain.FolderMimeType, Parents: []string{parentID}})
}

// Upload は、parentID の下に新しいファイルを作る。分割送信のため、やり直しはライブラリに任せる。
func (c *DriveClient) Upload(ctx context.Context, parentID, name, mimeType string, body io.Reader, _ int64, progress func(sent int64)) (domain.Entry, error) {
	call := c.svc.Files.Create(&drive.File{Name: name, Parents: []string{parentID}}).
		Media(body, googleapi.ContentType(mimeType), googleapi.ChunkSize(uploadChunk), googleapi.ChunkRetryDeadline(chunkRetry)).
		Fields(entryFields).Context(ctx)
	if progress != nil {
		call = call.ProgressUpdater(func(current, _ int64) { progress(current) })
	}
	res, err := call.Do()
	if err != nil {
		return domain.Entry{}, mapError(err)
	}
	return toEntry(res), nil
}

type countingWriter struct {
	w        io.Writer
	n        int64
	progress func(int64)
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	if n > 0 && cw.progress != nil {
		cw.progress(cw.n)
	}
	return n, err
}

// Download は、id の中身を w に書く。まだ1バイトも書いていない失敗のときだけやり直す。
func (c *DriveClient) Download(ctx context.Context, id string, w io.Writer, progress func(received int64)) error {
	cw := &countingWriter{w: w, progress: progress}
	for attempt := 1; ; attempt++ {
		err := c.downloadOnce(ctx, id, cw)
		if err == nil {
			return nil
		}
		if cw.n > 0 || !isRetryable(err) || attempt >= c.attempts || ctx.Err() != nil {
			return mapError(err)
		}
		c.sleep(time.Second << (attempt - 1))
	}
}

func (c *DriveClient) downloadOnce(ctx context.Context, id string, cw *countingWriter) error {
	resp, err := c.svc.Files.Get(id).Context(ctx).Download()
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.Copy(cw, resp.Body)
	return err
}

// Move は、名前を newName にし、親が変わるときだけ付け替える。
func (c *DriveClient) Move(ctx context.Context, id, fromParentID, toParentID, newName string) (domain.Entry, error) {
	var res *drive.File
	err := c.do(ctx, func() (err error) {
		call := c.svc.Files.Update(id, &drive.File{Name: newName}).Fields(entryFields).Context(ctx)
		if fromParentID != toParentID {
			call = call.AddParents(toParentID).RemoveParents(fromParentID)
		}
		res, err = call.Do()
		return err
	})
	if err != nil {
		return domain.Entry{}, err
	}
	return toEntry(res), nil
}
