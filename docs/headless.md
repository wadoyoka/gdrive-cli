# ブラウザのない環境

SSH の接続先、コンテナ、クラウドのサンドボックスなど、ブラウザやキーチェーンのない環境では、次のどちらかを使う。

## `login --no-browser`

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

**他人から渡された URL は貼り付けない。** 自分でログインして、自分でアドレスバーからコピーした URL だけを貼る。他人が自分のアカウントでログインして得た URL を貼ると、CLI はその人のドライブにログインした状態になる。そのあとのアップロードは、その人のドライブに入る。`state` が合わなければ CLI は止まるが、ログイン用の URL が相手に見えていれば、相手は同じ `state` を使える。

## 設定ディレクトリのコピー

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
