VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
BINARY := git-blamebot

.PHONY: build test vet clean install

build:
	go build $(LDFLAGS) -o dist/$(BINARY) .

test:
	go test ./... -v -count=1

vet:
	go vet ./...

clean:
	rm -rf dist/

install: build
	mkdir -p $(HOME)/.local/bin
	cp dist/$(BINARY) $(HOME)/.local/bin/$(BINARY)
	$(HOME)/.local/bin/$(BINARY) enable --global
