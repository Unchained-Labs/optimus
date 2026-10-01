PREFIX ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/Unchained-Labs/optimus/internal/cli.Version=$(VERSION)

.PHONY: lint
lint:
	gofmt -l . | (! grep .)
	go vet ./...
	shellcheck install.sh demo/setup.sh demo/record.sh

.PHONY: build install test vet clean

build:
	go build -ldflags '$(LDFLAGS)' -o bin/optimus ./cmd/optimus

install: build
	install -Dm755 bin/optimus $(PREFIX)/bin/optimus

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf bin

.PHONY: demo
demo:
	demo/record.sh

.PHONY: docs-gen docs docs-serve
docs-gen: build
	./bin/optimus help > docs/reference/cli-usage.txt
	mkdir -p docs/assets/demo && cp demo/optimus.mp4 demo/optimus.gif docs/assets/demo/

docs: docs-gen
	mkdocs build --strict

docs-serve: docs-gen
	mkdocs serve
