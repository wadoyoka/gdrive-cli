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
- **できる操作を絞る。** 削除、ゴミ箱への移動、共有設定の変更、同期は持たない。ドライブ上のファイルの上書き（`upload --overwrite`）もない。
- 操作の対象は、マイドライブ直下の専用ルートフォルダ（既定の名前は `gdrive-cli`）の下だけ。

作らないもの（v1）: 削除、共有設定の変更、同期とマウント、Google ドキュメントなどへの変換と書き出し、ブラウザやスマホから入れた既存ファイルへのアクセス、共有ドライブ、Workspace、サービスアカウント、Windows 対応、途中で止まったアップロードの再開、配布用パッケージ。

## 必要なもの

- Go 1.27.3 以上（`go install` や `make install` で入れる場合）
- Google アカウント（個人のもの。Workspace ではない）
- gcloud CLI（`gdrive-cli setup` が GCP のプロジェクトを作るために使う。手作業で設定する場合は不要）
- ブラウザ（`login` のため。ない環境は「ブラウザのない環境」を参照）
- macOS を主に想定している。Linux でも動く。トークンの暗号化の鍵は OS のキーチェーンに置く（キーチェーンのない環境は「トークンの暗号化」を参照）。
- macOS では Xcode Command Line Tools（cgo と `codesign` のため）。

入れ方（macOS）: リポジトリを clone して `make install` を実行する。`~/.local/bin` に入る（`make install BINDIR=/usr/local/bin` のように入れ先を変えられる）。

```bash
git clone https://github.com/wadoyoka/gdrive-cli.git
cd gdrive-cli
make install
```

入れ方（Linux）:

```bash
go install github.com/wadoyoka/gdrive-cli/cmd/gdrive-cli@latest
```

macOS で `go install` や `go build` で入れた gdrive-cli は、hardened runtime が付かないので、OS のキーチェーンを使えない（終了コード 3 で止まる）。その場合は `GDRIVE_CLI_KEYRING=file` を指定するか、`make install` で入れ直す。

## セットアップ

流れは、(1) GCP プロジェクトを作って Drive API を有効にする、(2) OAuth の設定を手作業でする、(3) `init` と `login` を実行する、の3段階。`gdrive-cli setup` を使えば、この3段階を1つのコマンドで案内してもらえる。

### 1. `gdrive-cli setup`

前提は次の2つ。

- gcloud CLI を入れてある（入れ方は Google Cloud CLI のインストールのページ: https://cloud.google.com/sdk/docs/install ）。
- `gcloud auth login` でログインしてある。

```bash
gdrive-cli setup
gdrive-cli setup --project my-gdrive-cli-project
gdrive-cli setup --client-secret ~/Downloads/client_secret_xxx.json
```

| フラグ | 意味 |
|---|---|
| `--project <id>` | 使う（または作る）GCP プロジェクトの ID。省略すると対話で聞く |
| `--client-secret <path>` | ダウンロード済みの JSON のパス。指定すると、手作業の案内と JSON のパスの質問を飛ばす |
| `--no-browser` | ブラウザを開かない。手作業の画面の URL を表示するだけにし、ログインは `login --no-browser` と同じ流れにする |

`setup` は人が端末で実行するコマンド。標準入力と標準エラーが端末でなければ、何もせずに終了コード 2 で止まる。`--json` にも対応しない（終了コード 2）。途中で Ctrl-C を押すと、質問の答えを待たずに「中止しました」と出して終了コード 2 で止まる。

流れ（済んでいる手順は飛ばすので、何度実行しても同じ結果になる）

1. `init` と同じく、設定ディレクトリと `policy.toml` を用意する。
2. 拒否リストに `setup` か `login` があれば、終了コード 7 で止まる。
3. 暗号化の鍵の置き場所が使えるかを、GCP を変える前に確かめる（使えなければ終了コード 3）。
4. gcloud が入っていて、ログイン済みかを確かめる。どちらかが欠けていれば、終了コード 3 で案内を出す。
5. プロジェクト ID を決める。`--project` があれば、それを使う（下の既定値より優先する）。なければ聞き、Enter だけなら既定値を使う。既定値は次の順で決まる。
   1. `--client-secret` があれば、その JSON の `project_id`。JSON は gcloud を呼ぶ前に検める（読めない・形が違えば、gcloud を呼ばずに終了コード 2）。
   2. なければ、取り込み済みのクライアントシークレット（`client_secret.enc`）の `project_id`。
   3. どちらもなければ、`gdrive-cli-` に小文字英数字 6 文字を付けたもの。

   このため、一度済ませたあとで `--project` なしに実行し直しても、Enter だけなら同じプロジェクトを使い、新しいプロジェクトは作らない。
