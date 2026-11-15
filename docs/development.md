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
- CI は commitlint、golangci-lint、`go test`、trivy を走らせる。
- AI エージェント向けのスキルは `agents/` にある（[agents/README.md](../agents/README.md)）。

## E2E テスト

実際の Drive を使う E2E テストは、既定では飛ばされる。先に `init` と `login` を済ませてから、次を実行する。ルートフォルダの下に `/e2e-<時刻>` を作り、終わると消す。

```bash
GDRIVE_CLI_E2E=1 go test ./internal/repository/driveapi/ -run TestE2E -v
```

macOS では、`go test` の実行ファイルに hardened runtime が付かないので、E2E テストは OS のキーチェーンを使えない。普段の設定ディレクトリとは別のディレクトリで、`GDRIVE_CLI_KEYRING=file` を指定してログインしてから動かす。

```bash
export GDRIVE_CLI_CONFIG_DIR="$HOME/.config/gdrive-cli-e2e" GDRIVE_CLI_KEYRING=file
gdrive-cli init && gdrive-cli login --client-secret <JSON>
GDRIVE_CLI_E2E=1 go test ./internal/repository/driveapi/ -run TestE2E -v
```

普段の設定ディレクトリを `GDRIVE_CLI_KEYRING=file` で使うと、`key` が別にできて、普段の鍵と食い違う。そのため、別のディレクトリにする。
