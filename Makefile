BINARY  := mdv
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# CGO is off everywhere so the result is a static binary that runs on any
# distribution regardless of its libc.
export CGO_ENABLED = 0

.PHONY: all build install test race vet fmt lint clean dist demo

all: build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/mdv

install:
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/mdv

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
