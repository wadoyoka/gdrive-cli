# gdrive-cli コマンドの早見表（エージェント向け）

すべてのコマンドに `--json` を付けて使う。`setup`・`login`・`logout`・`init` は人が実行するので、ここには載せない。

## 共通

- ドライブ側のパスは、ルートフォルダを `/` とする。先頭の `/` は省略してもよい。
- 成功時: 標準出力に結果の JSON が 1 つ（1 行）出る。
- 失敗時: 標準エラーに次の JSON が出る。原因（Drive API や通信のエラー）は `details` の最後の要素に入る。

  ```json
  {"error":{"kind":"conflict","message":"...","details":["..."]}}
  ```

- `upload`・`download`・`mkdir`・`mv` が途中で失敗したときは、できた分の結果が標準出力に、エラーが標準エラーに、と両方に出る。

## 終了コード

| 終了コード | `kind` | 意味 | エージェントのすること |
|---|---|---|---|
| 0 | — | 成功 | 結果を人に伝える |
| 1 | `unexpected` | 予期しないエラー、または一部の失敗 | `items[].error` を見る。送受信なら同じコマンドに `--skip-existing` を付けてやり直す |
| 2 | `usage` | 引数の誤り | コマンドを直す |
| 3 | `setup` | 準備が必要（ログインしていない、鍵がない、キーチェーンが使えないなど） | 止まって人に戻す |
| 4 | `not_found` | パスが見つからない（ドライブ側または手元） | `ls` で確かめる。ブラウザから入れたファイルは見えないことも伝える |
| 5 | `conflict` | 同じ名前がある、種類が違う | 何もされていない。別の名前にするか、人に確かめる |
| 6 | `ambiguous` | 同じ名前が複数ある | 候補を人に見せて判断を仰ぐ。推測で選ばない |
| 7 | `denied` | 拒否リスト（`policy.toml`）で止められた | 止まって人に伝える。回避しない |

## status

```bash
gdrive-cli status --json
```

```json
{"configDir":"...","loggedIn":true,"keyStore":"os","email":"...","rootFolderId":"...","rootFolderLink":"https://drive.google.com/..."}
```

ログインしていないときは `loggedIn` が `false` で、`problem` に理由が入り、終了コードは 3。
キーチェーンが使えないときは、理由は標準エラーのエラーの JSON の `message` に出る。

## ls

```bash
gdrive-cli ls --json                # ルートフォルダの直下
gdrive-cli ls --json /videos
gdrive-cli ls -R --json /videos     # 下の階層まですべて
```

結果は配列。各要素は次の形。`type` は `file` か `folder`。

```json
{"id":"...","name":"intro.mp4","path":"/videos/intro.mp4","type":"file","size":123,"mimeType":"video/mp4","modifiedTime":"2026-11-14T00:00:00Z","webViewLink":"https://drive.google.com/..."}
```

パスがファイルなら、そのファイル自身が 1 件だけ返る。

## mkdir

```bash
gdrive-cli mkdir -p --json /videos/2026-11
```

- `-p` なしでは、既にある・親がないとエラー。`-p` を付けると親もまとめて作り、既にあってもエラーにしない。
- 結果は、作ったフォルダの配列（形は `ls` と同じ）。

## upload

```bash
gdrive-cli upload [-p] [--skip-existing] --json <手元のパス>... <送り先のフォルダ>
```

- 送り先のフォルダは既にあるものでなければならない。`-p` でなければ作る。
- 同じ名前の扱い
  - ファイルとファイル: 既定ではエラー（終了コード 5）。`--skip-existing` で飛ばす。上書きはできない。
  - フォルダとフォルダ: 既存のフォルダに合流し、中身に同じ規則を当てはめる。
  - 種類が違う: 常にエラー。
- 書き込む前に計画を立てる。フラグなしの衝突が 1 つでもあれば、何もアップロードせずに終了コード 5 で衝突の一覧を出す。
- 大きいファイルは 16 MiB ずつ送る。同時に送るのは最大 4 本。一時的なエラーは 1 ファイルにつき最大 5 回まで送り直す。
- `.DS_Store` とシンボリックリンクは飛ばす（シンボリックリンクは警告が出る）。空のフォルダも作る。
- 途中で止められたアップロードは再開できない。止まったファイルは、`--skip-existing` を付けたやり直しで最初から送られる。

結果:

```json
{"items":[{"localPath":"./out/a.html","remotePath":"/pages/out/a.html","id":"...","type":"file","size":123,"webViewLink":"https://drive.google.com/...","action":"created"}],"warnings":["..."]}
```

- `action` は `created`（作った）・`merged`（既存のフォルダに合流した）・`skipped`（飛ばした）のどれか。
- **`error` が空でない項目は終わっていない。** そのときの `action` は行うつもりだった処理。

## download

```bash
gdrive-cli download [-p] [--overwrite | --skip-existing] --json <ドライブのパス>... <手元のフォルダ>
```

- 保存先のフォルダは既にあるものでなければならない。`-p` でなければ作る。
- 手元に同じ名前があれば、既定ではエラー（終了コード 5、何もしない）。`--overwrite` で上書き、`--skip-existing` で飛ばす。
- Google ドキュメント形式のファイルは、警告を出して飛ばす。
- Windows では、Windows のファイル名に使えない名前（`: * ? " < > |` を含むもの、末尾が `.` か空白のもの、`CON`・`NUL`・`COM1` などの予約名）も、警告を出して飛ばす。
- 結果の形は `upload` と同じ。`action` には `overwritten` もある。
- `--overwrite` が一部だけ失敗したときは、同じコマンドをもう一度実行する（`--skip-existing` に変えない）。
- macOS など大文字小文字を区別しない手元のファイルシステムで、ドライブ上の 2 つの項目が同じ手元のファイルになるとき（`Report.txt` と `report.txt`）は、終了コード 5 で何もしない。

## mv

```bash
gdrive-cli mv --json /videos/intro.mp4 /archive              # フォルダの中へ移す
gdrive-cli mv --json /videos/intro.mp4 /archive/intro-v1.mp4 # 移して名前を変える
```

- 移動先が既にあるフォルダなら、その中へ移す。
- 移すものが 1 つで、移動先がまだなく、その親があるときは、移動と名前変更を同時に行う。
- 移動先に同じ名前があればエラー（上書きはない）。
- 結果は配列。各要素は `{"from":"...","to":"...","id":"...","webViewLink":"..."}`。
- ルートフォルダは移せない。フォルダを自分の中へは移せない。
