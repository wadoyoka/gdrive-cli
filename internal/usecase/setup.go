package usecase

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
)

const (
	enableDriveAttempts = 5
	enableDriveFirstGap = 2 * time.Second
	projectIDPrefix     = "gdrive-cli-"
	suffixAlphabet      = "abcdefghijklmnopqrstuvwxyz0123456789"
	suffixLength        = 6
)

// Setup は、プロジェクトの用意からログインまでを案内しながら進める。
type Setup struct {
	Init         Init
	Policy       PolicyStore
	Auth         Authenticator
	Cloud        CloudSetup
	Login        Login
	Status       Status
	OpenURL      func(url string) error
	Wait         func(ctx context.Context, d time.Duration) error // nil なら ctx を見ながら time.After で待つ
	RandomSuffix func() (string, error)                           // nil なら crypto/rand で小文字英数字 6 文字
}

// SetupInput は、setup の入力。
type SetupInput struct {
	ProjectID        string
	ClientSecretPath string
	NoBrowser        bool
	Ask              SetupPrompter
	Prompt           io.Writer // Login に渡す
	Input            io.Reader // Login に渡す。Ask と同じ読み手であること
}

// SetupResult は、setup の結果。
type SetupResult struct {
	ProjectID      string
	GCloudAccount  string
	Email          string
	RootFolderLink string
	KeyStore       string
}

const errSetupAborted = "中止しました"

func setupAborted() error { return domain.Errorf(domain.KindUsage, "%s", errSetupAborted) }

// Run は、設計書 4.2 の手順 1〜12 を順に行う。済んでいる手順は飛ばす。
func (u Setup) Run(ctx context.Context, in SetupInput) (SetupResult, error) {
	say := in.Ask.Say

	// 1. init と同じ処理
	if _, err := u.Init.Run(); err != nil {
		return SetupResult{}, err
	}
	say("[済み] 設定ディレクトリと policy.toml を用意しました")

	// 2. 拒否リスト
	pol, err := u.Policy.Load()
	if err != nil {
		return SetupResult{}, err
	}
	for _, name := range []string{"setup", "login"} {
		if err := pol.Check(name); err != nil {
			return SetupResult{}, err
		}
	}

	// 3. 鍵の置き場所（GCP を変える前に確かめる）
	insp, err := u.Auth.Inspect()
	if err != nil {
		return SetupResult{}, err
	}

	// --project の形は、gcloud を呼ぶ前に確かめる（引数の誤りは終了コード 2）。
	if in.ProjectID != "" {
		if err := domain.ValidateProjectID(in.ProjectID); err != nil {
			return SetupResult{}, err
		}
	}

	// 4. gcloud
	if !u.Cloud.Installed() {
		return SetupResult{}, domain.Errorf(domain.KindSetup, "gcloud が見つかりません。Google Cloud CLI を入れてから、やり直してください: https://cloud.google.com/sdk/docs/install")
	}

	// 5. gcloud のアカウント
	account, err := u.Cloud.ActiveAccount(ctx)
	if err != nil {
		return SetupResult{}, err
	}
	if account == "" {
		return SetupResult{}, domain.Errorf(domain.KindSetup, "`gcloud auth login` を実行してから、やり直してください")
	}
	say("[確認] gcloud のアカウント %s でプロジェクトを作ります", account)

	// 6, 7. プロジェクト
	id, err := u.resolveProject(ctx, in)
	if err != nil {
		return SetupResult{}, err
	}

	// 8. Drive API
	if err := u.enableDrive(ctx, id); err != nil {
		return SetupResult{}, err
	}
	say("[済み] Drive API を有効にしました")

	// 9, 10. クライアントシークレット
	imported := false
	if in.ClientSecretPath != "" || !insp.ClientSecret {
		if in.ClientSecretPath == "" {
			u.guide(id, in)
		}
		if err := u.importClientSecret(id, in); err != nil {
			return SetupResult{}, err
		}
		imported = true
		say("[済み] クライアントシークレットを取り込みました。JSON は安全な場所に保管してください")
	} else {
		say("[省略] クライアントシークレットは取り込み済みです")
	}

	// 11, 12. ログイン
	res := SetupResult{ProjectID: id, GCloudAccount: account, KeyStore: insp.KeyStore}
	if !imported && insp.Token {
		say("[省略] ログイン済みです")
		st, err := u.Status.Run(ctx)
		if err != nil {
			return SetupResult{}, err
		}
		res.Email, res.RootFolderLink = st.Email, st.RootFolderLink
		return res, nil
	}
	lr, err := u.Login.Run(ctx, LoginInput{NoBrowser: in.NoBrowser, Prompt: in.Prompt, Input: in.Input})
	if err != nil {
		return SetupResult{}, err
	}
	say("[済み] %s でログインしました", lr.Email)
	res.Email, res.RootFolderLink = lr.Email, lr.RootFolderLink
	return res, nil
}

// resolveProject は、手順 6 と 7。使うプロジェクトの ID を返す。
func (u Setup) resolveProject(ctx context.Context, in SetupInput) (string, error) {
	say := in.Ask.Say
	interactive := in.ProjectID == ""
	id := in.ProjectID
	if !interactive {
		if err := domain.ValidateProjectID(id); err != nil {
			return "", err
		}
	}
	for {
		if interactive {
			var err error
			if id, err = u.askProjectID(in.Ask); err != nil {
				return "", err
			}
		}
		p, found, err := u.Cloud.DescribeProject(ctx, id)
		if err != nil {
			return "", err
		}
		if found {
			switch p.State {
			case "ACTIVE":
				say("[済み] プロジェクト %s は既にあります", id)
				return id, nil
			case "DELETE_REQUESTED":
				return "", domain.Errorf(domain.KindSetup, "プロジェクト %s は削除予定です。`gcloud projects undelete %s` で戻してから、やり直してください", id, id)
			default:
				return "", domain.Errorf(domain.KindSetup, "プロジェクト %s の状態が %s です", id, p.State)
			}
		}
		err = u.Cloud.CreateProject(ctx, id)
		if err == nil {
			say("[作成] プロジェクト %s を作りました", id)
			return id, nil
		}
		if domain.KindOf(err) == domain.KindConflict && interactive {
			say("プロジェクト ID %s は他の人に使われています。別の ID にしてください", id)
			continue
		}
		return "", err
	}
}

