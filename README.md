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

- Go 1.27.3 以上（`go install` で入れる場合）
- Google アカウント（個人のもの。Workspace ではない）
- Terraform 1.14 以上と、gcloud CLI（GCP のプロジェクトを作るため）
- ブラウザ（`login` のため。ない環境は「ブラウザのない環境」を参照）
- macOS を主に想定している。Linux でも動く。

入れ方

```bash
go install github.com/wadoyoka/gdrive-cli/cmd/gdrive-cli@latest
```

## セットアップ

流れは、(1) Terraform で GCP プロジェクトを作る、(2) 手作業で OAuth の設定をする、(3) `init` と `login` を実行する、の3段階。

### 1. Terraform

GCP のプロジェクトを作り、Drive API を有効にする。課金アカウントは紐づけない（Drive API は無料枠で使える想定）。

```bash
gcloud auth application-default login
terraform -chdir=infra init
terraform -chdir=infra apply -var project_id=<全世界で重複しないプロジェクト ID>
```

- `project_id` は、小文字で始まり、小文字・数字・ハイフンだけを使った 6〜30 文字で、最後は小文字か数字にする。
- プロジェクトは組織やフォルダに属させない。`deletion_policy = "PREVENT"` なので、`terraform destroy` ではプロジェクトを消せない。
- 課金がないので Compute Engine API は有効にならず、既定のネットワーク（VPC）は作られない。
- state はローカルに置かれる（`*.tfstate*` と `.terraform/` は git に入らない）。
- `apply` が終わると、`project_id`、`auth_overview_url`、`auth_clients_url` が出力される。次の手作業で、この2つの URL を使う。あとから見るには `terraform -chdir=infra output` を実行する。

quota project の警告やエラーが出たとき

ユーザーの認証情報（ADC）で動かすと、「quota project がない」という警告や、`API has not been used in project ...` のようなエラーが出ることがある。そのときは、自分が使える既存のプロジェクトを quota project に指定してから、やり直す。

```bash
gcloud auth application-default set-quota-project <自分が使える既存のプロジェクト ID>
```

環境変数で指定する場合は `export GOOGLE_BILLING_PROJECT=<プロジェクト ID>` と `export USER_PROJECT_OVERRIDE=true` でもよい。これでも解消しないときは、Terraform の実行に使うプロジェクトで `serviceusage.googleapis.com` と `cloudresourcemanager.googleapis.com` が有効かを確認する。

### 2. 手作業（OAuth 同意画面とクライアント）

Terraform では作れない部分。OAuth 同意画面と OAuth クライアントを作る公開 API がないため（Google の IAP OAuth Admin API は 2025年1月に非推奨になっている）。Terraform の出力した `auth_overview_url` と `auth_clients_url` から開ける。

1. Google Auth Platform の「ブランディング」で、アプリ名とサポート用メールアドレスを設定する。
2. 「対象（Audience）」で、ユーザーの種類を「外部（External）」にする。そのうえで公開ステータスを「本番（In production）」にする。
   - 「テスト」のままだとリフレッシュトークンが 7日で切れるため。
   - `drive.file` は影響が小さいスコープなので、本番にしても審査は要らず、「未確認のアプリ」警告も出ない想定。
3. 「データアクセス」で `https://www.googleapis.com/auth/drive.file` を追加する。
4. 「クライアント」で、種類が「デスクトップアプリ」のクライアントを作り、JSON をダウンロードする。
5. `gdrive-cli init` を実行し、続けて `gdrive-cli login --client-secret <ダウンロードした JSON>` を実行する。

注意

- GCP プロジェクトを消す、または OAuth クライアントを作り直すと、それまでに作ったファイルが CLI から見えなくなる。ファイル自体はドライブに残る。
- ダウンロードした JSON（`client_secret*.json`）は、リポジトリに入れない（`.gitignore` と trivy で防いでいる）。

### 3. `init` と `login`

```bash
gdrive-cli init
gdrive-cli login --client-secret ~/Downloads/client_secret_xxx.json
```

