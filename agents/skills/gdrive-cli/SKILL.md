---
name: gdrive-cli
description: >-
  gdrive-cli で、個人の Google ドライブ（マイドライブの専用フォルダ）へファイルやフォルダをアップロードし、
  一覧・フォルダ作成・移動・ダウンロードを行う。「ドライブに上げて」「Google Drive にアップロードして」
  「作った HTML／動画をドライブに置いて」「ドライブのフォルダを整理して」「ドライブから取ってきて」など、
  Google ドライブへの送受信や整理を頼まれたら使う。gdrive-cli のセットアップやログインは人が行うもので、このスキルの対象外。
  Use when the user asks to upload, list, organize, or download files on their Google Drive with gdrive-cli.
---

# gdrive-cli ― Google ドライブへの送受信と整理

`gdrive-cli` は、個人の Google ドライブの専用フォルダ（ルートフォルダ。既定の名前は `gdrive-cli`）の下だけを操作する CLI。
削除・ゴミ箱への移動・共有設定の変更・上書きアップロードの機能は持たない。取り返しのつかない操作は起きないが、
**二重に作る・人の作業を勝手に進める** ことは起こりうるので、下の手順と決まりを守る。

## 手順

### 1. 使える状態かを確かめる

```bash
command -v gdrive-cli
gdrive-cli status --json
```

- `gdrive-cli` が見つからなければ止まり、人にインストールを頼む（リポジトリの README に手順がある）。
- `status --json` の `loggedIn` が `true` なら次へ進む。
- 終了コード 3（`loggedIn` が `false`、鍵がない、キーチェーンが使えないなど）なら **止まって人に戻す**。
  「`gdrive-cli setup`（初回）か `gdrive-cli login` が必要です」と、`problem` やエラーの `message` を添えて伝える。
  自分で `setup`・`login`・`init` を実行しない。
  ログインは 7日で切れる（OAuth 同意画面を「テスト」で運用しているため）。前に使えていたのに終了コード 3 になったときは、
  「ログインの期限が切れたようです。`gdrive-cli login` を実行してください」と伝える。

### 2. 送り先を決める

- ドライブ側のパスは、ルートフォルダを `/` とする（例: `/videos/2026-11/intro.mp4`）。
- Windows の Git Bash で実行するときは、ドライブ側のパスを `//` で始める（例: `gdrive-cli upload ./a.txt //videos`、ルートは `//`）。手元のパスはそのまま書く。`C:/...` の形や `\` を含むドライブ側のパスは、終了コード 2 で止まる。`MSYS_NO_PATHCONV=1` は付けない（手元のパスが壊れる）。PowerShell と cmd では `/videos` のままでよい。
- 送り先の候補が分からなければ、`gdrive-cli ls --json /` や `gdrive-cli ls -R --json /<フォルダ>` で見てから決める。
- 人が送り先を指定していなければ、中身の分かる名前のフォルダを提案する（例: `/pages/2026-11`）。

### 3. アップロードする

```bash
gdrive-cli upload -p --json ./report.html /pages/2026-11
gdrive-cli upload -p --json ./site ./intro.mp4 /archive/2026-11
```

- 最後の引数が送り先のフォルダ。`-p` で、なければ作る。
- ファイルは `<送り先>/<ファイル名>`、フォルダは `<送り先>/<フォルダ名>/...` に中身ごと入る。
- **大きいファイル（目安 100 MB 以上）や、たくさんのファイルはバックグラウンドで実行する。**
  コマンドの実行時間の上限（Claude Code の Bash ツールは既定 2 分、最大 10 分）で止められると、そのファイルは最初から送り直しになる。
  Claude Code なら `run_in_background` を使う。

### 4. 結果を確かめる

成功・失敗は、**終了コードと JSON の両方**で判断する。

- 終了コード 0: 全件成功。標準出力の JSON の `items[]` に、各項目の `remotePath`・`webViewLink`・`action`（`created`・`merged`・`skipped`）が入る。
- 終了コード 1: 一部が失敗。**`items[]` で `error` が空でない項目は終わっていない**（`action` は行うつもりだった処理）。
  続きは、同じコマンドに `--skip-existing` を付けてやり直す。
- 終了コード 5（衝突）: 送り先に同じ名前がある。**何もアップロードされていない。** 下の「上書きしない」を参照。

### 5. 人に伝える

アップロードしたものの `remotePath` と `webViewLink` を伝える。HTML は、ドライブ上ではウェブページとして表示されない
（リンクを開くとソースが出るか、ダウンロードになる）ことも、HTML を送ったときは一言添える。

## 決まり

- **`--json` を付ける。** 成功時は標準出力に結果の JSON が 1 つ、失敗時は標準エラーに `{"error":{"kind":...,"message":...,"details":[...]}}` が出る。
  原因（Drive API や通信のエラー）は `details` の最後に入る。
- **`setup`・`login`・`logout`・`init` は実行しない。** ブラウザでの操作が要る、GCP を変える、ログインを消すなど、人が行う操作のため。
- **上書きしない。** ドライブ上のファイルを差し替える機能はない。作り直したものは別の名前（例: `report-v2.html`）で上げ、
  古いほうは人に確かめてから `gdrive-cli mv` で保管用のフォルダへ移す。衝突したからといって、勝手に `--skip-existing` を付けない
  （`--skip-existing` は名前だけを見て飛ばすので、新しい中身が上がらない）。`--skip-existing` は、途中で止まったアップロードの続きにだけ使う。
- **終了コード 6（あいまい）では推測で選ばない。** 同じフォルダに同じ名前が複数ある。エラーに出る候補の ID・更新日時・リンクを人に見せて判断を仰ぐ。
- **終了コード 7（拒否リスト）では止まる。** 人が `policy.toml` でそのコマンドを禁じている。`policy.toml` を書き換えたり、別の手で回避したりしない。
- **フォルダの作成は自動ではやり直されない。** フォルダの作成が失敗したら、`gdrive-cli mkdir -p <パス>` か `upload --skip-existing` でやり直す。
- 鍵や設定ディレクトリ（`~/.config/gdrive-cli/`、Windows では `%LOCALAPPDATA%\gdrive-cli\` など）のファイル、`security` コマンドには触れない。Windows では `cmdkey` と資格情報マネージャーにも触れない（`cmdkey /delete` で鍵が消えると、JSON の取り込みからやり直しになる）。
- 見えるのは gdrive-cli が作ったファイルとフォルダだけ。ブラウザやスマホから入れたファイルは、ルートフォルダの中にあっても見えない。
  「見つからない」（終了コード 4）ときは、それが原因のこともあると人に伝える。

## コマンドの詳細

各コマンドのフラグ、JSON の形、終了コードの一覧は [references/commands.md](references/commands.md) にある。
`gdrive-cli <コマンド> --help` でも確かめられる。
