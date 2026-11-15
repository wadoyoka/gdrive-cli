// Package driveapi は、Google Drive API の呼び出し（usecase.Drive の実装）と、失敗したときのやり直しを持つ。
package driveapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/repository/auth"
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

// Client は、drive/v3 で usecase.Drive を実装する。
type Client struct {
	svc      *drive.Service
	sleep    func(time.Duration)
	attempts int
}

var _ usecase.Drive = (*Client)(nil)

// NewClient は、svc を使う Client を返す。
func NewClient(svc *drive.Service) *Client {
	return &Client{svc: svc, sleep: time.Sleep, attempts: maxAttempts}
}

// NewOpener は、保存済みのトークンで Drive を開く関数を返す。
func NewOpener(a *auth.Auth) usecase.DriveOpener {
	return func(ctx context.Context) (usecase.Drive, error) {
		ts, err := a.TokenSource(ctx)
		if err != nil {
			return nil, err
		}
		svc, err := drive.NewService(ctx, option.WithTokenSource(ts))
		if err != nil {
			return nil, domain.Errorf(domain.KindUnexpected, "Drive に接続できません").Wrap(err)
		}
		return NewClient(svc), nil
	}
}

// escapeQuery は、q の文字列リテラルに入れられるように \ と ' を退避する。
func escapeQuery(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}

// mapError は、API のエラーを domain.Error に変える。エラーの文字列は利用者に見せるので、URL の問い合わせ部分は取り除く。
func mapError(err error) error {
	if err == nil {
		return nil
	}
	redactURL(err)
	if _, ok := errors.AsType[*domain.Error](err); ok {
		return err
	}
	if _, ok := errors.AsType[*oauth2.RetrieveError](err); ok {
		return domain.Errorf(domain.KindSetup, "ログインが切れています。`gdrive-cli login` を実行してください").Wrap(err)
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) && gerr.Code == http.StatusNotFound {
		return domain.Errorf(domain.KindNotFound, "Drive に見つかりません").Wrap(err)
	}
	return domain.Errorf(domain.KindUnexpected, "Drive の操作に失敗しました").Wrap(err)
}

