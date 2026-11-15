# 開発

```bash
mise install        # ツールを入れる。lefthook のフックも入る
make build          # リポジトリの直下に ./gdrive-cli を作る。macOS では hardened runtime を付ける
make install        # ~/.local/bin に入れる（BINDIR で変えられる）。macOS では hardened runtime を付ける
make lint           # golangci-lint run ./...
make fmt            # golangci-lint fmt
make test           # go test ./...
```

- macOS では、`make build` で作る（`go build` のままでは hardened runtime が付かず、キーチェーンを使えない）。ソースを変えてビルドし直すと、最初の実行でダイアログが出る（同じソースから作り直しただけなら、中身が同じなので出ない）。`make install` は、`~/.local/bin` に入れるときに使う。
- Windows では、`make build` と `make install` は `go build` だけで動く（hardened runtime は macOS だけ）。`make install` は `%USERPROFILE%\.local\bin` に入れる。
- macOS から Windows 向けのコンパイルを確かめるには、`GOOS=windows go build ./...` と `GOOS=windows go vet ./...` を実行する。Windows の上でのテストは、GitHub の Actions の画面で ci を「Run workflow」で起動する（または `gh workflow run ci.yml --ref <ブランチ>`）。手で起動したときだけ動く。
- ツールの版は `.mise.toml` に固定してある（go、golangci-lint、trivy、lefthook、node、pnpm）。CI も同じ版を使う。
- コミット時は lefthook が、gofmt、golangci-lint、go test、trivy（secret と vuln）、commitlint を走らせる。
- コミットメッセージの形式は `type: :emoji: 件名`（件名は日本語）。type と絵文字の対応は `commitlint.config.mjs` に従う。`--no-verify` は使わない。
- 層は cli、usecase、domain、repository の4つ。依存の向きは `internal/archtest` と golangci-lint の depguard が確かめる。削除と共有の API を本体のコードから呼べないことも、archtest が機械的に確かめている。
- タスクの管理には bd（beads）をステルスモードで使う（`bd ready`、`bd create`、`bd update <id> --status in_progress`、`bd close <id>`）。`.beads/` は git に入らない。
- 試し用のファイルは `rm` ではなく、`trash/` へ `mv` で片付ける。
- CI は commitlint、golangci-lint、`go test`、trivy を走らせる。毎日の trivy、Renovate による依存の更新、semantic-release によるリリースは [ci.md](ci.md) にある。
- main に入ると、コミットの type で次の版が決まる（feat はマイナー、fix・perf・revert はパッチ、ほかはリリースしない）。
- AI エージェント向けのスキルは `agents/` にある（[agents/README.md](../agents/README.md)）。

## E2E テスト

本物のドライブを使う確かめは 2 つある。

| 確かめ | 動かすもの | 確かめる範囲 |
|---|---|---|
| CLI の E2E スクリプト（`tests/scripts/e2e-cli.sh`） | ログイン済みの gdrive-cli | `status`・`mkdir`・`ls`・`upload`・`download`・`mv` の終了コード、`--json` の形、ドライブと手元の結果 |
| Go の E2E テスト（`TestE2E`） | usecase と repository を直に呼ぶ | 分割送信、`ls`・`mv`・`download` の結果、衝突（CLI の層は通らない） |

### CLI の E2E スクリプト

普段使っている gdrive-cli（ログイン済み）で、そのまま動かす。スクリプトはログインしない。

```bash
tests/scripts/e2e-cli.sh                              # PATH の gdrive-cli（make install で入れたもの）
GDRIVE_CLI_BIN=./gdrive-cli tests/scripts/e2e-cli.sh  # make build で作ったもの
```

- ドライブには、ルートフォルダの下に作業用フォルダ `/e2e-cli-<日時>-<PID>` を作り、その中だけを使う。**gdrive-cli には削除がないので、終わったら、最後に表示される作業用フォルダをブラウザで開いてゴミ箱に入れる。** 失敗や中断のときも表示される。
- 手元には一時ディレクトリを作り、終わったら（失敗や中断のときも）消す。
- 設定ディレクトリ（`policy.toml`・状態）には触れない。`GDRIVE_CLI_CONFIG_DIR` と `GDRIVE_CLI_KEYRING` は、そのまま gdrive-cli に渡る。
- 20 MiB のファイルを送受信するので、少し時間がかかる。
- 1 件ごとに ✓ か ✗ を出し、最後に件数を出す。✗ には、直前のコマンド、終了コード、標準出力と標準エラーの頭が付く。
- 終了コードは、0 がすべて通った、1 が通らなかったものがある、2 が前提が足りない（gdrive-cli・jq・`sha256sum` か `shasum` がない、ログインしていない）、130 が中断。ログインしていないときは、何も作らずに止まる。
- 確かめないもの: `init`・`setup`・`login`・`logout`、拒否リスト（`policy.toml` を書き換えることになるため）、終了コード 6（CLI では同じ名前を作れないため）。
- 要るものは bash（3.2 以上）と jq。Git Bash（Windows）でも動く（ドライブ側のパスの頭は自動で `//` にする）。Git Bash でシンボリックリンクを作れないときは、シンボリックリンクの確かめだけを飛ばす。

### Go の E2E テスト

実際の Drive を使う Go の E2E テストは、既定では飛ばされる。先に `init` と `login` を済ませてから、次を実行する。ルートフォルダの下に `/e2e-<時刻>` を作り、終わると消す。

```bash
GDRIVE_CLI_E2E=1 go test ./internal/repository/driveapi/ -run TestE2E -v
```

macOS では、`go test` の実行ファイルに hardened runtime が付かないので、E2E テストは OS のキーチェーンを使えない。Windows では、本物の資格情報マネージャーに項目を作らないために、同じく `GDRIVE_CLI_KEYRING=file` を求める（PowerShell なら `$env:GDRIVE_CLI_KEYRING = "file"`）。普段の設定ディレクトリとは別のディレクトリで、`GDRIVE_CLI_KEYRING=file` を指定してログインしてから動かす。

```bash
export GDRIVE_CLI_CONFIG_DIR="$HOME/.config/gdrive-cli-e2e" GDRIVE_CLI_KEYRING=file
gdrive-cli init && gdrive-cli login --client-secret <JSON>
GDRIVE_CLI_E2E=1 go test ./internal/repository/driveapi/ -run TestE2E -v
```

普段の設定ディレクトリを `GDRIVE_CLI_KEYRING=file` で使うと、`key` が別にできて、普段の鍵と食い違う。そのため、別のディレクトリにする。