// askProjectID は、形の合う ID が得られるまで聞く。
func (u Setup) askProjectID(ask SetupPrompter) (string, error) {
	suffix, err := u.randomSuffix()
	if err != nil {
		return "", err
	}
	def := projectIDPrefix + suffix
	for {
		ans, err := ask.Ask("プロジェクト ID", def)
		if errors.Is(err, io.EOF) {
			return "", setupAborted()
		}
		if err != nil {
			return "", err
		}
		ans = strings.TrimSpace(ans)
		if ans == "" {
			ans = def
		}
		if err := domain.ValidateProjectID(ans); err != nil {
			ask.Say("%s", err.Error())
			continue
		}
		return ans, nil
	}
}

func (u Setup) randomSuffix() (string, error) {
	if u.RandomSuffix != nil {
		return u.RandomSuffix()
	}
	b := make([]byte, suffixLength)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(suffixAlphabet))))
		if err != nil {
			return "", domain.Errorf(domain.KindUnexpected, "乱数を作れません").Wrap(err)
		}
		b[i] = suffixAlphabet[n.Int64()]
	}
	return string(b), nil
}

// enableDrive は、手順 8。作った直後は失敗しうるので、2、4、8、16 秒あけて最大 5 回試す。
func (u Setup) enableDrive(ctx context.Context, id string) error {
	gap := enableDriveFirstGap
	for attempt := 1; ; attempt++ {
		err := u.Cloud.EnableDrive(ctx, id)
		if err == nil {
			return nil
		}
		if attempt == enableDriveAttempts {
			return err
		}
		if err := u.wait(ctx, gap); err != nil {
			return err
		}
		gap *= 2
	}
}

func (u Setup) wait(ctx context.Context, d time.Duration) error {
	if u.Wait != nil {
		return u.Wait(ctx, d)
	}
	return waitCtx(ctx, d)
}

func waitCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// guide は、手順 9。Google Auth Platform での手作業を案内し、概要の画面を開く。
func (u Setup) guide(id string, in SetupInput) {
	say := in.Ask.Say
	say("次の4つを、ブラウザで行ってください（クライアントの JSON は、作成直後の画面でしかダウンロードできません）")
	say("  1. ブランディング %s : アプリ名とサポート用メールアドレスを設定する", domain.AuthConsoleURL("branding", id))
	say("  2. 対象 %s : ユーザーの種類を「外部」、公開ステータスを「本番」にする（「テスト」だとリフレッシュトークンが 7 日で切れます）", domain.AuthConsoleURL("audience", id))
	say("  3. データアクセス %s : https://www.googleapis.com/auth/drive.file を追加する", domain.AuthConsoleURL("scopes", id))
	say("  4. クライアント %s : 種類「デスクトップアプリ」のクライアントを作り、作成直後の画面で JSON をダウンロードする。この画面を閉じるとシークレットは二度と表示されません。JSON はパスワードマネージャーなど安全な場所に保管してください", domain.AuthConsoleURL("clients", id))
	if in.NoBrowser || u.OpenURL == nil {
		return
	}
	if err := u.OpenURL(domain.AuthConsoleURL("overview", id)); err != nil {
		say("ブラウザを開けませんでした。上の URL を開いてください")
	}
}

// importClientSecret は、手順 10。JSON のパスを決めて取り込む。
func (u Setup) importClientSecret(id string, in SetupInput) error {
	say := in.Ask.Say
	explicit := in.ClientSecretPath != ""
	for {
		path := in.ClientSecretPath
		if !explicit {
			ans, err := in.Ask.Ask("ダウンロードした JSON のパス（ファイルをここへドラッグしてもよい）", "")
			if errors.Is(err, io.EOF) {
				return setupAborted()
			}
			if err != nil {
				return err
			}
			if path, err = normalizeInputPath(ans); err != nil {
				say("%s", err.Error())
				continue
			}
			if path == "" {
				return setupAborted()
			}
		}
		pid, err := u.Auth.ClientSecretProjectID(path)
		if err != nil {
			if domain.KindOf(err) != domain.KindUsage || explicit {
				return err
			}
			say("%s", err.Error())
			continue
		}
		if pid != "" && pid != id {
			say("警告: この JSON のプロジェクトは %s で、setup のプロジェクト %s と違います", pid, id)
			ok, err := in.Ask.Confirm("このまま使いますか")
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			if err != nil || !ok {
				if explicit {
					return setupAborted()
				}
				continue
			}
		}
		return u.Auth.ImportClientSecret(path)
	}
}

// normalizeInputPath は、人が打った（またはドラッグした）パスを整える。
// 前後の空白と両端の ' か " を1組取り除き、`\ ` を空白に戻し、先頭の `~/` をホームに展開する。
func normalizeInputPath(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	s = strings.ReplaceAll(s, `\ `, " ")
	if rest, ok := strings.CutPrefix(s, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", domain.Errorf(domain.KindUsage, "ホームディレクトリを調べられません: %s", s).Wrap(err)
		}
		s = filepath.Join(home, rest)
	}
	return s, nil
}