// redactURL は、err の連なりにある url.Error の URL から、問い合わせ部分を取り除く。
// 再開可能アップロードの upload_id のように、他人に知られると困る値が入ることがあるため。
func redactURL(err error) {
	if uerr, ok := errors.AsType[*url.Error](err); ok {
		if i := strings.IndexByte(uerr.URL, '?'); i >= 0 {
			uerr.URL = uerr.URL[:i]
		}
	}
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
func (c *Client) do(ctx context.Context, fn func() error) error {
	return mapError(c.retry(ctx, fn))
}

// AccountEmail は、ログイン中のアカウントのメールアドレスを返す。
func (c *Client) AccountEmail(ctx context.Context) (string, error) {
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

func (c *Client) list(ctx context.Context, q, orderBy string, pageSize int64) ([]domain.Entry, error) {
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
func (c *Client) FindRootFolders(ctx context.Context) ([]domain.Entry, error) {
	q := "appProperties has { key='" + rootKey + "' and value='" + rootValue + "' } and mimeType='" + domain.FolderMimeType + "'"
	return c.list(ctx, q, "createdTime", 0)
}

func (c *Client) create(ctx context.Context, f *drive.File) (domain.Entry, error) {
	// 作成は冪等ではなく、やり直すと同じ名前のフォルダが二重にできるため、やり直さない。
	res, err := c.svc.Files.Create(f).Fields(entryFields).Context(ctx).Do()
	if err != nil {
		return domain.Entry{}, mapError(err)
	}
	return toEntry(res), nil
}

// CreateRootFolder は、印を付けたフォルダをマイドライブの直下に作る。
func (c *Client) CreateRootFolder(ctx context.Context, name string) (domain.Entry, error) {
	return c.create(ctx, &drive.File{
		Name:          name,
		MimeType:      domain.FolderMimeType,
		Parents:       []string{"root"},
		AppProperties: map[string]string{rootKey: rootValue},
	})
}

// Get は、id の項目を返す。ないときは KindNotFound。
func (c *Client) Get(ctx context.Context, id string) (domain.Entry, error) {
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
func (c *Client) ListChildren(ctx context.Context, parentID string) ([]domain.Entry, error) {
	return c.list(ctx, "'"+escapeQuery(parentID)+"' in parents and trashed=false", "folder,name", listPageSize)
}

// FindChildren は、parentID の直下から、名前が一致するもの（ゴミ箱を除く）を返す。
func (c *Client) FindChildren(ctx context.Context, parentID, name string) ([]domain.Entry, error) {
	q := "'" + escapeQuery(parentID) + "' in parents and name='" + escapeQuery(name) + "' and trashed=false"
	return c.list(ctx, q, "", 0)
}

// CreateFolder は、parentID の下にフォルダを作る。
func (c *Client) CreateFolder(ctx context.Context, parentID, name string) (domain.Entry, error) {
	return c.create(ctx, &drive.File{Name: name, MimeType: domain.FolderMimeType, Parents: []string{parentID}})
}

// Upload は、parentID の下に新しいファイルを作る。16 MiB を超えるものは分けて送り、分けた 1 つずつのやり直しはライブラリが行う。
// body が io.Seeker なら、一時的な失敗のとき、先頭に戻して最初から送り直す（最大 attempts 回）。
//   - 混み合いで断られた（429 など）なら、何も作られていないので、そのまま送り直す。
//   - 5xx や通信の失敗なら、ファイルが作られたかもしれないので、送り直す前に同じ名前のファイルを探す。
//     大きさが size のファイルが 1 つだけあれば、それを結果にする（usecase の計画で、同じ名前のファイルが
//     なかったことを確かめてあるため）。なければ送り直す。それ以外は、重ねて作らないよう、送り直さずにエラーにする。
//
// body が io.Seeker でなければ、送り直さない。
func (c *Client) Upload(ctx context.Context, parentID, name, mimeType string, body io.Reader, size int64, progress func(sent int64)) (domain.Entry, error) {
	seeker, ok := body.(io.Seeker)
	if !ok {
		e, err := c.uploadOnce(ctx, parentID, name, mimeType, body, progress)
		return e, mapError(err)
	}
	for attempt := 1; ; attempt++ {
		e, err := c.uploadOnce(ctx, parentID, name, mimeType, body, progress)
		if err == nil {
			return e, nil
		}
		if !isRetryable(err) || attempt >= c.attempts || ctx.Err() != nil {
			return domain.Entry{}, mapError(err)
		}
		c.sleep(backoff(attempt))
		if !isRateLimited(err) {
			found, settled, ferr := c.findUploaded(ctx, parentID, name, size, err)
			if settled {
				return found, mapError(ferr)
			}
		}
		if _, serr := seeker.Seek(0, io.SeekStart); serr != nil {
			return domain.Entry{}, domain.Errorf(domain.KindUnexpected, "%s を最初から読み直せません", name).Wrap(serr)
		}
		if progress != nil {
			progress(0)
		}
	}
}

func (c *Client) uploadOnce(ctx context.Context, parentID, name, mimeType string, body io.Reader, progress func(sent int64)) (domain.Entry, error) {
	call := c.svc.Files.Create(&drive.File{Name: name, Parents: []string{parentID}}).
		Media(body, googleapi.ContentType(mimeType), googleapi.ChunkSize(uploadChunk), googleapi.ChunkRetryDeadline(chunkRetry)).
		Fields(entryFields).Context(ctx)
	if progress != nil {
		call = call.ProgressUpdater(func(current, _ int64) { progress(current) })
	}
	res, err := call.Do()
	if err != nil {
		return domain.Entry{}, err
	}
	return toEntry(res), nil
}

// findUploaded は、失敗した送信でファイルが作られていないかを調べる。settled が false なら、何もなく、送り直してよい。
// settled が true なら、送り直さずに entry と err を結果にする。
func (c *Client) findUploaded(ctx context.Context, parentID, name string, size int64, cause error) (entry domain.Entry, settled bool, err error) {
	found, err := c.FindChildren(ctx, parentID, name)
	if err != nil {
		return domain.Entry{}, true, err
	}
	switch {
	case len(found) == 0:
		return domain.Entry{}, false, nil
	case len(found) == 1 && found[0].Type == domain.TypeFile && found[0].Size == size:
		return found[0], true, nil
	}
	list := make([]string, 0, len(found))
	for _, e := range found {
		list = append(list, fmt.Sprintf("ID %s、%d バイト", e.ID, e.Size))
	}
	// usecase は項目ごとに Error() の文字列だけを残すので、見つかったものは補足ではなく本文に入れる。
	return domain.Entry{}, true, domain.Errorf(domain.KindConflict,
		"%s を送る途中で失敗したあと、同じ名前のものが見つかりましたが、送ったものか分かりません（%s）。重ねて作らないよう、送り直していません",
		name, strings.Join(list, "／")).Wrap(cause)
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
func (c *Client) Download(ctx context.Context, id string, w io.Writer, progress func(received int64)) error {
	cw := &countingWriter{w: w, progress: progress}
	for attempt := 1; ; attempt++ {
		err := c.downloadOnce(ctx, id, cw)
		if err == nil {
			return nil
		}
		if cw.n > 0 || !isRetryable(err) || attempt >= c.attempts || ctx.Err() != nil {
			return mapError(err)
		}
		c.sleep(backoff(attempt))
	}
}

func (c *Client) downloadOnce(ctx context.Context, id string, cw *countingWriter) error {
	resp, err := c.svc.Files.Get(id).Context(ctx).Download()
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.Copy(cw, resp.Body)
	return err
}

// Move は、名前を newName にし、親が変わるときだけ付け替える。
func (c *Client) Move(ctx context.Context, id, fromParentID, toParentID, newName string) (domain.Entry, error) {
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
