PREFIX ?= /usr/local
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/seinsdieum/also/internal/also.Commit=$(COMMIT) -X github.com/seinsdieum/also/internal/also.BuildDate=$(DATE)

.PHONY: build test vet check smoke install clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/also .

test:
	go test ./...

vet:
	go vet ./...

check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test ./...

smoke: build
	./test/e2e/harness-smoke.sh

install: build
	install -d $(PREFIX)/bin
	install -m 0755 bin/also $(PREFIX)/bin/also

clean:
	rm -rf bin
