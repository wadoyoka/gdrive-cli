#!/usr/bin/env bash
# gdrive-cli の普段使いのコマンド（status・mkdir・ls・upload・download・mv）が、
# 本物のドライブで正しく動くかを確かめる。ログイン済みが前提で、login はしない。
#
# 使い方:
#   tests/scripts/e2e-cli.sh                              # PATH の gdrive-cli（make install で入れたもの）
#   GDRIVE_CLI_BIN=./gdrive-cli tests/scripts/e2e-cli.sh  # make build で作ったもの
#
# - ドライブには、ルートフォルダの下に作業用フォルダ /e2e-cli-<日時>-<PID> を作り、その中だけを使う。
#   gdrive-cli には削除がないので、終わったら作業用フォルダをブラウザで消す（最後に場所とリンクを出す）。
# - 手元には作業場所を mktemp -d で作り、終わったら（失敗や中断でも）消す。
# - 設定ディレクトリ（policy.toml・状態）には触れない。GDRIVE_CLI_CONFIG_DIR・GDRIVE_CLI_KEYRING はそのまま渡す。
# - 要るもの: bash（3.2 以上）、jq、sha256sum か shasum。Git Bash（Windows）でも動く。
#
# 終了コード: 0 すべて通った / 1 通らなかったものがある / 2 前提が足りない / 130 中断

# macOS の bash 3.2 は、変数のすぐ後の日本語の頭のバイトを変数名の続きとして読む。日本語が続く変数は ${名前} と書く。
# jq の式の中の $w などは jq の変数なので、シェルで展開させない（単引用符のままでよい）。
# shellcheck disable=SC2016
set -u

PASS=0
FAIL=0
# 手元の作業場所。TMP は Git Bash などで export されている名前なので、使わない。
LOCAL_WORK=""
WORK=""
WORK_TRIED=0
WORK_MADE=0
WORK_LINK=""
CMD=""
CODE=""
OUT=""
ERR=""

say() { printf '%s\n' "$*"; }

die() {
	printf 'e2e-cli: %s\n' "$*" >&2
	exit 2
}

# --- 前提 -------------------------------------------------------------------

bin_arg="${GDRIVE_CLI_BIN:-gdrive-cli}"
case "$bin_arg" in
*/*)
	if [ ! -f "$bin_arg" ] || [ ! -x "$bin_arg" ]; then
		die "GDRIVE_CLI_BIN の $bin_arg が見つからないか、実行できません"
	fi
	# 相対パスでも動くよう、絶対パスにしておく。CDPATH があると cd がそちらへ移ってパスを出すので、空にする。
	BIN="$(CDPATH='' cd -- "$(dirname -- "$bin_arg")" && pwd)/$(basename -- "$bin_arg")"
	;;
*)
	BIN="$(command -v "$bin_arg")" || die "$bin_arg が PATH にありません。make install で入れるか、GDRIVE_CLI_BIN で場所を指定してください"
	;;
esac

command -v jq >/dev/null 2>&1 || die "jq がありません。入れてから動かしてください"

# Git Bash は / で始まる引数を、Windows の実行ファイルに渡すときに C:/Program Files/Git/... に書き換える。
# jq に渡すドライブ側のパス（--arg w /e2e-cli-...）が書き換えられないよう、jq はすべて jqx で呼ぶ。
# jq には手元のパスを渡さないので、書き換えを止めても困らない。gdrive-cli には付けない（手元のパスの書き換えまで止まるため）。
jqx() { MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' jq "$@"; }

# jq_raw は値を文字列で取り出す。Windows の jq は改行を CRLF で出すので、\r を除く。
jq_raw() { jqx -r "$@" | tr -d '\r'; }

if command -v sha256sum >/dev/null 2>&1; then
	SHA=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
	SHA=(shasum -a 256)
else
	die "sha256sum も shasum もありません"
fi

sha256_of() { "${SHA[@]}" "$1" | awk '{print $1}'; }

# Git Bash は /videos のような引数を C:/Program Files/Git/videos に書き換えるので、
# ドライブ側のパスの頭を // にして渡す（docs/commands.md）。JSON の中のパスは / のまま比べる。
case "$(uname -s)" in
MINGW* | MSYS* | CYGWIN*) REMOTE_PREFIX="/" ;;
*) REMOTE_PREFIX="" ;;
esac

remote() { printf '%s%s' "$REMOTE_PREFIX" "$1"; }

# --- 後片付け ---------------------------------------------------------------

# trap から呼ぶ。
# shellcheck disable=SC2329
finish() {
	local code=$?
	# 片付けの途中で、もう一度 Ctrl-C されても案内が消えないよう、INT と TERM を無視する。
	trap - EXIT
	trap '' INT TERM
	# 消すのは、このスクリプトが mktemp -d で作った場所だけ。
	case "$LOCAL_WORK" in
	*/gdrive-cli-e2e.*) [ -d "$LOCAL_WORK" ] && rm -rf -- "$LOCAL_WORK" ;;
	esac
	if [ "$WORK_MADE" = 1 ]; then
		say ""
		say "ドライブに作業用フォルダが残っています: $WORK"
		[ -n "$WORK_LINK" ] && say "  $WORK_LINK"
		say "gdrive-cli には削除がないので、ブラウザで開いてゴミ箱に入れてください。"
	elif [ "$WORK_TRIED" = 1 ]; then
		# 作る途中で止まったり、応答が届かなかったりしても、ドライブにはできていることがある。
		say ""
		say "ドライブに作業用フォルダ $WORK ができているかもしれません。"
		say "ブラウザで確かめて、あればゴミ箱に入れてください。"
	fi
	if [ $((PASS + FAIL)) -gt 0 ]; then
		say ""
		say "通った $PASS 件 / 通らなかった $FAIL 件"
	fi
	exit "$code"
}

