# gdrive-cli

Google ドライブ（マイドライブ）にファイルを上げ、整理するための Go 製の CLI。

## 概要

Claude Code などの AI が作った HTML や動画（数百 MB 以上のものも含む）を、個人の Google ドライブへアップロードして整理するために作った。人がターミナルで使うことも、AI エージェントやスクリプトから呼ぶことも想定している。

できること

- フォルダの作成（`mkdir`）、一覧（`ls`）
- ファイルとフォルダのアップロード（`upload`）とダウンロード（`download`）
- 同じドライブの中での移動と名前変更（`mv`）

作りで守っていること

- **触れる範囲を Google のサーバー側で限定する。** OAuth スコープは `https://www.googleapis.com/auth/drive.file` だけ。トークンが漏れても、この CLI が作ったファイルとフォルダ以外には届かない。
- **できる操作を絞る。** 削除、ゴミ箱への移動、共有設定の変更、同期は持たない。ドライブ上のファイルの上書きもない。
- 操作の対象は、マイドライブ直下の専用ルートフォルダ（既定の名前は `gdrive-cli`）の下だけ。

必要なもの: 個人の Google アカウント（Workspace ではない）、ブラウザ、Go 1.27.3 以上。macOS を主に想定している（Linux、Windows 10・11 でも動く）。macOS では Xcode Command Line Tools も要る。詳しくは [docs/install.md](docs/install.md)。

## gcloud のインストール

`gdrive-cli setup` が、GCP のプロジェクトを作って Drive API を有効にするために gcloud CLI を使う。

macOS（Homebrew）:

```bash
brew install --cask gcloud-cli
```