- `init` は設定ディレクトリを作り、拒否リスト（`policy.toml`）を置く。通信はしない。何度実行しても同じ結果になり、既にある `policy.toml` は書き換えない。`login` は `init` のあとでないと実行できない。
- `login` はブラウザを開いてログインを求め、トークンを保存する。ログインしたアカウントのメールアドレスを表示し、専用のルートフォルダを確定させる。
- 初回は `--client-secret` が必須で、指定した JSON を設定ディレクトリにコピーする。2回目以降は省略できる。
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
| `policy.toml` | `init` | 拒否リスト。人が手で編集する |
| `client_secret.json` | `login --client-secret` | ダウンロードした JSON のコピー |
| `token.json` | `login` | OAuth トークン（リフレッシュトークンを含む） |
| `state.json` | CLI | ルートフォルダの ID など、CLI が自分で書く情報 |
| `lock` | CLI | 同時実行のためのロックファイル |

トークンはファイルに保存する（OS のキーチェーンは使わない）。AI エージェントもこのファイルを読めるが、`drive.file` があるので被害は CLI の領域の中に限られる。

## コマンドと例

共通のフラグは `--json`（結果を JSON で標準出力に出す。エラーも JSON にして標準エラーに出す）。普段は人が読みやすい表示になる。`login` 以外のコマンドは、標準入力から何も読まず、入力を求めて止まらない。

ドライブ側のパスは、ルートフォルダを `/` とする（例: `/videos/2026-11/intro.mp4`）。先頭の `/` は省略してもよい。

| コマンド | 内容 |
|---|---|
| `init` | 設定ディレクトリと拒否リストを用意する |
| `login [--client-secret <JSON>] [--no-browser]` | Google にログインする |
| `logout` | Google 側でトークンを無効化し、保存したトークンを消す |
| `status` | ログイン状態、アカウント、ルートフォルダの ID と URL を表示する |
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

AI エージェントが作業前に確認するためのコマンド。ルートフォルダがまだなければ、最初の `login` と同じ規則で `gdrive-cli` フォルダを作る。

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

Google 側でトークンを無効化（revoke）してから、トークンファイルを消す。

## 終了コードの表

| 終了コード | 意味 |
|---|---|
| 0 | 成功 |
| 1 | 予期しないエラー（通信、API、ローカルの入出力）や、一部の失敗 |
| 2 | 引数の誤り |
| 3 | 準備が必要（`init` をしていない、トークンがない、トークンの更新に失敗した、client_secret がない） |
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
# 書ける名前: login, logout, status, mkdir, upload, download, mv, ls
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

- `init` と `help` は、拒否リストの対象にならない。拒否リストを作る、または直すためのコマンドだから。それ以外のすべてのコマンドは、実行の前に拒否リストを読む。
- 拒否リストは、誤操作を防ぐための柵であって、セキュリティの境界ではない。設定ディレクトリに書き込める人やプログラム（AI エージェントを含む）は書き換えられる。本当の境界は、`drive.file` スコープと、削除や共有の処理を持たない作りである（機械的な検査で守っている）。

## AI エージェントから使うとき

- **`--json` を付ける。** 成功したときは、標準出力に結果の JSON が1つだけ出る。失敗したときは、エラーの JSON（`{"error":{"kind":...,"message":...,"details":[...]}}`）が標準エラーに出る。upload、download、mkdir、mv が途中で失敗したときは、結果の JSON が標準出力に、エラーの JSON が標準エラーにと、両方に出る。結果は終了コードと JSON の両方で判断する。upload と download では、各項目の `error` が空でないものは、その処理が終わっていない。
- エラーに原因（Drive API や通信のエラーなど）があれば、`details` の最後の要素に入る（`--json` なしでは、最後の補足の行に出る）。
- **作業の前に `gdrive-cli status --json`** で、ログインできているかを確かめる。
- **大きなアップロードはバックグラウンドで実行する。** AI エージェントには、コマンドの実行時間に上限があることが多い（Claude Code の Bash ツールは既定で 2 分、最大で 10 分）。Claude Code なら `run_in_background` を使う。途中で止められた場合、そのファイルは最初から送り直しになる（再開には対応していない）。止まったあとは、`upload --skip-existing` で同じコマンドをやり直す。
- **`login` は人が行う。** ブラウザでの操作が要り、標準入力も読む唯一のコマンドなので、エージェントには実行させない。
- **終了コード 3 が返ったら、人に戻す。** 「`gdrive-cli init` か `gdrive-cli login` が必要」と伝えて、止まる。エージェントが自分でログインしようとしない。
- 削除や共有のコマンドはないので、取り返しのつかない操作は起きない。さらに絞りたいときは、拒否リストを使う。

