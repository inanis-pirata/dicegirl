# dicegirl Makefile
BINARY  := dicegirl
GO      := go
PKGS    := ./...

.PHONY: all build test race vet fmt fmt-check package clean

all: build

build:
	$(GO) build -o bin/$(BINARY) ./cmd/dicegirl

test:
	$(GO) test $(PKGS)

race:
	$(GO) test -race $(PKGS)

vet:
	$(GO) vet $(PKGS)

fmt:
	$(GO) fmt $(PKGS)

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)

package:
	$(GO) run ./scripts/package

clean:
	rm -rf bin dist
