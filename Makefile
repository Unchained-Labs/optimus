PREFIX ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/wardn/optimus/internal/cli.Version=$(VERSION)

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
