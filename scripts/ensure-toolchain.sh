#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════
# ensure-toolchain.sh — resolve a Go toolchain that SATISFIES go.mod.
#
# Why this exists (MISCHIEF-DF-69): `make bin` inherited whatever the ambient
# $HOME happened to provide. go.mod declares `toolchain go1.26.9`, and when the
# `go` on PATH is older the go command switches toolchains. Under an isolated
# $HOME that switch had nothing to switch to — a golang.org/dl wrapper named
# `go1.26.9` on PATH won the PATH lookup (cmd/go/internal/toolchain/select.go:
# "Look in PATH for the toolchain before we download one") and refused with
# "go1.26.9: not downloaded. Run 'go1.26.9 download' to install to
# $HOME/sdk/go1.26.9". The documented fresh-machine build therefore died BEFORE
# compiling, exit 2, purely because the build leaned on the operator's $HOME.
#
# This resolver makes the build path self-contained and honest:
#   1. a `go` that already satisfies the declaration is used as-is;
#   2. otherwise the DECLARED toolchain is obtained from the module cache —
#      exactly the golang.org/toolchain module the go command's own
#      GOTOOLCHAIN=auto fetch installs — honouring the caller's GOMODCACHE, so
#      a fresh $HOME gets it under that $HOME;
#   3. if neither is possible (offline, no cache, too-old go) it exits 2 with a
#      NAMED requirement: what go.mod declares, what was found, and the exact
#      commands that fix it.
# It never falls back to a toolchain older than the declaration: a silent
# wrong-toolchain build is the defect, not the fallback.
#
# Usage: scripts/ensure-toolchain.sh [--print|--quiet] [--go CMD]
#   --print   print the resolved go binary path on stdout (default); the
#             rationale goes to stderr so `$(...)` capture stays clean
#   --quiet   print nothing on stdout (rationale still on stderr)
#   --go CMD  the go command to start from (default: $GO, else `go` on PATH)
#
# Exit: 0 = resolved · 2 = named refusal (nothing built).
# ═══════════════════════════════════════════════════════════════════════════
set -uo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
gomod="$root/go.mod"

mode=print
go_cmd="${GO:-go}"

while [ $# -gt 0 ]; do
  case "$1" in
    --print) mode=print ;;
    --quiet) mode=quiet ;;
    --go)
      shift
      [ $# -gt 0 ] || { echo "ensure-toolchain: --go needs a command" >&2; exit 2; }
      go_cmd="$1"
      ;;
    -h|--help)
      sed -n '2,32p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "ensure-toolchain: unknown argument '$1' (supported: --print, --quiet, --go CMD)" >&2
      exit 2
      ;;
  esac
  shift
done

[ -f "$gomod" ] || { echo "ensure-toolchain: no go.mod at $gomod — run from a mischief checkout" >&2; exit 2; }

# ─── the declaration ────────────────────────────────────────────────────────
# `toolchain goX.Y.Z` is the build's declared toolchain; the `go X.Y` line is
# the language floor. A module with no toolchain line only needs the floor.
declared_toolchain=$(sed -nE 's/^toolchain[[:space:]]+go([0-9][0-9A-Za-z.-]*).*/\1/p' "$gomod" | head -1)
language_floor=$(sed -nE 's/^go[[:space:]]+([0-9][0-9A-Za-z.-]*).*/\1/p' "$gomod" | head -1)
req="${declared_toolchain:-$language_floor}"
declared_by="toolchain go$declared_toolchain"
[ -n "$declared_toolchain" ] || declared_by="go $language_floor (language floor; no toolchain line)"