6. プロジェクトを確かめ、なければ作る。表示名は `gdrive-cli`。組織やフォルダには属させず、課金アカウントも紐づけない。ID が他の人に使われていれば、対話なら聞き直し、`--project` なら終了コード 5。削除予定の状態なら、`gcloud projects undelete <id>` で戻す方法を案内して止まる（終了コード 3）。プロジェクトが決まったら、「やり直すときは `gdrive-cli setup --project <id>` を使ってください」と表示する。途中で失敗してやり直すときは、この形で実行すると、同じプロジェクトの続きから進む。
7. Drive API（`drive.googleapis.com`）を有効にする。作った直後は失敗することがあるので、数回やり直す。
8. 手作業（次の「2. 手作業」）の手順を、プロジェクト ID 入りの URL で表示し、ブラウザで開く。クライアントシークレットが取り込み済みで復号できるとき、または `--client-secret` を指定したときは飛ばす。
9. ダウンロードした JSON のパスを聞いて、暗号化して取り込む。デスクトップアプリのクライアントの JSON でなければ（ウェブアプリケーションのクライアントのものなど）、理由を出して聞き直す。JSON の `project_id` が手順 6 のプロジェクトと違えば、警告して続けるかを聞く。
10. ログインする（`login` と同じ処理）。ログイン済みで、JSON を取り込み直していなければ飛ばす。ただし、Google 側でトークンが無効になっている（取り消された、期限が切れたなど）と分かったときは、飛ばさずにログインし直す。
11. プロジェクト、gcloud のアカウント、ログインした Google アカウント、ルートフォルダのリンク、鍵の置き場所を表示する。

`setup` が GCP に加える変更は、プロジェクトの作成と Drive API の有効化だけ。gcloud は、`auth list`、`projects describe`、`projects create`、`services enable drive.googleapis.com` の4つしか呼ばない（毎回 `--quiet` を付け、標準入力はつながない）。

- gcloud のアカウント（プロジェクトの持ち主）と、ログインする Google アカウント（ドライブの持ち主）は、別でもよい。
- 2台目の PC では、`setup` を使わずに、`init` と `login --client-secret <保管しておいた JSON>` だけでもよい。`setup` を使っても、済んでいる手順は飛ばされる。`setup --client-secret <保管しておいた JSON>` なら、JSON の `project_id` が ID の既定値になるので、Enter だけで同じプロジェクトを使う（gcloud のアカウントからそのプロジェクトが見えれば、新しく作らない）。

### 2. 手作業（OAuth 同意画面とクライアント）

OAuth 同意画面と OAuth クライアントを作る公開 API がないため、ここだけは手作業になる（Google の IAP OAuth Admin API は 2025年1月に非推奨になっている）。`setup` は同じ内容を表示する。`setup` を使わない人は、Google Cloud コンソールの Google Auth Platform を開き、自分のプロジェクトを選んで、次を行う。

1. 「ブランディング」（`https://console.cloud.google.com/auth/branding?project=<プロジェクト ID>`）で、アプリ名とサポート用メールアドレスを設定する。
2. 「対象（Audience）」（`.../auth/audience?project=<プロジェクト ID>`）で、ユーザーの種類を「外部（External）」にする。そのうえで公開ステータスを「本番（In production）」にする。
   - 「テスト」のままだとリフレッシュトークンが 7日で切れるため。
   - `drive.file` は影響が小さいスコープなので、本番にしても審査は要らず、「未確認のアプリ」警告も出ない想定。
3. 「データアクセス」（`.../auth/scopes?project=<プロジェクト ID>`）で `https://www.googleapis.com/auth/drive.file` を追加する。
4. 「クライアント」（`.../auth/clients?project=<プロジェクト ID>`）で、種類が「デスクトップアプリ」のクライアントを作り、JSON をダウンロードする。
5. `gdrive-cli init` を実行し、続けて `gdrive-cli login --client-secret <ダウンロードした JSON>` を実行する（`setup` を使うなら、JSON のパスを聞かれる）。

注意

- **クライアントシークレットは、作成直後の画面でしかダウンロードできない。** その画面を閉じると二度と表示されない。ダウンロードした JSON は、パスワードマネージャーなど安全な場所に保管する（別の PC で使うときや、鍵をなくしたときに要る）。
- 6か月使われていない OAuth クライアントは、自動で削除される。削除から 30日以内なら戻せる。普段使っていれば、トークンの更新で使われ続けるので、削除されない。
- GCP プロジェクトを消す、または OAuth クライアントを作り直すと、それまでに作ったファイルが CLI から見えなくなる。ファイル自体はドライブに残る。
- ダウンロードした JSON（`client_secret*.json`）は、リポジトリに入れない（`.gitignore` と trivy で防いでいる）。

