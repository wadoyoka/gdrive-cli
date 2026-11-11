# CLAUDE.md

Google Drive を操作する Go 製の CLI。設計は `docs/superpowers/specs/`、実装計画は `docs/superpowers/plans/` にある。

## 試し用のファイルの片付け

試し用に作ったファイルやディレクトリを片付けるときは、`rm` を使わず repo 直下の `trash/` へ `mv` する。`rm` は取り消せず、実行の前に確認を求められることも多いため。

- 例: `mv probe-dir trash/`
- `trash/` の中身は git に入らない。`trash/.gitignore` が `.gitignore`・`.gitkeep`・`go.mod` 以外を除外する。golangci-lint や trivy の対象にもならない。
- `trash/go.mod` は git に入っている。`trash/` を別の Go モジュールにして、`./...` や golangci-lint が `trash/` の中の Go のファイルを見ないようにするためのもの。消さない。
- 同じ名前のファイルは上書きしてよい。同じ名前のディレクトリがあると `mv` が失敗するので、名前を変えて入れる。
- `trash/` がないブランチや worktree では、`mkdir -p trash` してから入れる。
- `trash/` を空にするのは人がやる。

## コミットメッセージ

- 形式は `type: :emoji: 件名`（件名は日本語）。type と絵文字の対応は `commitlint.config.mjs` に従う。
- Claude のセッションの URL（`Claude-Session:` の行など）は入れない（commitlint の `no-claude-session-url` が落とす）。
- `--no-verify` は使わない。

## Terraform

- Terraform は Claude が実行しない（利用者の指示）。`init`、`fmt`、`validate`、`plan`、`apply` のどれも実行しない。確かめは利用者が行う。
- `infra/` を含むコミットは、terraform を呼ぶフックだけを外して行う: `LEFTHOOK_EXCLUDE=6-terraform-fmt git commit ...`

## 層の分け方

| 層 | 置き場所 | 役割 | import してよい層 |
|---|---|---|---|
| cli | `internal/cli/` | cobra のコマンド定義、フラグの解釈、出力の整形、終了コードへの変換 | cli、usecase、domain |
| usecase | `internal/usecase/` | 1操作につき1ファイル。`ports.go` に repository が満たすインターフェースを置く | usecase、domain |
| domain | `internal/domain/` | 外部に触れない処理（パスの解釈、計画と衝突の判定、エラーの種類など） | domain |
| repository | `internal/repository/` | Drive API、OAuth、設定ファイル、手元のファイルなど外部とのやりとり | repository、usecase（ports）、domain |

- 入口は `cmd/gdrive-cli/main.go` の1つだけ。各層を組み立てるだけにする。
- 依存の向きは `internal/archtest` と golangci-lint の depguard が確かめる（Google の API は repository だけ、cobra は cli と cmd だけ、`os/exec` は repository だけ）。

## タスクの管理（bd）

bd（beads）をステルスモードで使う。`.beads/` は git に入らない。

- `bd ready`: 着手できるタスクを見る
- `bd create`: タスクを登録する（始める前に登録する）
- `bd update <id> --status in_progress`: 着手する
- `bd close <id>`: 終える

## テストの書き方

- `t.Run` の名前は日本語にする。
- 本文には `// Arrange` `// Act` `// Assert` の区切りを入れる。
- テストの値は、trivy-secret の規則（`GOCSPX-` に28文字が続く、`1//0` に40文字以上が続く）に当たらないものにする。

## 開発コマンド

- `make lint` / `make fmt` / `make test`
- コミット時は lefthook が整形・lint・テスト・trivy・commitlint を走らせる。
