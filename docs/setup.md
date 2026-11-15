# セットアップの詳細

手順の要点は [README](../README.md#プロジェクトのセットアップ) にある。ここでは、`setup` の動き、手作業の内容、`init` と `login`、設定ディレクトリを詳しく書く。

流れは、(1) GCP プロジェクトを作って Drive API を有効にする、(2) OAuth の設定を手作業でする、(3) `init` と `login` を実行する、の3段階。`gdrive-cli setup` を使えば、この3段階を1つのコマンドで案内してもらえる。

## 1. `gdrive-cli setup`

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

## 2. 手作業（OAuth 同意画面とクライアント）

OAuth 同意画面と OAuth クライアントを作る公開 API がないため、ここだけは手作業になる（Google の IAP OAuth Admin API は 2025年1月に非推奨になっている）。`setup` は同じ内容を表示する。`setup` を使わない人は、Google Cloud コンソールの Google Auth Platform を開き、自分のプロジェクトを選んで、次を行う。

1. 「ブランディング」（`https://console.cloud.google.com/auth/branding?project=<プロジェクト ID>`）で、アプリ名とサポート用メールアドレスを設定する。
2. 「対象（Audience）」（`.../auth/audience?project=<プロジェクト ID>`）で、ユーザーの種類を「外部（External）」にする。公開ステータスは「テスト（Testing）」のままにする。そのうえで「テストユーザー」に、この CLI でドライブを使う Google アカウントを追加する。
   - 自分のアカウントも追加する。プロジェクトの持ち主でも、テストユーザーでなければログインできない。
   - 別の人に使ってもらうときは、その人の Google アカウントを追加する（テストユーザーは最大 100 人）。各自が自分のドライブにログインする。その人には保管しておいた JSON を渡し、`gdrive-cli init` と `gdrive-cli login --client-secret <JSON>` を実行してもらう（その人は gcloud も `setup` も要らない）。
   - 「テスト」では、リフレッシュトークンが 7日で切れる。切れたあとは、コマンドが終了コード 3 で止まるので、`gdrive-cli login` をやり直す（取り込み済みの JSON を使うので、`--client-secret` は要らない）。
   - ログインの画面で「Google はこのアプリを確認していません」と出たら、「続行」で進む。
3. 「データアクセス」（`.../auth/scopes?project=<プロジェクト ID>`）で `https://www.googleapis.com/auth/drive.file` を追加する。
4. 「クライアント」（`.../auth/clients?project=<プロジェクト ID>`）で、種類が「デスクトップアプリ」のクライアントを作り、JSON をダウンロードする。
5. `gdrive-cli init` を実行し、続けて `gdrive-cli login --client-secret <ダウンロードした JSON>` を実行する（`setup` を使うなら、JSON のパスを聞かれる）。

注意

- **クライアントシークレットは、作成直後の画面でしかダウンロードできない。** その画面を閉じると二度と表示されない。ダウンロードした JSON は、パスワードマネージャーなど安全な場所に保管する（別の PC で使うときや、鍵をなくしたときに要る）。
- 6か月使われていない OAuth クライアントは、自動で削除される。削除から 30日以内なら戻せる。普段使っていれば、トークンの更新で使われ続けるので、削除されない。
- GCP プロジェクトを消す、または OAuth クライアントを作り直すと、それまでに作ったファイルが CLI から見えなくなる。ファイル自体はドライブに残る。
- ダウンロードした JSON（`client_secret*.json`）は、リポジトリに入れない（`.gitignore` と trivy で防いでいる）。

## 3. `init` と `login`

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
- 以後のコマンドは、トークンの更新に失敗したときなどに終了コード 3 で止まる。そのときは `gdrive-cli login` をやり直す。公開ステータスが「テスト」なので、7日ごとに起こる。
- ログインするアカウントは、OAuth 同意画面のテストユーザーに入っていなければならない。入っていないと、ブラウザの画面でアクセスを拒まれる。

## 設定ディレクトリ

設定ディレクトリは、次の順で決まる。

1. 環境変数 `GDRIVE_CLI_CONFIG_DIR`
2. `$XDG_CONFIG_HOME/gdrive-cli`
3. `~/.config/gdrive-cli`

Windows では、次の順で決まる。

1. 環境変数 `GDRIVE_CLI_CONFIG_DIR`
2. `%LOCALAPPDATA%\gdrive-cli`

Windows では `XDG_CONFIG_HOME` を見ない。見ると、Git Bash と PowerShell で設定ディレクトリが分かれてしまう。`%APPDATA%` ではなく `%LOCALAPPDATA%` にするのは、鍵がその PC にしか残らないので、ファイルも PC の外へ移さないため。

ディレクトリの権限は 0700、中のファイルは 0600。Windows では数字は効かず、`%LOCALAPPDATA%` のフォルダの ACL で守られる。`GDRIVE_CLI_CONFIG_DIR` でほかの場所にするときは、そのフォルダの ACL に注意する。

| ファイル | 作るもの | 中身 |
|---|---|---|
| `policy.toml` | `init`、`setup` | 拒否リスト。人が手で編集する（[policy.md](policy.md)） |
| `client_secret.enc` | `login --client-secret`、`setup` | ダウンロードした JSON を暗号化したもの |
| `token.enc` | `login` | OAuth トークン（リフレッシュトークンを含む）を暗号化したもの |
| `key` | `login`（`GDRIVE_CLI_KEYRING=file` のときだけ） | 暗号化の鍵 |
| `state.json` | CLI | ルートフォルダの ID など、CLI が自分で書く情報 |
| `lock` | CLI | 同時実行のためのロックファイル |

## 鍵の置き場所

`token.enc` と `client_secret.enc` を暗号化する鍵の置き場所は、環境変数 `GDRIVE_CLI_KEYRING` で決まる。

| `GDRIVE_CLI_KEYRING` | 鍵の置き場所 |
|---|---|
| 未設定、空、`os` | OS のキーチェーン（macOS はキーチェーン、Linux は Secret Service） |
| `file` | 設定ディレクトリの `key`。キーチェーンのない環境（SSH の接続先、コンテナなど）で使う。以後のコマンドにも毎回付ける |

- macOS で gdrive-cli を入れ直したあとの最初の実行では、キーチェーンのダイアログが出る。**ダイアログに gdrive-cli と書いてあるときだけ「常に許可」を押す。** `security` やほかのプログラムの名前が出たら「拒否」を押す（許可すると、そのプログラムも鍵を読めるようになる）。AI エージェントに使わせる前に、自分で1回 `gdrive-cli status` を実行して許可しておく。
- 鍵がない・合わない・キーチェーンが使えないときは、終了コード 3 で止まる。`gdrive-cli login --client-secret <JSON>` で取り込み直す。
- `logout` は鍵を消さない（`client_secret.enc` が同じ鍵で暗号化されているため）。

すべてを消すとき（macOS の例）は、設定ディレクトリとキーチェーンの項目を消す。`<設定ディレクトリの絶対パス>` は、`gdrive-cli status` が表示する「設定ディレクトリ」で確かめる。ドライブにある CLI のファイルは消えない。

```bash
rm -r <設定ディレクトリの絶対パス>
security delete-generic-password -s gdrive-cli -a <設定ディレクトリの絶対パス>
```