### 3. `init` と `login`

```bash
gdrive-cli init
gdrive-cli login --client-secret ~/Downloads/client_secret_xxx.json
```

- `init` は設定ディレクトリを作り、拒否リスト（`policy.toml`）を置く。通信はしない。何度実行しても同じ結果になり、既にある `policy.toml` は書き換えない。`login` は `init` のあとでないと実行できない。
- `login` はブラウザを開いてログインを求め、トークンを暗号化して保存する。ログインしたアカウントのメールアドレスを表示し、専用のルートフォルダを確定させる。
- 初回は `--client-secret` が必須で、指定した JSON を検めてから、暗号化して設定ディレクトリに保存する。2回目以降は省略できる。
- 鍵がなくなった・合わなくなったなどで `client_secret.enc` を復号できないときも、`--client-secret` を付けて取り込み直す。
- ルートフォルダがなければ新しく作る。別の PC などで既にある場合は、それを見つけて使う。2つ以上見つかったときは、作成日時が最も古いものを選び、残りの ID とリンクを警告として出す。
- ルートフォルダがゴミ箱にあるときは、新しく作らずにエラーにする。ブラウザでゴミ箱から戻す。
- 以後のコマンドは、トークンの更新に失敗したときなどに終了コード 3 で止まる。そのときは `gdrive-cli login` をやり直す。

設定ディレクトリは、次の順で決まる。

1. 環境変数 `GDRIVE_CLI_CONFIG_DIR`
2. `$XDG_CONFIG_HOME/gdrive-cli`
3. `~/.config/gdrive-cli`

ディレクトリの権限は 0700、中のファイルは 0600。

| ファイル | 作るもの | 中身 |
|---|---|---|
| `policy.toml` | `init`、`setup` | 拒否リスト。人が手で編集する |
| `client_secret.enc` | `login --client-secret`、`setup` | ダウンロードした JSON を暗号化したもの |
| `token.enc` | `login` | OAuth トークン（リフレッシュトークンを含む）を暗号化したもの |
| `key` | `login`（`GDRIVE_CLI_KEYRING=file` のときだけ） | 暗号化の鍵 |
| `state.json` | CLI | ルートフォルダの ID など、CLI が自分で書く情報 |
| `lock` | CLI | 同時実行のためのロックファイル |

#### トークンの暗号化

`token.enc` と `client_secret.enc` は、AES-256-GCM で暗号化してある。鍵は 32 バイトの乱数で、置き場所は環境変数 `GDRIVE_CLI_KEYRING` で決まる。

| `GDRIVE_CLI_KEYRING` | 鍵の置き場所 |
|---|---|
| 未設定、空、`os` | OS のキーチェーン（macOS はキーチェーン、Linux は Secret Service）。サービス名は `gdrive-cli`、アカウント名は設定ディレクトリの絶対パス |
| `file` | 設定ディレクトリの `key`（権限 0600） |
| それ以外 | 終了コード 2 |

- 守れるもの: 設定ディレクトリのファイルだけが外に出た場合（バックアップ、dotfiles の同期、誤ったコミット、ログへの貼り付けなど）。鍵がなければ使えない。
- 守れないもの: 同じユーザーとして動くプログラム（AI エージェントを含む）が、gdrive-cli 本体を実行すること。ここでの守りは、`drive.file` スコープと、削除や共有の処理を持たない作り。設定ディレクトリのファイルの差し替えも守れない。Linux は今までどおり、同じユーザーのプログラムがキーチェーンの項目を読み出せる。
- macOS では、キーチェーンの項目を読めるのは gdrive-cli の実行ファイルだけ。ほかのプログラム（`security find-generic-password` を含む）が読もうとすると、ダイアログが出る。gdrive-cli にコードを差し込むことも、hardened runtime で防ぐ。
- macOS で入れ直したあとの最初の実行で、ダイアログが出る。「常に許可」を押す（ログインのパスワードを求められたら入れる）。AI エージェントに使わせる前に、自分で1回 `gdrive-cli status` を実行して許可しておく。ビルドし直すたびに、最初の実行でまたダイアログが出る。
- 前の版（go-keyring）で作った項目は、最初の実行で自動で新しい形に移る。標準エラーに「キーチェーンの鍵を新しい形に移しました」と1行出る。
- `GDRIVE_CLI_KEYRING=file` では、鍵が同じディレクトリにあるので、守りは暗号化しない場合とほぼ同じになる。キーチェーンのない環境（SSH の接続先の Linux、コンテナなど）で、明示したときだけ使う。
- キーチェーンが使えない（ロックされている、Secret Service がないなど）ときは、新しい鍵を作らずに終了コード 3 で止まり、`GDRIVE_CLI_KEYRING=file` の案内を出す。鍵を作るのは、クライアントシークレットを取り込む `login --client-secret`（と `setup`）だけ。
- 鍵がない、合わない、キーチェーンが使えない場合は、終了コード 3 で止まる。`status` は、ログインしていない扱いにして、理由を表示する。
- `GDRIVE_CLI_KEYRING=file` で使っている設定ディレクトリで、この環境変数を付け忘れると、OS のキーチェーンを探して「鍵が見つかりません」となる。設定ディレクトリに `key` があるときは、付け忘れていないかを尋ねる一言が添えられる。付け忘れたまま `login --client-secret` で取り込み直すと、キーチェーンに新しい鍵ができてしまうので、先に環境変数を確かめる。
- `logout` は、Google 側でトークンを無効にして `token.enc` を消すが、**鍵は消さない**。`client_secret.enc` が同じ鍵で暗号化されているため。
- 古い形式のファイル（`token.json`、`client_secret.json`）だけが残っているときは、終了コード 3 で `login --client-secret` のやり直しを求める。自動では移し替えない。ログインに成功すると、古いファイルは消える。