# ─── version comparison on dotted numeric components ────────────────────────
# A pre-release suffix (1.27.0-rc.1) compares on its numeric part only: an rc of
# the declared version is "not older" and the build proceeds; the go command
# itself remains the authority on language-version compatibility.
ver_ge() { # ver_ge A B → 0 when A >= B
  local a="${1%%-*}" b="${2%%-*}" i ai bi
  local -a A B
  IFS=. read -r -a A <<<"$a"
  IFS=. read -r -a B <<<"$b"
  for i in 0 1 2; do
    ai="${A[i]:-0}"
    bi="${B[i]:-0}"
    [[ $ai =~ ^[0-9]+$ ]] || ai=0
    [[ $bi =~ ^[0-9]+$ ]] || bi=0
    if ((10#$ai > 10#$bi)); then return 0; fi
    if ((10#$ai < 10#$bi)); then return 1; fi
  done
  return 0
}

refuse() { # refuse <why>
  local found
  if [ -n "${local_version:-}" ]; then found="$go_cmd (go$local_version)"; else found="$go_cmd (not runnable)"; fi
  {
    echo "mischief build: Go $req is REQUIRED but could not be obtained."
    echo "  declared by:  go.mod ($declared_by)"
    echo "  found:        $found"
    echo "  reason:       $1"
    echo "What to do — any one of these:"
    echo "  · install Go $req or newer:      https://go.dev/dl/  (or your package manager)"
    echo "  · point the build at it:         GO=/path/to/go$req make bin"
    echo "  · pre-download it (needs net):   go install golang.org/dl/go$req@latest && go$req download"
    echo "Nothing was built in this run: an unstamped/wrong-toolchain binary would be worse than this error."
    [ -f "$root/bin/mischief" ] && echo "(the existing bin/mischief was left untouched — it was NOT rebuilt here)"
  } >&2
  exit 2
}

# ─── 1 · probe the starting go WITHOUT letting it switch toolchains ─────────
# GOTOOLCHAIN=local pins the probe to the binary being probed; without it the
# very act of asking for a version can trigger the broken PATH switch above.
if ! command -v "$go_cmd" >/dev/null 2>&1; then
  local_version=""
  refuse "no usable 'go' command (looked for '$go_cmd' on PATH and as a path)"
fi

local_version=$(GOTOOLCHAIN=local GOWORK=off "$go_cmd" env GOVERSION 2>/dev/null)
local_version="${local_version#go}"
if [ -z "$local_version" ]; then
  refuse "'$go_cmd env GOVERSION' produced nothing — the go command is not runnable"
fi

[ -n "$req" ] || req="$local_version"

if ver_ge "$local_version" "$req"; then
  echo "toolchain: go$local_version via '$go_cmd' (satisfies go.mod's $declared_by)" >&2
  resolved="$go_cmd"
else
  # ─── 2 · obtain the DECLARED toolchain from the module cache ─────────────
  goos=$(GOTOOLCHAIN=local GOWORK=off "$go_cmd" env GOOS 2>/dev/null)
  goarch=$(GOTOOLCHAIN=local GOWORK=off "$go_cmd" env GOARCH 2>/dev/null)
  modcache=$(GOTOOLCHAIN=local GOWORK=off "$go_cmd" env GOMODCACHE 2>/dev/null)
  [ -n "$goos" ] && [ -n "$goarch" ] && [ -n "$modcache" ] ||
    refuse "could not read GOOS/GOARCH/GOMODCACHE from '$go_cmd'"

  tc_mod="golang.org/toolchain@v0.0.1-go$req.$goos-$goarch"
  tc_dir="$modcache/$tc_mod"

  if [ ! -x "$tc_dir/bin/go" ]; then
    # Toolchain modules are a Go 1.21+ mechanism; an older go cannot fetch one.
    if ! ver_ge "$local_version" "1.21"; then
      refuse "the go found (go$local_version) predates toolchain modules (Go 1.21+), so it cannot fetch go$req"
    fi
    scratch=$(mktemp -d "${TMPDIR:-/tmp}/mischief-toolchain.XXXXXX") || \
      refuse "could not create a scratch directory under ${TMPDIR:-/tmp}"
    dl_log="$scratch/download.log"
    # Run OUTSIDE any module (a scratch cwd) so the fetch cannot touch go.mod or
    # go.sum, and honour the caller's GOMODCACHE so an isolated $HOME stays
    # isolated — this is the same module the go command's own auto-switch writes.
    if ! ( cd "$scratch" && GOTOOLCHAIN=local GOWORK=off GOFLAGS=-mod=mod \
             "$go_cmd" mod download "$tc_mod" ) >"$dl_log" 2>&1; then
      why="downloading $tc_mod failed: $(tail -3 "$dl_log" | tr '\n' ' ' | sed 's/  */ /g')"
      refuse "$why"
    fi
  fi

  [ -x "$tc_dir/bin/go" ] || [ -f "$tc_dir/bin/go" ] ||
    refuse "the toolchain module is not at $tc_dir after the fetch (module cache: $modcache)"

  # The module cache stores files read-only; the go command itself sets exec
  # bits on first use (select.go's allowExec). Do the same, on the tool set it
  # runs: bin/ (go, gofmt) and pkg/tool (compile, vet, …).
  if [ ! -x "$tc_dir/bin/go" ]; then
    find "$tc_dir/bin" "$tc_dir/pkg/tool" -type f -exec chmod a+x {} + 2>/dev/null
  fi
  [ -x "$tc_dir/bin/go" ] || refuse "the cached toolchain at $tc_dir has no executable bin/go"

  got=$(GOTOOLCHAIN=local GOWORK=off "$tc_dir/bin/go" env GOVERSION 2>/dev/null)
  got="${got#go}"
  [ "$got" = "$req" ] ||
    refuse "the toolchain at $tc_dir reports go${got:-nothing}, expected go$req"

  echo "toolchain: go$got from the module cache at $tc_dir/bin/go (satisfies go.mod's $declared_by; downloaded under GOMODCACHE=$modcache)" >&2
  resolved="$tc_dir/bin/go"
fi

case "$mode" in
  print) printf '%s\n' "$resolved" ;;
  quiet) : ;;
esac
