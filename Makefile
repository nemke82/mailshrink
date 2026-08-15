BINARY     := mailshrink
MODULE     := github.com/nemke82/mailshrink

# CalVer: YYYY.MM.PATCH — override with VERSION=2025.08.1 if needed.
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || date +%Y.%m.0-dev)
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION := $(shell go version | awk '{print $$3}')
LDFLAGS    := -s -w \
	-X '$(MODULE)/cmd.Version=$(VERSION)' \
	-X '$(MODULE)/cmd.BuildDate=$(BUILD_DATE)' \
	-X '$(MODULE)/cmd.GoVersion=$(GO_VERSION)'

.PHONY: build test lint clean release snapshot

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test ./... -v -race -count=1

lint:
	go vet ./...

clean:
	rm -f $(BINARY)
	rm -rf dist/

release: clean
	mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 .
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 .
	@echo "Binaries built in dist/"
	@ls -lh dist/

snapshot:
	goreleaser release --snapshot --clean
