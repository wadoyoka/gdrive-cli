.PHONY: help lint fmt test

help:
	@echo "gdrive-cli - Makefile targets"
	@echo ""
	@echo "  make lint                   Go の lint (golangci-lint run ./...)"
	@echo "  make fmt                    Go の整形 (golangci-lint fmt)"
	@echo "  make test                   Go のテスト実行 (go test ./...)"

lint:
	golangci-lint run ./...

fmt:
	golangci-lint fmt

test:
	go test ./...
