.PHONY: help build install lint fmt test

# OS で分ける。$(OS) は Windows の環境変数で、PowerShell・cmd・Git Bash のどれから make を実行しても入っている。
# Windows のレシピは go だけを呼ぶ（Scoop の make は sh.exe がないと cmd.exe でレシピを動かすため）。uname は Windows 側では呼ばない。
ifeq ($(OS),Windows_NT)
# make install の入れ先。例: make install BINDIR=C:/tools
BINDIR ?= $(USERPROFILE)/.local/bin

HELP_BUILD = 実行ファイル gdrive-cli.exe をリポジトリの直下に作る。署名はしない
HELP_INSTALL = %USERPROFILE%\.local\bin に入れる。署名はしない（BINDIR で入れ先を変えられる）

# 出力先の gdrive-cli.exe は .gitignore で除外してある。
# 動いている gdrive-cli.exe は上書きできない。make install が失敗したら gdrive-cli を止めてからやり直す。
build:
	go build -o gdrive-cli.exe ./cmd/gdrive-cli

install:
	go build -o "$(BINDIR)/gdrive-cli.exe" ./cmd/gdrive-cli
else
# make install の入れ先。例: make install BINDIR=/usr/local/bin
BINDIR ?= $(HOME)/.local/bin

HELP_BUILD = 実行ファイルをリポジトリの直下に作る。macOS では hardened runtime を付ける
HELP_INSTALL = ~/.local/bin に入れる。macOS では hardened runtime を付ける（BINDIR で入れ先を変えられる）

# macOS では hardened runtime を付ける（証明書の要らない ad-hoc 署名）。付いていない gdrive-cli はキーチェーンを使わない。
ifeq ($(shell uname -s),Darwin)
SIGN = codesign --force -s - -o runtime
else
SIGN = true
endif

# 出力先の gdrive-cli は .gitignore で除外してある。
# 一時ファイルに作って署名してから mv で置き換える。同じファイルを上書きすると、
# macOS が前の署名を覚えていて、実行したときに止められることがあるため。
build:
	go build -o gdrive-cli.tmp ./cmd/gdrive-cli
	$(SIGN) gdrive-cli.tmp
	mv -f gdrive-cli.tmp gdrive-cli

install:
	mkdir -p "$(BINDIR)"
	go build -o "$(BINDIR)/gdrive-cli.tmp" ./cmd/gdrive-cli
	$(SIGN) "$(BINDIR)/gdrive-cli.tmp"
	mv -f "$(BINDIR)/gdrive-cli.tmp" "$(BINDIR)/gdrive-cli"
endif

# $(info ...) で表示する。cmd の echo は " をそのまま表示するため、echo は使わない。
# $(info) は行頭の空白を落とすので、字下げは SP で入れる。
# 最後の @cd . は、make 3.81 が「Nothing to be done」を出さないための何もしない行（cmd.exe でも sh でも同じに動く）。
SP := $(subst x, ,x)

help:
	$(info gdrive-cli - Makefile targets)
	$(info )
	$(info $(SP)$(SP)make build                  $(HELP_BUILD))
	$(info $(SP)$(SP)make install                $(HELP_INSTALL))
	$(info $(SP)$(SP)make lint                   Go の lint (golangci-lint run ./...))
	$(info $(SP)$(SP)make fmt                    Go の整形 (golangci-lint fmt))
	$(info $(SP)$(SP)make test                   Go のテスト実行 (go test ./...))
	@cd .

lint:
	golangci-lint run ./...

fmt:
	golangci-lint fmt

test:
	go test ./...
