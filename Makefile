BINARY  := nanny
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install uninstall run demo test vet fmt lint snapshot clean

build: ## build ./bin/nanny
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/nanny

install: build ## install to $(PREFIX)/bin (default ~/.local/bin)
	install -Dm755 bin/$(BINARY) $(PREFIX)/bin/$(BINARY)
	@echo "installed $(PREFIX)/bin/$(BINARY) — add 'eval \"\$$(nanny shell-init zsh)\"' to your shell rc for sp/spr"

uninstall:
	rm -f $(PREFIX)/bin/$(BINARY)

run: build
	./bin/$(BINARY)

demo: build ## run against a generated fake $HOME
	./bin/$(BINARY) demo

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

lint: vet
	@test -z "$$(gofmt -s -l .)" || (gofmt -s -l . && echo 'run make fmt' && exit 1)

snapshot: ## local cross-compiled release via goreleaser
	goreleaser release --snapshot --clean

clean:
	rm -rf bin dist