すべてを消すとき: 設定ディレクトリを消し、キーチェーンの `gdrive-cli` の項目も消す。macOS の例は次のとおり。

```bash
rm -r <設定ディレクトリの絶対パス>
security delete-generic-password -s gdrive-cli -a <設定ディレクトリの絶対パス>
```

ダイアログが出たら許可する。移し替えが途中で止まったときは、アカウント名が `<設定ディレクトリの絶対パス>#migrating` の項目が残ることがある。同じやり方で消す（`-a '<設定ディレクトリの絶対パス>#migrating'`）。

`<設定ディレクトリの絶対パス>` は、実際に使っているディレクトリに置き換える。`GDRIVE_CLI_CONFIG_DIR` や `XDG_CONFIG_HOME` を設定していると既定の `~/.config/gdrive-cli` とは限らないので、先に `gdrive-cli status` を実行して、表示される「設定ディレクトリ」で確かめる。消したものは元に戻せない。ドライブにある CLI のファイルは消えない。

## コマンドと例

共通のフラグは `--json`（結果を JSON で標準出力に出す。エラーも JSON にして標準エラーに出す）。普段は人が読みやすい表示になる。`setup` と `login` 以外のコマンドは、標準入力から何も読まず、入力を求めて止まらない。

ドライブ側のパスは、ルートフォルダを `/` とする（例: `/videos/2026-11/intro.mp4`）。先頭の `/` は省略してもよい。

| コマンド | 内容 |
|---|---|
| `init` | 設定ディレクトリと拒否リストを用意する |
| `setup [--project <ID>] [--client-secret <JSON>] [--no-browser]` | gcloud で GCP プロジェクトを用意し、手作業を案内して、ログインまで進める（人が端末で実行する） |
| `login [--client-secret <JSON>] [--no-browser]` | Google にログインする |
| `logout` | Google 側でトークンを無効化し、保存したトークンを消す |
| `status` | ログイン状態、アカウント、ルートフォルダの ID と URL、鍵の置き場所を表示する |
| `mkdir [-p] <パス>...` | フォルダを作る |
| `ls [-R] [-l] [パス]` | フォルダの中身（ファイルならそのファイル自身）を表示する |
| `upload [-p] [--skip-existing] <手元のパス>... <送り先のフォルダ>` | 手元のファイルやフォルダをドライブへ送る |
| `download [-p] [--overwrite \| --skip-existing] <ドライブのパス>... <手元のフォルダ>` | ドライブのファイルやフォルダを手元へ受け取る |
| `mv <移動元>... <移動先>` | ドライブの中で移す。名前も変えられる |

### status

```bash
gdrive-cli status
gdrive-cli status --json
```

AI エージェントが作業前に確認するためのコマンド。ルートフォルダがまだなければ、最初の `login` と同じ規則で `gdrive-cli` フォルダを作る。鍵の置き場所（`os` か `file`）も表示する（`--json` では `keyStore`）。ログインしていないときは、その理由（鍵がない、合わない、古い形式のファイルだけがある）も表示する（`--json` では `problem`）。キーチェーンが使えないときは、理由は `problem` には入らず、エラーとして標準エラーに出る（`--json` では標準エラーのエラーの JSON の `message`）。どの場合も終了コードは 3。

