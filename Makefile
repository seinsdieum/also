PREFIX ?= /usr/local

.PHONY: build test vet check install clean

build:
	go build -trimpath -o bin/also .

test:
	go test ./...

vet:
	go vet ./...

check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test ./...

install: build
	install -d $(PREFIX)/bin
	install -m 0755 bin/also $(PREFIX)/bin/also

clean:
	rm -rf bin
