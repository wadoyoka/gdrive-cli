package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wadoyoka/gdrive-cli/internal/domain"
	"github.com/wadoyoka/gdrive-cli/internal/usecase"
)

func newSetupCommand(d Deps, asJSON *bool) *cobra.Command {
	var projectID, secretPath string
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "プロジェクトの用意からログインまでを、対話で行う",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if *asJSON {
				return domain.Errorf(domain.KindUsage, "setup は --json に対応していません")
			}
			if !d.Interactive {
				return domain.Errorf(domain.KindUsage, "setup は人が端末で実行してください（AI エージェントやスクリプトからは実行できません）")
			}
			// 標準入力の読み手は1つだけ。質問役もログインの貼り付けも、これから読む。
			br := bufio.NewReader(d.Stdin)
			res, err := d.Setup.Run(cmd.Context(), usecase.SetupInput{
				ProjectID:        projectID,
				ClientSecretPath: secretPath,
				NoBrowser:        noBrowser,
				Ask:              termPrompter{in: br, out: d.Stderr},
				Prompt:           d.Stderr,
				Input:            br,
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(d.Stdout, "プロジェクト: %s（gcloud のアカウント: %s）\nログイン: %s\nルートフォルダ: %s\n鍵の置き場所: %s\nダウンロードした JSON は、パスワードマネージャーなど安全な場所に保管してください。\n",
				res.ProjectID, res.GCloudAccount, res.Email, res.RootFolderLink, keyStoreLabel(res.KeyStore))
			return err
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "使う GCP プロジェクトの ID（省略すると尋ねる）")
	cmd.Flags().StringVar(&secretPath, "client-secret", "", "OAuth クライアントの JSON のパス（省略すると尋ねる）")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "ブラウザを開かず、URL を表示して認可コードを貼り付ける")
	return cmd
}

// termPrompter は、端末で人に尋ねる SetupPrompter。in は1行ずつ読む。
type termPrompter struct {
	in  *bufio.Reader
	out io.Writer
}

func (p termPrompter) readLine() (string, error) {
	line, err := p.in.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (p termPrompter) Ask(question, def string) (string, error) {
	if def != "" {
		_, _ = fmt.Fprintf(p.out, "%s [%s]: ", question, def)
	} else {
		_, _ = fmt.Fprintf(p.out, "%s: ", question)
	}
	line, err := p.readLine()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(line) == "" {
		return def, nil
	}
	return line, nil
}

func (p termPrompter) Confirm(question string) (bool, error) {
	_, _ = fmt.Fprintf(p.out, "%s [y/N]: ", question)
	line, err := p.readLine()
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

func (p termPrompter) Say(format string, args ...any) {
	_, _ = fmt.Fprintf(p.out, format+"\n", args...)
}