### mkdir

```bash
gdrive-cli mkdir /videos
gdrive-cli mkdir -p /videos/2026-11/drafts
```

- 既にある場合や、親フォルダがない場合はエラーにする。
- `-p` を付けると親フォルダもまとめて作り、既にあってもエラーにしない（何度実行しても同じ結果になる）。
- 引数の順に作り、最初のエラーで止まる。それまでに作れたものは標準出力に出る（`--json` でも同じ）。

### ls

```bash
gdrive-cli ls
gdrive-cli ls -l /videos
gdrive-cli ls -R --json /
```

- パスを省略すると `/` を表示する。
- `-R` で下の階層まですべて、`-l` で種類、サイズ、更新日時、ID も表示する。
- JSON の各要素は `id`、`name`、`path`、`type`（`file` か `folder`）、`size`、`mimeType`、`modifiedTime`、`webViewLink`。

### upload

```bash
gdrive-cli upload ./intro.mp4 /videos
gdrive-cli upload ./site ./report.html /pages
gdrive-cli upload -p ./out /archive/2026-11
gdrive-cli upload --skip-existing --json ./out /archive/2026-11
```

- 送り先のフォルダは、既にあるものでなければならない。`-p` を付けるとなければ作る。
- ファイルは `<送り先>/<ファイル名>` に、フォルダは `<送り先>/<フォルダ名>/...` に中身ごと送る。
- 同じ名前の扱い
  - ファイルとファイル: 既定ではエラー。`--skip-existing` を付けると飛ばす。上書きはできない。
  - フォルダとフォルダ: 既存のフォルダの中に合流し、中身に同じルールを当てはめる。
  - ファイルとフォルダのように種類が違う場合: 常にエラー。
- **書き込む前に計画を立てる。** 衝突がフラグなしで1つでもあれば、何もアップロードせずに終了コード 5 で返し、衝突の一覧を出す。
- 大きいファイルは、16 MiB ずつ分けて送る再開可能アップロードで送る。同時に送るファイルは最大 4 本。
- 一時的なエラー（429、5xx、通信エラー）は、1、2、4、8 秒と待ち時間を伸ばしながら、1 ファイルにつき最大 5 回まで、そのファイルを最初から送り直す。
  - 429 などの混み合いで断られたときは、何も作られていないので、そのまま送り直す。
  - 5xx や通信エラーのときは、ファイルが作られたかもしれないので、送り直す前に同じ名前のファイルを探す。大きさの合うものが 1 つだけあれば、それを送れたものとして扱う。なければ送り直す。大きさが違うものや 2 つ以上あるときは、二重に作らないよう、送り直さずにそのファイルを失敗にする。
- `.DS_Store` は飛ばす。シンボリックリンクはたどらず、警告を出して飛ばす。空のフォルダも作る。MIME タイプは拡張子から決める（Google ドキュメントなどへの変換はしない）。
- 結果は1件ごとに、ローカルのパス、ドライブ側のパス、ID、サイズ、ブラウザ用リンク（`webViewLink`）、処理（`created`、`merged`、`skipped` のどれか。`merged` は、既にあるフォルダに合流したこと）が出る。
- **JSON の結果で `error` が空でない項目は、その処理が終わっていない。** `action` の欄に出ているのは、行うつもりだった処理である。成功したかどうかは、`error` が空かどうかで判断する。
- 途中で失敗したファイルがあれば終了コード 1 になる。成功したものと失敗したものの両方が出る。続きは `--skip-existing` を付けて同じコマンドをやり直す。
- **フォルダの作成は自動ではやり直さない。** やり直すと同じ名前のフォルダが2つできるおそれがあるため。フォルダの作成が失敗したときは、`mkdir -p` か `upload --skip-existing` で自分でやり直す。

### download

```bash
gdrive-cli download /videos/intro.mp4 ./downloads
gdrive-cli download -p /pages ./backup/pages
gdrive-cli download --overwrite /pages ./backup
gdrive-cli download --skip-existing /pages ./backup
```

