# mischief — build targets (M1 chassis, MSF-014)
#
# `make bin` is the stamped build: CGO_ENABLED=0 (single static binary, the
# PRD §10 M1 shape), version stamped with the git sha via -X main.gitSha.
# A dirty tree still stamps the REAL head sha (the binary reports what it
# was built FROM, not what the tree wishes it were).

GO ?= go
SHA ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BIN := bin/mischief

LDFLAGS := -X main.gitSha=$(SHA)

.PHONY: bin test vet build clean

bin:
	CGO_ENABLED=0 $(GO) build -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/mischief

build:
	CGO_ENABLED=0 $(GO) build ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

clean:
	rm -rf bin/
