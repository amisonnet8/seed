# Seed: .seedソースをAMIVM-IR経由でGoにコンパイルする言語処理系

BINARY := seed
PKG    := ./cmd/seed
GO     := go

.PHONY: all build install test test-examples fmt vet tidy clean help

all: build ## デフォルトターゲット(ビルドのみ)

build: ## seedバイナリをビルドする
	$(GO) build -o $(BINARY) $(PKG)

install: ## seedバイナリをGOBIN($GOPATH/bin)へインストールする
	$(GO) install $(PKG)

test: ## go testで全パッケージのユニットテストを実行する
	$(GO) test ./...

test-examples: build ## examples/配下の全.seedファイルをamivm→go buildまで通し実行する(amivmがPATHにある前提)
	@set -e; \
	tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' EXIT; \
	for src in examples/*.seed; do \
		name=$$(basename "$$src" .seed); \
		echo "== $$src =="; \
		./$(BINARY) build -o "$$tmp/$$name" "$$src"; \
		"$$tmp/$$name"; \
	done

fmt: ## *.goをgoimportsで整形する
	goimports -w .

vet: ## go vetで静的検査する
	$(GO) vet ./...

tidy: ## go.mod/go.sumを整理する
	$(GO) mod tidy

clean: ## ビルド成果物を削除する
	rm -f $(BINARY)

help: ## 使えるターゲット一覧を表示する
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}'