- 保存先のフォルダは、既にあるものでなければならない。`-p` を付けるとなければ作る。
- ファイルは `<保存先>/<名前>` に、フォルダは `<保存先>/<フォルダ名>/...` に中身ごと保存する。
- 手元に同じ名前があれば、既定ではエラー。`--overwrite` で上書き、`--skip-existing` で飛ばす（この2つは同時に指定できない）。ここでも書き込む前に計画を立て、衝突があれば何もしない。
- 一時ファイルに書き終えてから名前を変える。途中で止まったファイルが、完成品のように残ることはない。
- Google ドキュメント形式のファイルは、警告を出して飛ばす。
- 結果は upload と同じ形で、1件ごとに出る。処理は `created`、`merged`、`skipped`、`overwritten` のどれか。`merged` は手元に既にあるフォルダに合流したこと、`overwritten` は `--overwrite` で手元のファイルを置き換えたこと。
- **JSON の結果で `error` が空でない項目は、その処理が終わっていない。** `action` の欄には、行うつもりだった処理が出る。
- **macOS のように、大文字小文字や Unicode の正規化を区別しないファイルシステムでは、ドライブ上の2つの項目が同じ手元のファイルになる場合（例: `Report.txt` と `report.txt`）、`download` は何もせず終了コード 5 で止まる。** 片方を `mv` で別の名前に変えてから、やり直す。
- **`download --overwrite` が一部だけ失敗したときは、同じコマンドをもう一度実行する**（`--skip-existing` に変えない）。`--skip-existing` は名前だけで判断するため、途中まで書いたものを新しい中身で置き換えられない。

### mv

```bash
gdrive-cli mv /videos/intro.mp4 /archive
gdrive-cli mv /videos/intro.mp4 /archive/intro-v1.mp4
gdrive-cli mv /a.html /b.html /archive
```

- 移動先が既にあるフォルダなら、その中へ移す（名前は変えない）。
- 移すものが1つで、移動先がまだなく、その親フォルダがあるときは、移動と名前変更を同時に行う。
- 移動先に同じ名前があればエラー（`mv` に上書きはない）。
- 移す前にすべてを確かめるので、確かめで失敗すれば1つも移さない。移している途中で失敗したときは、それまでに移せたものが標準出力に出る（`--json` でも同じ）。
- ルートフォルダは移せない。フォルダを自分自身の中や、その下の階層へは移せない。
- 作り直したものは別の名前でアップロードし、古いほうは `mv` で保管用のフォルダへ移す使い方を想定している。動画や HTML の古い版は 30日で自動的に消えるので、差し替え（上書き）はできないようにしてある。

### logout

```bash
gdrive-cli logout
```

Google 側でトークンを無効化（revoke）してから、`token.enc`（と、あれば古い形式の `token.json`）を消す。暗号化の鍵は消さない。`client_secret.enc` が同じ鍵で暗号化されているので、鍵を残しておけば、次の `login` で JSON を指定し直さなくてよい。

## 終了コードの表

| 終了コード | 意味 |
|---|---|
| 0 | 成功 |
| 1 | 予期しないエラー（通信、API、ローカルの入出力）や、一部の失敗 |
| 2 | 引数の誤り |
| 3 | 準備が必要（`init` をしていない、トークンがない、トークンの更新に失敗した、client_secret がない、暗号化の鍵がない・合わない・キーチェーンが使えない、古い形式のファイルだけがある、`setup` で gcloud がない・ログインしていない） |
| 4 | 見つからない（ドライブ側のパス、またはローカルのパス） |
| 5 | 衝突（同じ名前がある、種類が違う） |
| 6 | パスがあいまい（同じ名前が複数ある） |
| 7 | 拒否リストで止められた（拒否リストが読めない場合も含む） |

同じフォルダに同じ名前のものが複数あるとき（ブラウザで名前を変えた場合や、同時実行が競合した場合に起こりうる）は、推測で選ばない。終了コード 6 で、候補の ID、更新日時、ブラウザ用リンクを出す。

## 拒否リスト（`policy.toml`）

`init` が設定ディレクトリに作るファイル。ここに書いたコマンドは、終了コード 7 で止まる。人が手で編集する。

```toml
# gdrive-cli で実行を拒否するコマンドの一覧。
# ここに書いたコマンドを実行しようとすると、終了コード 7 で止まる。
# 書ける名前: setup, login, logout, status, mkdir, upload, download, mv, ls
# 例: deny = ["mv", "download"]
deny = []
```

例えば AI エージェントに読み取りだけをさせたいときは、`deny = ["upload", "mv", "mkdir", "login", "logout"]` のようにする。ただし、ドライブ上にルートフォルダ（`gdrive-cli`）が見つからないときは、`status`、`ls`、`download` も、最初の1回だけルートフォルダを作る（`login` と同じ規則）。ドライブに何も書かせたくないときは、先に人が `login` を済ませて、ルートフォルダがある状態にしておく。

**安全側に倒す（fail closed）。** 誤字があったときに、拒否したつもりが素通りになるのを防ぐための規則。

