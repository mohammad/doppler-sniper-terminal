.PHONY: deps run ui build tidy

deps:
	@command -v go >/dev/null || (echo "Missing dependency: go" && exit 1)
	@command -v rg >/dev/null || (echo "Missing dependency: rg (ripgrep). Install it first: https://github.com/BurntSushi/ripgrep#installation" && exit 1)

run: ui

ui:
	@$(MAKE) deps
	go run ./cmd/server

build:
	@$(MAKE) deps
	go build -o doppler-sniper ./cmd/server

tidy:
	go mod tidy
