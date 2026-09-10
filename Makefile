BINARY  := mdv
VERSION ?= $(shell (git describe --tags --always --dirty 2>/dev/null || echo dev) | sed 's/^v//')
LDFLAGS := -s -w -X main.version=$(VERSION)

# Install locations. The defaults follow the usual convention, and packagers
# can stage into a build root with DESTDIR:
#   make install PREFIX=$HOME/.local
#   make install DESTDIR=/tmp/root PREFIX=/usr
PREFIX  ?= /usr/local
BINDIR  ?= $(PREFIX)/bin
MANDIR  ?= $(PREFIX)/share/man

# CGO is off everywhere so the result is a static binary that runs on any
# distribution regardless of its libc.
export CGO_ENABLED = 0

.PHONY: all build install uninstall test race vet fmt lint golden clean dist demo man snapshot release-check

all: build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/mdv

# mkdir and install -m rather than install -D, which BSD install on macOS
# does not have.
install: build
	mkdir -p '$(DESTDIR)$(BINDIR)' '$(DESTDIR)$(MANDIR)/man1'
	install -m 755 bin/$(BINARY) '$(DESTDIR)$(BINDIR)/$(BINARY)'
	install -m 644 docs/$(BINARY).1 '$(DESTDIR)$(MANDIR)/man1/$(BINARY).1'

uninstall:
	rm -f '$(DESTDIR)$(BINDIR)/$(BINARY)' '$(DESTDIR)$(MANDIR)/man1/$(BINARY).1'

test:
	go test ./...

race:
	CGO_ENABLED=1 go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint: fmt vet test

# Regenerate the golden files after an intentional rendering change.
golden:
	go test ./internal/render -update

clean:
	rm -rf bin dist

demo: build
	./bin/$(BINARY) internal/render/testdata/sample.md

# Preview the man page without installing it.
man:
	man -l docs/$(BINARY).1

# Cross-compiled release binaries.
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

dist:
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "  $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' \
			-o dist/$(BINARY)-$$os-$$arch ./cmd/mdv || exit 1; \
	done

# Release archives and Linux packages, built the way the release workflow
# builds them but without publishing anything. Needs goreleaser.
snapshot:
	goreleaser release --snapshot --clean

release-check:
	goreleaser check
