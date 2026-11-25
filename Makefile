# mischief — build targets (M1 chassis, MSF-014)
#
# `make bin` is the stamped build: CGO_ENABLED=0 (single static binary, the
# PRD §10 M1 shape), version stamped with the git sha via -X main.gitSha.
# A dirty tree still stamps the REAL head sha (the binary reports what it
# was built FROM, not what the tree wishes it were).

GO ?= go
SHA ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BIN := bin/mischief

# Release flow (MSF-018): `make tags` + push cuts nothing by itself — it
# arms the tag-triggered release workflow (.github/workflows/release.yml),
# which reruns every gate and only then goreleasers. `make dist` is the
# local dry-run of exactly that release shape (docs/RELEASE.md). `make
# release-dry` is the config-only check (no build).
DIST := dist/release

.PHONY: bin test vet build clean shim dist tags release-dry row-drift docs-check \
        toolchain-check artifact-check

# bin (MISCHIEF-DF-69): the documented fresh-machine build. scripts/build-bin.sh
# resolves the toolchain go.mod DECLARES (via scripts/ensure-toolchain.sh, which
# obtains it from the module cache or refuses with a NAMED requirement — never a
# silent wrong-toolchain build), builds with GOTOOLCHAIN=local, and verifies the
# artifact it produced is executable and stamped with the tree's HEAD sha.
bin:
	@SHA=$(SHA) BIN=$(BIN) ./scripts/build-bin.sh

# toolchain-check: read-only guard for the build's toolchain requirement. It
# resolves go.mod's declaration the same way `make bin` does (downloading the
# declared toolchain into the module cache when the local go is older) and fails
# with the same named refusal — so a broken/absent toolchain is caught by a
# cheap gate instead of mid-release.
toolchain-check:
	@./scripts/ensure-toolchain.sh --quiet

# artifact-check: the product-aware install artifact assertion
# (scripts/install-check.sh) — a stamped bin/mischief whose `version --json`
# says {"version":<HEAD sha>,"tool":"mischief"}. This is the check the bunker
# install leg runs (scripts/bunker-qa-mischief.sh wires it in).
artifact-check: bin
	@./scripts/install-check.sh

build:
	CGO_ENABLED=0 $(GO) build ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

# row-drift (MISCHIEF-DF-63): the repo's current-behaviour prose (skills/*.md,
# README) must not cite a CLOSED board row as if its defect were still live.
# Resolves MISCHIEF-DF-<n>/MSF-<n> references against
# .coding-hermes/board/tasks.jsonl; docs/, probe/ and the board itself are
# exempt (dated history may record a row as open at the time of the run).
row-drift:
	python3 tools/check_row_drift.py

# docs-check: bounded, read-only documentation freshness gate. It checks the
# generated HTML twins and current-status references without modifying files,
# plus the CLI-table gate (DOC-35, tools/check_cli_table.py): shipped verbs
# and per-verb flags of the built binary vs skills/mischief-usage/SKILL.md.
# `bin` is a prerequisite because the checker drives the real binary (its
# --help / refusal output is the source of truth; without it the checker
# errors with a "run make bin first" hint).
docs-check: bin row-drift
	python3 tools/gen-docs.py --check
	python3 tools/check_cli_table.py

# shim: the per-arch shim .so from the DECIDED path (MSF-018) — the same
# ShimSourceC the runtime compiles at arm time, via system cc (amd64) and
# gcc-aarch64-linux-gnu (arm64); zig is not used. Add --smoke to land the
# fault live per arch (amd64 native, arm64 under qemu-user).
shim:
	./tools/shim-build.sh

# dist: a LOCAL dry-run of the release: gates first (build/vet/selftest/
# one battery cell), then goreleaser with --snapshot --clean. Produces
# dist/release/*.tar.gz, each carrying its arch's shim .so (mode 0755),
# plus checksums — and stamps main.gitSha with the snapshot version
# 0.0.0-SNAPSHOT-<shortsha> from .goreleaser.yml's snapshot.version_template
# (a snapshot must never wear a tag it was not cut from: the measured default
# would borrow the previous tag, see RELEASE-015; a real tag release stamps
# the literal tag). No GitHub call, no tag required.
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
