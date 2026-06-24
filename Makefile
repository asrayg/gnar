BIN := gnar
PREFIX ?= /usr/local
PKG := github.com/asrayg/gnar/internal
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w \
	-X $(PKG)/cli.Version=$(VERSION) \
	-X $(PKG)/mcpserver.Version=$(VERSION)

.PHONY: build test race vet fmt install clean run cover

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN) .

test:
	go test ./...

race:
	go test -race ./...

cover:
	go test -cover ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

install: build
	install -m 0755 $(BIN) $(PREFIX)/bin/$(BIN)

clean:
	rm -f $(BIN)

# Run the MCP server against a scratch home (for manual testing)
run:
	GNAR_HOME=$(CURDIR)/.gnar-dev go run . serve