Windows（PowerShell。[Scoop](https://scoop.sh/) を使う）:

```powershell
scoop bucket add extras
scoop install gcloud
```

Scoop を使わないときは、公式のインストーラー（https://cloud.google.com/sdk/docs/install ）で入れる。

Linux（ほかの入れ方は公式のページ: https://cloud.google.com/sdk/docs/install ）:

```bash
curl https://sdk.cloud.google.com | bash
exec -l $SHELL
```

入れたら、GCP のプロジェクトを作るアカウントでログインしておく。

```bash
gcloud auth login
```

GCP のプロジェクトをコンソールで手作業で用意する場合は、gcloud は要らない（[docs/setup.md](docs/setup.md)）。

## gdrive-cli のインストール

macOS: リポジトリを clone して `make install` を実行する。`~/.local/bin` に入る（`make install BINDIR=/usr/local/bin` のように入れ先を変えられる）。

```bash
git clone https://github.com/wadoyoka/gdrive-cli.git
cd gdrive-cli
make install
```

`~/.local/bin` が PATH に入っていることを確かめる。macOS では `go install` や `go build` で入れたものは OS のキーチェーンを使えないので、必ず `make install` で入れる。

Linux:

```bash
go install github.com/wadoyoka/gdrive-cli/cmd/gdrive-cli@latest
```

Windows: PowerShell で、Scoop で git・go・make を入れてから、`make install` を実行する。`%USERPROFILE%\.local\bin\gdrive-cli.exe` に入る。make は PowerShell、cmd、Git Bash のどこから実行してもよい。

```powershell
scoop install git go make
git clone https://github.com/wadoyoka/gdrive-cli.git
cd gdrive-cli
make install
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$env:USERPROFILE\.local\bin", "User")
```

最後の行は、PATH に `%USERPROFILE%\.local\bin` を足す（1回だけ）。足したら、端末を開き直す。

前の版から入れ替えるときの注意などは [docs/install.md](docs/install.md)。

## プロジェクトのセットアップ

`gdrive-cli setup` が、GCP プロジェクトの用意からログインまでを対話で案内する。済んでいる手順は飛ばすので、途中で止まっても同じコマンドでやり直せる。

```bash
gdrive-cli setup
```

1. プロジェクト ID を聞かれる。Enter だけなら `gdrive-cli-xxxxxx` という ID で作る。続けて Drive API を有効にする。
2. OAuth の設定は公開 API がないので、ブラウザで手作業になる。`setup` が、プロジェクト ID 入りの URL で次の手順を表示し、ブラウザで開く。
   1. 「ブランディング」で、アプリ名とサポート用メールアドレスを設定する。
   2. 「対象（Audience）」で、ユーザーの種類を「外部」にする。公開ステータスは「テスト」のままにし、「テストユーザー」に、この CLI でドライブを使う Google アカウント（自分を含む）を追加する。テストユーザーでないアカウントはログインできない。
   3. 「データアクセス」で `https://www.googleapis.com/auth/drive.file` を追加する。
   4. 「クライアント」で、種類が「デスクトップアプリ」のクライアントを作り、JSON をダウンロードする。
3. ダウンロードした JSON のパスを入れる。暗号化して取り込まれる。
4. ブラウザで Google にログインする（テストユーザーに入れたアカウントで。gcloud のアカウントと別でもよい）。「Google はこのアプリを確認していません」と出たら、「続行」で進む。

**公開ステータスが「テスト」なので、ログインは 7日で切れる。** 切れると、コマンドが終了コード 3 で止まり、`gdrive-cli login` のやり直しを求める。そのときは `gdrive-cli login` を実行する（JSON の指定は要らない）。

**ダウンロードした JSON は、作成直後の画面でしか手に入らない。** パスワードマネージャーなど安全な場所に保管しておく（別の PC で使うときや、テストユーザーに入れたほかの人に使ってもらうときに要る。[docs/setup.md](docs/setup.md)）。リポジトリには入れない。

終わったら、ログインできていることを確かめる。

```bash
gdrive-cli status
```

macOS では、最初の実行でキーチェーンのダイアログが出ることがある。ダイアログに gdrive-cli と書いてあるときだけ「常に許可」を押す（[docs/setup.md](docs/setup.md#鍵の置き場所)）。

`setup` のフラグ、`init` と `login` を個別に使う方法、ブラウザのない環境での使い方は [docs/setup.md](docs/setup.md) と [docs/headless.md](docs/headless.md)。

## ファイルのアップロード

ドライブ側のパスは、ルートフォルダ（マイドライブの `gdrive-cli`）を `/` とする。

```bash
gdrive-cli upload -p ./intro.mp4 /videos/2026-11     # -p で送り先のフォルダがなければ作る
gdrive-cli upload -p ./site ./report.html /pages     # フォルダは中身ごと送る
gdrive-cli ls -l /videos/2026-11                     # 送ったものを確かめる
```

結果に、ドライブ側のパスとブラウザで開くリンクが出る。

- 送り先に同じ名前のファイルがあると、何もアップロードせずに止まる（上書きはできない）。作り直したものは別の名前で送り、古いほうは `gdrive-cli mv` で保管用のフォルダへ移す。
- 途中で失敗したときは、同じコマンドに `--skip-existing` を付けてやり直すと、続きから送る。
- ドライブは HTML をウェブページとして表示しない（リンクを開くとソースが出るか、ダウンロードになる）。

## もっと詳しく

| 内容 | 文書 |
|---|---|
| 各コマンドのフラグと動き、終了コード | [docs/commands.md](docs/commands.md) |
| インストールの詳細 | [docs/install.md](docs/install.md) |
| セットアップの詳細、設定ディレクトリ、鍵の置き場所、すべてを消す方法 | [docs/setup.md](docs/setup.md) |
| 拒否リスト（`policy.toml`） | [docs/policy.md](docs/policy.md) |
| AI エージェントから使うとき | [docs/ai-agents.md](docs/ai-agents.md) |
| ブラウザやキーチェーンのない環境 | [docs/headless.md](docs/headless.md) |
| SSH で Mac に入って、切れたログインを更新する | [docs/faqs/ssh-relogin.md](docs/faqs/ssh-relogin.md) |
| 既知の制約、作らないもの | [docs/limitations.md](docs/limitations.md) |
| 開発 | [docs/development.md](docs/development.md) |
| CI、依存の更新（Renovate）、脆弱性の検査（trivy）、リリース | [docs/ci.md](docs/ci.md) |

## AI エージェント用のスキル

Claude Code と Codex に、gdrive-cli の使い方（ログインの確かめ方、`--json` の読み方、止まるべき場面）を教えるスキルを [agents/](agents/README.md) で配っている。

```bash
./agents/setup.sh
```
