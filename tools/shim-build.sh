#!/usr/bin/env bash
# tools/shim-build.sh — build the release shim .so per architecture (MSF-018).
#
# DECIDED PATH (MSF-018): the per-arch .so is compiled from the SAME source
# the runtime compiles at arm time — the ShimSourceC constant in
# internal/backend_signal/shim.go — extracted verbatim here, built with the
# SYSTEM cc (amd64, native) and the DISTRO CROSS-GCC (arm64,
# gcc-aarch64-linux-gnu). Zig is NOT used: it is absent on this host and
# unnecessary — two linux targets, glibc ABI, plain C; a container-with-zig
# path adds infra without removing a constraint. CI (ubuntu runners) has
# cc on the base image; the arm64 cross-gcc + qemu-user are one apt line,
# and a release REFUSES (exit 1, named tool) rather than skipping.
#
# The compiled .so is a RELEASE ARTIFACT per arch (the shape ArmShim's
# standaloneShimCandidates already looks for at /tmp/mischief-shim/
# libfault-anchored.so), not a build-time afterthought.
#
# Drift gate: the extracted source is written to tools/shim-extracted.c
# (byte-identical to the constant, comments included). If the committed
# copy is stale the file is refreshed and the script says so — CI fails
# on `git diff --exit-code -- tools/shim-extracted.c`, so a ShimSourceC
# edit can never land without re-stamping the release source.
#
# Usage: tools/shim-build.sh [--out DIR] [--smoke] [amd64] [arm64]
#   --out DIR   output dir (default: <repo>/dist/shim)
#   --smoke     after each build, LAND the fault live: native amd64 run;
#               arm64 under qemu-user (skipped with the named reason when
#               qemu-aarch64 or the arm64 sysroot is missing)
# Targets default to "amd64 arm64".
#
# ch:trace row=MSF-018 evidence=tools/shim-build.sh + tools/shim-extracted.c witness=none:no-tag-cut-from-worker

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SHIM_GO="$REPO_ROOT/internal/backend_signal/shim.go"
VICTIM_C="$REPO_ROOT/probe/victim.c"
OUT="$REPO_ROOT/dist/shim"
SMOKE=0
TARGETS=()

usage() { sed -n '2,30p' "${BASH_SOURCE[0]}" | grep -E '^# (Usage|  )' | sed 's/^# //'; }

while [ $# -gt 0 ]; do
  case "$1" in
    --smoke) SMOKE=1 ;;
    --out) OUT="$2"; shift ;;
    -h|--help) usage; exit 0 ;;
    amd64|arm64) TARGETS+=("$1") ;;
    *) echo "shim-build: unknown argument: $1 (see --help)" >&2; exit 2 ;;
  esac
  shift
done
[ ${#TARGETS[@]} -eq 0 ] && TARGETS=(amd64 arm64)

[ -f "$SHIM_GO" ] || { echo "shim-build: source constant not found: $SHIM_GO" >&2; exit 1; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

# extract_source: pull ShimSourceC's exact bytes out of shim.go (between the
# opening `const ShimSourceC = <backtick>` line and the closing backtick).
# The opening line carries the C comment opener (`const ShimSourceC = ` is a
# Go prefix ON THE SAME LINE as `/* mischief...`) — strip ONLY the Go prefix
# and keep the rest, or the extracted .c starts mid-comment.
extract_source() {
  awk '/^const ShimSourceC = `/{sub(/^const ShimSourceC = `/,""); f=1; print; next} f && /^`$/{exit} f' "$SHIM_GO"
}

extract_source > "$T/libfault-anchored.c"
[ -s "$T/libfault-anchored.c" ] || {
  echo "shim-build: FATAL — ShimSourceC extraction is EMPTY (did shim.go's constant marker move?)" >&2
  exit 1
}
# sanity: the extracted bytes must still be the anchored shim
grep -q 'ssize_t write(int fd' "$T/libfault-anchored.c" \
  || { echo "shim-build: FATAL — extraction lost the write() interception (refusing to build a wrong shim)" >&2; exit 1; }
grep -q '__attribute__((constructor))' "$T/libfault-anchored.c" \
  || { echo "shim-build: FATAL — extraction lost the constructor (refusing)" >&2; exit 1; }
# determinism control: a second extraction must be byte-identical
extract_source > "$T/again.c"
cmp -s "$T/libfault-anchored.c" "$T/again.c" \
  || { echo "shim-build: FATAL — extraction is non-deterministic (refusing)" >&2; exit 1; }

# drift gate against the committed release source: refresh + say so (CI
# enforces the rest via git diff --exit-code).
if [ ! -f "$REPO_ROOT/tools/shim-extracted.c" ] \
   || ! cmp -s "$T/libfault-anchored.c" "$REPO_ROOT/tools/shim-extracted.c"; then
  cp "$T/libfault-anchored.c" "$REPO_ROOT/tools/shim-extracted.c"
  echo "shim: tools/shim-extracted.c REFRESHED from ShimSourceC (was stale or absent) — commit it; CI gates the drift"
else
  echo "shim: tools/shim-extracted.c matches ShimSourceC (sha256 $(sha256sum "$REPO_ROOT/tools/shim-extracted.c" | cut -d' ' -f1 | cut -c1-16)…)"
fi

mkdir -p "$OUT"

cc_for() {
  case "$1" in
    amd64) echo "${CC_AMD64:-cc}" ;;
    arm64) echo "${CC_ARM64:-aarch64-linux-gnu-gcc}" ;;
  esac
}

elf_machine_for() {
  case "$1" in
    amd64) echo "X86-64" ;;
    arm64) echo "AArch64" ;;
  esac
}