## ブラウザのない環境

SSH の接続先、コンテナ、クラウドのサンドボックスなど、ブラウザを開けない環境では、次のどちらかを使う。

### `login --no-browser`

```bash
gdrive-cli login --no-browser --client-secret <JSON>
```

1. CLI がログイン用の URL を表示する。
2. その URL を、別の PC のブラウザで開いてログインする。
3. ログイン後、ブラウザは `127.0.0.1` に飛ばされ、「接続できません」と表示される。そのときのアドレスバーの URL を丸ごとコピーして、CLI の標準入力に貼り付ける。
4. CLI がその URL から認可コードと `state` を取り出し、トークンに交換して保存する。

### 設定ディレクトリのコピー

ログイン済みの設定ディレクトリ（`~/.config/gdrive-cli/`）を別の環境へコピーし、`GDRIVE_CLI_CONFIG_DIR` で指定する。

```bash
scp -r ~/.config/gdrive-cli remote:/home/me/gdrive-cli-config
ssh remote 'GDRIVE_CLI_CONFIG_DIR=/home/me/gdrive-cli-config gdrive-cli status'
```

トークンのファイル（`token.json`）を含むので、渡す先は信頼できる環境だけにする。

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
make lint           # golangci-lint run ./...
make fmt            # golangci-lint fmt
make test           # go test ./...
make tf-check       # infra/ の terraform fmt -check、init、validate
```

- 実際の Drive を使う E2E テストは、既定では飛ばされる。先に `init` と `login` を済ませてから、次を実行する。ルートフォルダの下に `/e2e-<時刻>` を作り、終わると消す。

```bash
GDRIVE_CLI_E2E=1 go test ./internal/repository/ -run TestE2E -v
```

- ツールの版は `.mise.toml` に固定してある（go、golangci-lint、trivy、lefthook、terraform、node、pnpm）。CI も同じ版を使う。
- コミット時は lefthook が、gofmt、golangci-lint、go test、trivy（secret と vuln）、`infra/` の `terraform fmt -check`、commitlint を走らせる。
- コミットメッセージの形式は `type: :emoji: 件名`（件名は日本語）。type と絵文字の対応は `commitlint.config.mjs` に従う。`--no-verify` は使わない。
- 層は cli、usecase、domain、repository の4つ。依存の向きは `internal/archtest` と golangci-lint の depguard が確かめる。削除と共有の API を本体のコードから呼べないことも、archtest が機械的に確かめている。
- タスクの管理には bd（beads）をステルスモードで使う（`bd ready`、`bd create`、`bd update <id> --status in_progress`、`bd close <id>`）。`.beads/` は git に入らない。
- 試し用のファイルは `rm` ではなく、`trash/` へ `mv` で片付ける。
- CI は commitlint、golangci-lint、`go test`、trivy、`terraform fmt -check` と `validate` を走らせる。trivy の Terraform 向けの misconfig 検査（`trivy fs --scanners misconfig infra`）も CI に入れている。`google_project` の GCP-0010（既定のネットワークを作る設定）だけは、`infra/main.tf` の `#trivy:ignore:GCP-0010` で外している。`auto_create_network = false` にすると、provider が既定のネットワークを消すために Compute Engine API を有効にしようとし、課金を紐づけないこのプロジェクトでは `apply` が失敗するため。課金がなければ Compute Engine API は有効にならないので、既定のネットワークは作られない。
