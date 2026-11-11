.PHONY: help lint fmt test tf-check

help:
	@echo "gdrive-cli - Makefile targets"
	@echo ""
	@echo "  make lint                   Go の lint (golangci-lint run ./...)"
	@echo "  make fmt                    Go の整形 (golangci-lint fmt)"
	@echo "  make test                   Go のテスト実行 (go test ./...)"
	@echo "  make tf-check               infra/ の Terraform の確認 (fmt -check と validate)"

lint:
	golangci-lint run ./...

fmt:
	golangci-lint fmt

test:
	go test ./...

tf-check:
	terraform -chdir=infra fmt -check -recursive && terraform -chdir=infra init -backend=false -input=false && terraform -chdir=infra validate
