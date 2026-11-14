.PHONY: help build install lint fmt test

# make install の入れ先。例: make install BINDIR=/usr/local/bin
BINDIR ?= $(HOME)/.local/bin

# macOS では hardened runtime を付ける（証明書の要らない ad-hoc 署名）。付いていない gdrive-cli はキーチェーンを使わない。
ifeq ($(shell uname -s),Darwin)
SIGN = codesign --force -s - -o runtime
else
SIGN = true
endif

help:
	@echo "gdrive-cli - Makefile targets"
	@echo ""
	@echo "  make build                  実行ファイルをリポジトリの直下に作る。macOS では hardened runtime を付ける"
	@echo "  make install                ~/.local/bin に入れる。macOS では hardened runtime を付ける（BINDIR で入れ先を変えられる）"
	@echo "  make lint                   Go の lint (golangci-lint run ./...)"
	@echo "  make fmt                    Go の整形 (golangci-lint fmt)"
	@echo "  make test                   Go のテスト実行 (go test ./...)"

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

lint:
	golangci-lint run ./...

fmt:
	golangci-lint fmt

test:
	go test ./...
