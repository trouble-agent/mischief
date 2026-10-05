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

# Release flow (MSF-018): `make tags` + push cuts nothing by itself — it
# arms the tag-triggered release workflow (.github/workflows/release.yml),
# which reruns every gate and only then goreleasers. `make dist` is the
# local dry-run of exactly that release shape (docs/RELEASE.md). `make
# release-dry` is the config-only check (no build).
DIST := dist/release

.PHONY: bin test vet build clean shim dist tags release-dry

bin:
	CGO_ENABLED=0 $(GO) build -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/mischief

build:
	CGO_ENABLED=0 $(GO) build ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

# shim: the per-arch shim .so from the DECIDED path (MSF-018) — the same
# ShimSourceC the runtime compiles at arm time, via system cc (amd64) and
# gcc-aarch64-linux-gnu (arm64); zig is not used. Add --smoke to land the
# fault live per arch (amd64 native, arm64 under qemu-user).
shim:
	./tools/shim-build.sh

# dist: a LOCAL dry-run of the release: gates first (build/vet/selftest/
# one battery cell), then goreleaser with --snapshot --clean. Produces
# dist/release/*.tar.gz, each carrying its arch's shim .so (mode 0755),
# plus checksums — and stamps main.gitSha with goreleaser's synthetic
# v0.0.0 (a snapshot must never wear a tag it was not cut from; a real
# tag release stamps the literal tag). No GitHub call, no tag required.
# Requires goreleaser on PATH.
dist: bin shim
	CGO_ENABLED=0 goreleaser release --snapshot --clean
	@echo "dist: snapshot release under $(DIST)/ (snapshot stamp, never a real tag)"

# release-dry: config-only validation (goreleaser check) — no build.
release-dry:
	goreleaser check

# tags: ARM a release (docs/RELEASE.md owns the flow). Lists the annotated
# tag commands; the foreman cuts after acceptance — a worker does not tag.
tags:
	@echo "release flow (docs/RELEASE.md):"
	@echo "  1. gates green on main (CI, incl. shim + battery + selftest)"
	@echo "  2. git tag -a <vX.Y.Z> -m \"mischief <vX.Y.Z>\" && git push origin <vX.Y.Z>"
	@echo "  3. .github/workflows/release.yml reruns every gate, then goreleaser"

clean:
	rm -rf bin/