| 状態 | 結果 |
|---|---|
| ファイルがない | 終了コード 3。`gdrive-cli init` を実行する |
| 読めない、または TOML として正しくない | 終了コード 7。すべてのコマンドが止まる |
| 知らないキーがある（例: `denny`） | 終了コード 7。すべてが止まる |
| 知らないコマンド名がある（例: `uplaod`） | 終了コード 7。すべてが止まる |

- `init` と `help` は、拒否リストの対象にならない。拒否リストを作る、または直すためのコマンドだから。`setup` はルートの判定から外れていて、途中で `policy.toml` を作ったあとに、自分で `setup` と `login` の両方が拒否されていないかを調べる（`setup` の中でログインもするため）。それ以外のすべてのコマンドは、実行の前に拒否リストを読む。
- 拒否リストは、誤操作を防ぐための柵であって、セキュリティの境界ではない。設定ディレクトリに書き込める人やプログラム（AI エージェントを含む）は書き換えられる。本当の境界は、`drive.file` スコープと、削除や共有の処理を持たない作りである（機械的な検査で守っている）。

## AI エージェントから使うとき

- **`--json` を付ける。** 成功したときは、標準出力に結果の JSON が1つだけ出る。失敗したときは、エラーの JSON（`{"error":{"kind":...,"message":...,"details":[...]}}`）が標準エラーに出る。upload、download、mkdir、mv が途中で失敗したときは、結果の JSON が標準出力に、エラーの JSON が標準エラーにと、両方に出る。結果は終了コードと JSON の両方で判断する。upload と download では、各項目の `error` が空でないものは、その処理が終わっていない。
- エラーに原因（Drive API や通信のエラーなど）があれば、`details` の最後の要素に入る（`--json` なしでは、最後の補足の行に出る）。
- **作業の前に `gdrive-cli status --json`** で、ログインできているかを確かめる。
- **大きなアップロードはバックグラウンドで実行する。** AI エージェントには、コマンドの実行時間に上限があることが多い（Claude Code の Bash ツールは既定で 2 分、最大で 10 分）。Claude Code なら `run_in_background` を使う。途中で止められた場合、そのファイルは最初から送り直しになる（再開には対応していない）。止まったあとは、`upload --skip-existing` で同じコマンドをやり直す。
- **`setup` と `login` は人が行う。** ブラウザでの操作が要り、標準入力も読むコマンドなので、エージェントには実行させない。`setup` は標準入力と標準エラーが端末でなければ、何もせずに終了コード 2 で止まる。さらに、本物の gcloud で GCP を変える（プロジェクトを作る）ので、なおさら人が行う。
- **終了コード 3 が返ったら、人に戻す。** 「`gdrive-cli init`、`gdrive-cli setup`、`gdrive-cli login` のどれかが必要」と伝えて、止まる。エージェントが自分でセットアップやログインをしようとしない。
- 削除や共有のコマンドはないので、取り返しのつかない操作は起きない。さらに絞りたいときは、拒否リストを使う。

## ブラウザのない環境

SSH の接続先、コンテナ、クラウドのサンドボックスなど、ブラウザやキーチェーンのない環境では、次のどちらかを使う。

### `login --no-browser`

キーチェーンのない環境では、鍵をファイルに置くよう `GDRIVE_CLI_KEYRING=file` を付ける。

```bash
gdrive-cli init
GDRIVE_CLI_KEYRING=file gdrive-cli login --no-browser --client-secret <JSON>
```

以後のコマンドでも、同じ環境変数を付ける。

1. CLI がログイン用の URL を表示する。
2. その URL を、別の PC のブラウザで開いてログインする。
3. ログイン後、ブラウザは `127.0.0.1` に飛ばされ、「接続できません」と表示される。そのときのアドレスバーの URL を丸ごとコピーして、CLI の標準入力に貼り付ける。
4. CLI がその URL から認可コードと `state` を取り出し、トークンに交換して、暗号化して保存する。

### 設定ディレクトリのコピー

ログイン済みの設定ディレクトリ（`~/.config/gdrive-cli/`）を別の環境へコピーし、`GDRIVE_CLI_CONFIG_DIR` で指定する。

- 鍵を OS のキーチェーンに置いている場合、設定ディレクトリをコピーしても、コピー先には鍵がないので使えない。これは暗号化の目的どおりの動き。
- コピーして使うなら、コピー元でも `GDRIVE_CLI_KEYRING=file` でログインし、`key` ごとコピーする。コピー先でも `GDRIVE_CLI_KEYRING=file` を付ける。

```bash
scp -rp ~/.config/gdrive-cli remote:/home/me/gdrive-cli-config
ssh remote 'chmod 700 /home/me/gdrive-cli-config && chmod 600 /home/me/gdrive-cli-config/*'
ssh remote 'GDRIVE_CLI_KEYRING=file GDRIVE_CLI_CONFIG_DIR=/home/me/gdrive-cli-config gdrive-cli status'
```

