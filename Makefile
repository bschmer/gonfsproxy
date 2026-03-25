GO ?= go
BINARY ?= nfsproxy
CMD_PKG ?= ./cmd/nfsproxy
GOCACHE ?= /tmp/go-build-cache

.PHONY: all build test vet fmt fmt-check tidy clean ci

all: build

build:
	GOCACHE=$(GOCACHE) $(GO) build -o $(BINARY) $(CMD_PKG)

test:
	GOCACHE=$(GOCACHE) $(GO) test ./...

vet:
	GOCACHE=$(GOCACHE) $(GO) vet ./...

fmt:
	@files=$$(find . -type f -name '*.go' -not -path './vendor/*'); \
	if [ -n "$$files" ]; then gofmt -w $$files; fi

fmt-check:
	@files=$$(find . -type f -name '*.go' -not -path './vendor/*'); \
	if [ -n "$$files" ]; then \
		unformatted=$$(gofmt -l $$files); \
		if [ -n "$$unformatted" ]; then \
			echo "unformatted Go files:"; \
			echo "$$unformatted"; \
			exit 1; \
		fi; \
	fi

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BINARY)

ci: fmt-check vet test build