LOCAL_WORK="$(mktemp -d "${TMPDIR:-/tmp}/gdrive-cli-e2e.XXXXXX")" || die "手元の作業場所を作れません"
trap finish EXIT
trap 'exit 130' INT TERM

# --- 確かめの道具 -----------------------------------------------------------

# run は gdrive-cli を動かし、CODE・OUT・ERR に結果を入れる。止まらない。
run() {
	CMD="gdrive-cli $*"
	"$BIN" "$@" </dev/null >"$LOCAL_WORK/stdout" 2>"$LOCAL_WORK/stderr"
	CODE=$?
	OUT="$(<"$LOCAL_WORK/stdout")"
	ERR="$(<"$LOCAL_WORK/stderr")"
}

ok() {
	PASS=$((PASS + 1))
	say "✓ $1"
}

# ng は、結果を貼ってもらったときに原因を追えるよう、直前のコマンドと出力の頭も出す。
ng() {
	FAIL=$((FAIL + 1))
	say "✗ $1"
	[ $# -ge 2 ] && say "    理由: $2"
	say "    直前のコマンド: $CMD"
	say "    終了コード: $CODE"
	if [ "$CODE" = 7 ]; then
		say "    （終了コード 7 は、拒否リスト policy.toml で止められたことを表す。gdrive-cli の不具合ではない）"
	fi
	[ -n "$OUT" ] && say "    標準出力: ${OUT:0:300}"
	if [ -n "$ERR" ]; then
		printf '%s\n' "$ERR" | head -n 3 | sed 's/^/    標準エラー: /'
	fi
}

# expect_json <名前> <jq の式> [jq の引数...]: 標準出力に jq -e を当てる。
expect_json() {
	local name=$1 filter=$2
	shift 2
	if printf '%s' "$OUT" | jqx -e "$@" "$filter" >/dev/null 2>&1; then
		ok "$name"
	else
		ng "$name" "jq -e '$filter' が通らない"
	fi
}

# expect_ok <名前> [<jq の式> [jq の引数...]]: 終了コードが 0 で、式があれば標準出力がそれを満たす。
expect_ok() {
	local name=$1
	shift
	if [ "$CODE" != 0 ]; then
		ng "$name" "終了コードが 0 ではない"
	elif [ $# -eq 0 ]; then
		ok "$name"
	else
		expect_json "$name" "$@"
	fi
}

# expect_err_kind <名前> <kind>: 標準エラーの JSON の .error.kind を比べる。
expect_err_kind() {
	if printf '%s' "$ERR" | jqx -e --arg k "$2" '.error.kind == $k' >/dev/null 2>&1; then
		ok "$1"
	else
		ng "$1" "標準エラーの .error.kind が $2 ではない"
	fi
}

# expect_err_shape <名前>: 標準エラーが {"error":{"kind","message","details"}} の形かを確かめる。
expect_err_shape() {
	if printf '%s' "$ERR" | jqx -e '.error | (.kind | type == "string") and (.message | type == "string") and (.details | type == "array")' >/dev/null 2>&1; then
		ok "$1"
	else
		ng "$1" "標準エラーが {\"error\":{\"kind\",\"message\",\"details\"}} の形ではない"
	fi
}

# expect_error <名前> <終了コード> <kind>: 終了コードと、標準エラーの .error.kind の両方を比べる。
expect_error() {
	if [ "$CODE" != "$2" ]; then
		ng "$1" "終了コードが $2 ではない"
	else
		expect_err_kind "$1" "$3"
	fi
}

# expect_stdout_empty <名前>
expect_stdout_empty() {
	if [ -z "$OUT" ]; then ok "$1"; else ng "$1" "標準出力が空ではない"; fi
}

# --- status ------------------------------------------------------------------

say "gdrive-cli: $BIN"
say "手元の作業場所: $LOCAL_WORK"

run status --json
if [ "$CODE" != 0 ] || ! printf '%s' "$OUT" | jqx -e '.loggedIn == true' >/dev/null 2>&1; then
	say "gdrive-cli の status が通らないので、何も作らずに止めます（ログインしていないか、準備が足りません。status の終了コード: ${CODE}）。"
	[ -n "$ERR" ] && printf '%s\n' "$ERR" | head -n 3 | sed 's/^/  /'
	exit 2
fi
say "アカウント: $(printf '%s' "$OUT" | jq_raw '.email // ""')（鍵の置き場所: $(printf '%s' "$OUT" | jq_raw '.keyStore // ""')）"
ok "status: ログイン済みと出る"

# --- 手元の材料 --------------------------------------------------------------

BIG_SIZE=20971520 # 20 MiB。16 MiB ずつの分割送信を 2 回以上起こす大きさ。
mkdir -p "$LOCAL_WORK/src/emptydir"
head -c "$BIG_SIZE" /dev/urandom >"$LOCAL_WORK/src/big.bin"
printf '<!doctype html><title>e2e</title><p>gdrive-cli e2e</p>\n' >"$LOCAL_WORK/src/page.html"
: >"$LOCAL_WORK/src/empty.txt"
printf 'x' >"$LOCAL_WORK/src/.DS_Store"
printf 'single\n' >"$LOCAL_WORK/single.txt"
# Git Bash の ln -s は、既定ではリンクではなく写しを作る。リンクにならなければ消して、その確かめを飛ばす。
HAVE_LINK=0
ln -s page.html "$LOCAL_WORK/src/link.txt" 2>/dev/null
if [ -L "$LOCAL_WORK/src/link.txt" ]; then
	HAVE_LINK=1
else
	rm -f -- "$LOCAL_WORK/src/link.txt"
	say "注意: シンボリックリンクを作れないので、シンボリックリンクを飛ばす確かめは行いません。"
fi

# --- mkdir -------------------------------------------------------------------

W="/e2e-cli-$(date +%Y%m%d-%H%M%S)-$$"
WORK="$W"
say "ドライブの作業用フォルダ: $W"
say ""

WORK_TRIED=1
run mkdir --json "$(remote "$W")"
if [ "$CODE" = 0 ]; then
	WORK_MADE=1
	WORK_LINK="$(printf '%s' "$OUT" | jq_raw '.[0].webViewLink // ""')"
	ok "mkdir: 作業用フォルダを作れる"
else
	ng "mkdir: 作業用フォルダを作れる"
	say "作業用フォルダを作れないので、ここで止めます。"
	exit 1
fi

run mkdir -p --json "$(remote "$W/a/b/c")"
expect_ok "mkdir -p: 途中の階層ごと作り、最後の階層を出す" '.[0].path == $p' --arg p "$W/a/b/c"

run mkdir -p --json "$(remote "$W/a/b/c")"
expect_ok "mkdir -p: 既にあってもエラーにしない"

run mkdir --json "$(remote "$W/a")"
expect_error "mkdir: -p なしで既にあれば 5（conflict）" 5 conflict

run mkdir --json "$(remote "$W/nope/x")"
expect_error "mkdir: 親フォルダがなければ 4（not_found）" 4 not_found

# --- upload ------------------------------------------------------------------

run upload --json "$LOCAL_WORK/src" "$(remote "$W/up")"
expect_error "upload: 送り先がなく -p もなければ 4（not_found）" 4 not_found

say "（20 MiB のファイルを送るので、少し時間がかかります）"
run upload -p --json "$LOCAL_WORK/src" "$(remote "$W/up")"
expect_ok "upload -p: 送り先を作り、フォルダを中身ごと送る（.DS_Store とシンボリックリンクは送らない）" \
	'[.items[].remotePath] | sort == ([$w + "/up/src", $w + "/up/src/big.bin", $w + "/up/src/page.html", $w + "/up/src/empty.txt", $w + "/up/src/emptydir"] | sort)' \
	--arg w "$W"
expect_json "upload -p: どの項目も created で、error がない" \
	'(.items | length > 0) and all(.items[]; .action == "created" and (.error // "") == "")'
if [ "$HAVE_LINK" = 1 ]; then
	expect_json "upload -p: シンボリックリンク（link.txt）を飛ばしたと警告する" \
		'any(.warnings[]?; contains("link.txt"))'
fi

# --- ls ----------------------------------------------------------------------

run ls -R --json "$(remote "$W/up")"
expect_ok "ls -R: 送ったものが、種類と大きさごと見える" \
	'(map(select(.path == $w + "/up/src/big.bin" and .type == "file" and .size == $big)) | length == 1)
	 and (map(select(.path == $w + "/up/src/empty.txt" and .type == "file" and .size == 0)) | length == 1)
	 and (map(select(.path == $w + "/up/src/emptydir" and .type == "folder")) | length == 1)' \
	--arg w "$W" --argjson big "$BIG_SIZE"
expect_json "ls -R: .DS_Store とシンボリックリンクはない" \
	'map(select(.name == ".DS_Store" or .name == "link.txt")) | length == 0'
LS_COUNT="$(printf '%s' "$OUT" | jq_raw 'length' 2>/dev/null)"

run ls -l "$(remote "$W/up/src")"
if [ "$CODE" = 0 ] && printf '%s\n' "$OUT" | grep -q 'big\.bin'; then
	ok "ls -l: 種類・大きさ・更新日時・ID つきで一覧を出す"
else
	ng "ls -l: 種類・大きさ・更新日時・ID つきで一覧を出す" "終了コードが 0 でないか、big.bin が出ていない"
fi

run ls --json "$(remote "$W/none")"
expect_error "ls: ないパスは 4（not_found）" 4 not_found

# --- upload の衝突と飛ばし ---------------------------------------------------

run upload --json "$LOCAL_WORK/src" "$(remote "$W/up")"
expect_error "upload: 同じものをもう一度送ると 5（conflict）" 5 conflict
expect_stdout_empty "upload: 衝突したときは、--json でも標準出力に何も出さない"

run ls -R --json "$(remote "$W/up")"
expect_ok "upload: 衝突したときは何も送らない（ls -R の件数が変わらない）" \
	'length == $n' --argjson n "${LS_COUNT:-0}"

run upload --skip-existing --json "$LOCAL_WORK/src" "$(remote "$W/up")"
expect_ok "upload --skip-existing: ファイルはすべて skipped、フォルダは merged" \
	'(.items | length > 0)
	 and all(.items[] | select(.type == "file"); .action == "skipped")
	 and all(.items[] | select(.type == "folder"); .action == "merged")'

# --- mv ----------------------------------------------------------------------

run mv --json "$(remote "$W/up/src/page.html")" "$(remote "$W/up/src/page-v1.html")"
expect_ok "mv: 名前を変えられる" '.[0].to == $to' --arg to "$W/up/src/page-v1.html"

run ls --json "$(remote "$W/up/src")"
expect_ok "mv: 名前を変えたあと、page-v1.html があり page.html がない" \
	'any(.[]; .name == "page-v1.html") and all(.[]; .name != "page.html")'

run mkdir --json "$(remote "$W/archive")"
expect_ok "mkdir: 移し先のフォルダを作れる"

run mv --json "$(remote "$W/up/src/page-v1.html")" "$(remote "$W/up/src/empty.txt")" "$(remote "$W/archive")"
expect_ok "mv: いくつかをまとめてフォルダへ移せる" 'length == 2'

run ls --json "$(remote "$W/archive")"
expect_ok "mv: 移した先に 2 つともある" '([.[].name] | sort) == ["empty.txt", "page-v1.html"]'

run upload --json "$LOCAL_WORK/single.txt" "$(remote "$W/archive")"
expect_ok "upload: 1 つのファイルを送れる（移し先へ）"
run upload --json "$LOCAL_WORK/single.txt" "$(remote "$W/up/src")"
expect_ok "upload: 1 つのファイルを送れる（移し元へ）"

run mv --json "$(remote "$W/up/src/single.txt")" "$(remote "$W/archive")"
expect_error "mv: 移し先に同じ名前があれば 5（conflict）" 5 conflict

# --- download ----------------------------------------------------------------

run download -p --json "$(remote "$W/up/src")" "$LOCAL_WORK/down/x"
expect_ok "download -p: 保存先を作り、フォルダを中身ごと受け取る" \
	'(.items | length > 0) and all(.items[]; (.error // "") == "")'
if [ -f "$LOCAL_WORK/down/x/src/big.bin" ] &&
	[ "$(sha256_of "$LOCAL_WORK/down/x/src/big.bin")" = "$(sha256_of "$LOCAL_WORK/src/big.bin")" ]; then
	ok "download: 受け取った big.bin（20 MiB）の sha256 が送ったものと同じ"
else
	ng "download: 受け取った big.bin（20 MiB）の sha256 が送ったものと同じ" "ファイルがないか、sha256 が違う"
fi
if [ -d "$LOCAL_WORK/down/x/src/emptydir" ]; then
	ok "download: 空のフォルダも受け取る"
else
	ng "download: 空のフォルダも受け取る" "$LOCAL_WORK/down/x/src/emptydir がない"
fi

run download --json "$(remote "$W/up/src")" "$LOCAL_WORK/down/x"
expect_error "download: 同じものをもう一度受け取ると 5（conflict）" 5 conflict

run download --overwrite --json "$(remote "$W/up/src")" "$LOCAL_WORK/down/x"
expect_ok "download --overwrite: ファイルはすべて overwritten" \
	'([.items[] | select(.type == "file")] | length > 0)
	 and all(.items[] | select(.type == "file"); .action == "overwritten")'

run download --skip-existing --json "$(remote "$W/up/src")" "$LOCAL_WORK/down/x"
expect_ok "download --skip-existing: ファイルはすべて skipped" \
	'([.items[] | select(.type == "file")] | length > 0)
	 and all(.items[] | select(.type == "file"); .action == "skipped")'

run download --overwrite --skip-existing --json "$(remote "$W/up/src")" "$LOCAL_WORK/down/x"
expect_error "download: --overwrite と --skip-existing を同時に付けると 2（usage）" 2 usage

# --- 引数の誤り --------------------------------------------------------------

run upload --json "$LOCAL_WORK/single.txt"
expect_error "引数の誤り: upload に引数が 1 つだけなら 2（usage）" 2 usage
expect_stdout_empty "引数の誤り: --json のエラーは標準エラーに出て、標準出力は空"
expect_err_shape "引数の誤り: --json のエラーは {\"error\":{\"kind\",\"message\",\"details\"}} の形"

[ "$FAIL" -eq 0 ] || exit 1
exit 0