`scp -p` で権限を保ったうえで、コピー先でもディレクトリを 0700、中のファイルを 0600 にそろえる（`key` も含まれるため）。

トークンと鍵（`token.enc`、`key`）を含むので、渡す先は信頼できる環境だけにする。

## 既知の制約

- ブラウザやスマホから入れたファイルは、CLI が作ったフォルダの中にあっても CLI からは見えない（`drive.file` の仕様）。
- Drive は HTML をウェブページとして表示しない。リンクを開くと、ソースが表示されるか、ダウンロードになる。
- CLI が作ったファイルを、ブラウザでルートフォルダの外に移すと、パスでは辿れなくなる（ファイル自体は残る）。
- GCP プロジェクトを消す、または OAuth クライアントを作り直すと、それまでのファイルが CLI から見えなくなる。
- 拒否リスト（`policy.toml`）は誤操作を防ぐための柵で、設定ディレクトリに書き込めるもの（AI エージェントを含む）なら書き換えられる。
- **同時実行について。** 同じ PC の中では、フォルダを作る処理だけをロックファイルで1つずつ順番に行うので、同じ名前のフォルダは重複しにくい。ただし、同じフォルダに同じ名前のファイルを同時にアップロードした場合の競合は防がない。同じ名前のファイルが2つできることがあり、その後は終了コード 6 で気づける。複数の PC から同時に使わないこと。
- 途中で止められたアップロードを、次の実行で途中から再開することはできない。止まったファイルは最初から送り直しになる。
- 名前に `/` を含むもの（ブラウザでなら付けられる）も `ls` では見えるが、パスでは指定しにくい。

## 開発

```bash
mise install        # ツールを入れる。lefthook のフックも入る
make build          # リポジトリの直下に ./gdrive-cli を作る。macOS では hardened runtime を付ける
make install        # ~/.local/bin に入れる（BINDIR で変えられる）。macOS では hardened runtime を付ける
make lint           # golangci-lint run ./...
make fmt            # golangci-lint fmt
make test           # go test ./...
```

- macOS では、`make build` で作る（`go build` のままでは hardened runtime が付かず、キーチェーンを使えない）。ビルドし直すたびに、最初の実行でダイアログが出る。`make install` は、`~/.local/bin` に入れるときに使う。
- 実際の Drive を使う E2E テストは、既定では飛ばされる。先に `init` と `login` を済ませてから、次を実行する。ルートフォルダの下に `/e2e-<時刻>` を作り、終わると消す。

```bash
GDRIVE_CLI_E2E=1 go test ./internal/repository/driveapi/ -run TestE2E -v
```

- macOS では、`go test` の実行ファイルに hardened runtime が付かないので、E2E テストは OS のキーチェーンを使えない。普段の設定ディレクトリとは別のディレクトリで、`GDRIVE_CLI_KEYRING=file` を指定してログインしてから動かす。

```bash
export GDRIVE_CLI_CONFIG_DIR="$HOME/.config/gdrive-cli-e2e" GDRIVE_CLI_KEYRING=file
gdrive-cli init && gdrive-cli login --client-secret <JSON>
GDRIVE_CLI_E2E=1 go test ./internal/repository/driveapi/ -run TestE2E -v
```

- 普段の設定ディレクトリを `GDRIVE_CLI_KEYRING=file` で使うと、`key` が別にできて、普段の鍵と食い違う。そのため、別のディレクトリにする。

- ツールの版は `.mise.toml` に固定してある（go、golangci-lint、trivy、lefthook、node、pnpm）。CI も同じ版を使う。
- コミット時は lefthook が、gofmt、golangci-lint、go test、trivy（secret と vuln）、commitlint を走らせる。
- コミットメッセージの形式は `type: :emoji: 件名`（件名は日本語）。type と絵文字の対応は `commitlint.config.mjs` に従う。`--no-verify` は使わない。
- 層は cli、usecase、domain、repository の4つ。依存の向きは `internal/archtest` と golangci-lint の depguard が確かめる。削除と共有の API を本体のコードから呼べないことも、archtest が機械的に確かめている。
- タスクの管理には bd（beads）をステルスモードで使う（`bd ready`、`bd create`、`bd update <id> --status in_progress`、`bd close <id>`）。`.beads/` は git に入らない。
- 試し用のファイルは `rm` ではなく、`trash/` へ `mv` で片付ける。
- CI は commitlint、golangci-lint、`go test`、trivy を走らせる。
