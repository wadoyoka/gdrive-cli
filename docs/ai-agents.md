# AI エージェントから使うとき

Claude Code や Codex には、この決まりをまとめたスキルを配っている。[agents/](../agents/README.md) の `setup.sh` で入れられる。

- **`--json` を付ける。** 成功したときは、標準出力に結果の JSON が1つだけ出る。失敗したときは、エラーの JSON（`{"error":{"kind":...,"message":...,"details":[...]}}`）が標準エラーに出る。upload、download、mkdir、mv が途中で失敗したときは、結果の JSON が標準出力に、エラーの JSON が標準エラーにと、両方に出る。結果は終了コードと JSON の両方で判断する。upload と download では、各項目の `error` が空でないものは、その処理が終わっていない。
- エラーに原因（Drive API や通信のエラーなど）があれば、`details` の最後の要素に入る（`--json` なしでは、最後の補足の行に出る）。
- **Windows の Claude Code（Git Bash）では、ドライブ側のパスの先頭を `//` にする。** Git Bash が `/videos` を `C:/Program Files/Git/videos` に書き換えるため（例: `gdrive-cli upload ./a.txt //videos`、`gdrive-cli ls -R //`）。書き換えられたパスや `\` 区切りのパスは、終了コード 2 で止まる。手元のパスはそのままでよい。`MSYS_NO_PATHCONV=1` は付けない。手元のパスの書き換えまで止まり、`~/out` が `C:\c\Users\...` のような別の場所として読まれて壊れるため。
- `agents/setup.sh` は Windows に対応していない（シンボリックリンクを張る sh のスクリプトのため）。Windows では、`agents/skills/gdrive-cli/` を手でコピーして、スキルの置き場所に入れる。
- **作業の前に `gdrive-cli status --json`** で、ログインできているかを確かめる。
- **大きなアップロードはバックグラウンドで実行する。** AI エージェントには、コマンドの実行時間に上限があることが多い（Claude Code の Bash ツールは既定で 2 分、最大で 10 分）。Claude Code なら `run_in_background` を使う。途中で止められた場合、そのファイルは最初から送り直しになる（再開には対応していない）。止まったあとは、`upload --skip-existing` で同じコマンドをやり直す。
- **`setup` と `login` は人が行う。** ブラウザでの操作が要り、標準入力も読むコマンドなので、エージェントには実行させない。`setup` は標準入力と標準エラーが端末でなければ、何もせずに終了コード 2 で止まる。さらに、本物の gcloud で GCP を変える（プロジェクトを作る）ので、なおさら人が行う。
- **終了コード 3 が返ったら、人に戻す。** 「`gdrive-cli init`、`gdrive-cli setup`、`gdrive-cli login` のどれかが必要」と伝えて、止まる。エージェントが自分でセットアップやログインをしようとしない。公開ステータスが「テスト」なので、ログインは 7日で切れる。前に使えていたのに終了コード 3 になったときは、たいていこれで、人が `gdrive-cli login` をやり直せば済む。
- 削除や共有のコマンドはないので、取り返しのつかない操作は起きない。さらに絞りたいときは、拒否リスト（[policy.md](policy.md)）を使う。
- macOS では、gdrive-cli を入れ直した直後の最初の実行でキーチェーンのダイアログが出る。エージェントに使わせる前に、人が1回 `gdrive-cli status` を実行して許可しておく（[setup.md](setup.md#鍵の置き場所)）。