# compile_one <arch>: cc flags MUST match the runtime's buildShimLib
# (-shared -fPIC -O2 … -ldl) so the release .so is the same shape the
# selftest compiles fresh.
compile_one() {
  local arch="$1"
  local so="$OUT/libfault-anchored_linux_${arch}.so"
  local cc_bin; cc_bin="$(cc_for "$arch")"
  if ! command -v "$cc_bin" >/dev/null 2>&1; then
    echo "shim: REFUSE [$arch]: compiler '$cc_bin' not found — the per-arch .so cannot build" >&2
    if [ "$arch" = "arm64" ]; then
      echo "  install (docs/RELEASE.md): sudo apt-get update && sudo apt-get install -y gcc-aarch64-linux-gnu" >&2
    fi
    return 1
  fi
  if ! ( cd "$T" && "$cc_bin" -shared -fPIC -O2 -o "$so" "$T/libfault-anchored.c" -ldl ); then
    echo "shim: FAIL [$arch]: compile failed" >&2
    return 1
  fi
  local want; want="$(elf_machine_for "$arch")"
  readelf -h "$so" | grep -q "Machine:.*${want}" \
    || { echo "shim: FAIL [$arch]: ELF machine is not $want: $(readelf -h "$so" | grep Machine:)" >&2; return 1; }
  readelf -h "$so" | grep -q "Type:.*DYN" \
    || { echo "shim: FAIL [$arch]: not a shared object (Type != DYN)" >&2; return 1; }
  nm -D "$so" | grep -Eq ' T write$' \
    || { echo "shim: FAIL [$arch]: write is not exported (nm -D)" >&2; return 1; }
  echo "shim: $arch -> $so ($(du -h "$so" | cut -f1), ELF $want DYN, write exported)"
}

# smoke_one <arch>: land write:*:smoke-target.txt:EIO on a REAL process —
# amd64 native; arm64 as a dynamically-linked victim under qemu-user with
# the cross sysroot (-L). Asserts all three proof legs: the fault landed
# (EIO on the target write), the control write passed (/dev/null), and the
# shim's own counter PROVES the landing (hits=1 + this rule's text).
smoke_one() {
  local arch="$1"
  local so="$OUT/libfault-anchored_linux_${arch}.so"
  local victim="$T/victim_${arch}"
  local run_prefix=()
  case "$arch" in
    amd64)
      cc -O2 -o "$victim" "$VICTIM_C"
      ;;
    arm64)
      local qemu
      qemu="$(command -v qemu-aarch64 || command -v qemu-aarch64-static || true)"
      if [ -z "$qemu" ]; then
        echo "shim: SKIP smoke [arm64]: no qemu-aarch64 on PATH (install qemu-user) — .so is built and ELF-verified, live landing unproven here"
        return 0
      fi
      if [ ! -e /usr/aarch64-linux-gnu/lib/ld-linux-aarch64.so.1 ]; then
        echo "shim: SKIP smoke [arm64]: no arm64 sysroot (libc6-dev-arm64-cross missing) — a static victim cannot honor LD_PRELOAD (no guest loader), so the live leg cannot run"
        return 0
      fi
      aarch64-linux-gnu-gcc -O2 -o "$victim" "$VICTIM_C"
      run_prefix=("$qemu" -L /usr/aarch64-linux-gnu)
      ;;
  esac
  local out="$T/smoke_out_${arch}" proof="$T/proof_${arch}" target="$T/smoke-target.txt"
  rm -f "$proof" "$target"
  if ! env LD_PRELOAD="$so" \
        MCFAULT="write:*:smoke-target.txt:EIO" MCFAULT_BUDGET=1 MCFAULT_PROOF="$proof" \
        "${run_prefix[@]}" "$victim" "$target" > "$out" 2>&1; then
    echo "shim: FAIL smoke [$arch]: victim exited nonzero:" >&2; cat "$out" >&2; return 1
  fi
  grep -q 'errno=Input/output error' "$out" \
    || { echo "shim: FAIL smoke [$arch]: target write did not return EIO:" >&2; cat "$out" >&2; return 1; }
  grep -q 'write(/dev/null) -> 6' "$out" \
    || { echo "shim: FAIL smoke [$arch]: control write to /dev/null was affected:" >&2; cat "$out" >&2; return 1; }
  grep -qF 'hits=1 rule=write:*:smoke-target.txt:EIO' "$proof" \
    || { echo "shim: FAIL smoke [$arch]: landproof counter missing or wrong:" >&2; cat "$proof" >&2; return 1; }
  echo "shim: smoke $arch -> EIO landed, /dev/null control clean, proof hits=1 (landed-proof independent of target)"
}

rc=0
for t in "${TARGETS[@]}"; do
  compile_one "$t" || rc=1
done
[ "$rc" -eq 0 ] || { echo "shim: one or more targets FAILED to build — no artifact is trusted" >&2; exit 1; }

if [ "$SMOKE" -eq 1 ]; then
  for t in "${TARGETS[@]}"; do
    smoke_one "$t" || rc=1
  done
  [ "$rc" -eq 0 ] || { echo "shim: smoke FAILED — the .so exists but did not prove its landing" >&2; exit 1; }
fi

( cd "$OUT" && sha256sum libfault-anchored_linux_*.so > SHA256SUMS )
echo "shim: source sha256 $(sha256sum "$T/libfault-anchored.c" | cut -d' ' -f1)"
echo "shim: SHA256SUMS written to $OUT/SHA256SUMS"
