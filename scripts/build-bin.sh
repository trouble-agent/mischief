#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════
# build-bin.sh — `make bin`: the stamped, self-contained build.
#
# MISCHIEF-DF-69: this is the documented fresh-machine build (README "Option B",
# CONTRIBUTING "Build and test"). It no longer inherits an ambient toolchain:
# scripts/ensure-toolchain.sh resolves the toolchain go.mod DECLARES and this
# script builds through it with GOTOOLCHAIN=local, so nothing can silently
# switch paths mid-build (the failure mode was go's own PATH toolchain switch
# resolving a golang.org/dl wrapper under an isolated $HOME and dying before
# compilation). Then it CHECKS the artifact it just produced: an executable
# bin/mischief whose stamp is the tree's real HEAD sha — a build that leaves an
# unstamped ("unknown") binary is a failure here, not a surprise later.
#
# Environment: SHA=<sha> overrides the stamp (make passes its own), BIN=<path>
# overrides the output (default bin/mischief), GO=<cmd> overrides the starting
# go (resolved via ensure-toolchain.sh).
# ═══════════════════════════════════════════════════════════════════════════
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
out="${BIN:-bin/mischief}"
sha="${SHA:-$(git -C "$root" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)}"

cd "$root"

# Resolve FIRST — a named refusal must land before anything is written.
go_bin=$(./scripts/ensure-toolchain.sh --print) || exit $?

# BIN may be absolute (a probe target) or repo-relative (the default).
case "$out" in
  /*) binary="$out" ;;
  *) binary="$root/$out" ;;
esac

mkdir -p "$(dirname "$binary")"
CGO_ENABLED=0 GOTOOLCHAIN=local "$go_bin" build -ldflags "-X main.gitSha=$sha" -o "$binary" ./cmd/mischief

[ -x "$binary" ] || { echo "bin: '$out' was not produced as an executable" >&2; exit 1; }

got=$("$binary" version 2>&1) || { echo "bin: '$out version' exited non-zero: $got" >&2; exit 1; }
if [ "$sha" != "unknown" ] && [ "$got" != "mischief $sha" ]; then
  echo "bin: '$out' reports '$got', expected 'mischief $sha' — the stamp did not land" >&2
  exit 1
fi

echo "bin: $out ($got, toolchain $(GOTOOLCHAIN=local "$go_bin" env GOVERSION))"
