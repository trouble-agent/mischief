#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════
# install-check.sh — the PRODUCT-aware install artifact check (MISCHIEF-DF-69).
#
# The fresh-system leg used to be graded by a generic detector that looked for
# "any new executable" (and, before QA-BUNKER-078, for a toolsd binary by name).
# Neither knows what mischief is supposed to install. This check does: the
# documented install path (`make bin`) must leave an EXECUTABLE mischief binary
# whose own `version --json` reports {"version":"<stamped>","tool":"mischief"},
# with a placeholder stamp ("unknown" = a bare `go build`) or a mismatched sha
# treated as a failure. Read-only: it verifies, it never builds.
#
# Usage: scripts/install-check.sh [path-to-binary]
#        (default: <repo>/bin/mischief; the caller's cwd is irrelevant)
# On success prints ONE machine-readable line:
#        ARTIFACT-OK: <path> version=<stamp> tool=mischief
# Exit: 0 = a valid stamped mischief artifact · 1 = named failure.
# ═══════════════════════════════════════════════════════════════════════════
set -uo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
bin_path="${1:-$root/bin/mischief}"
case "$bin_path" in
  /*) : ;;
  *) bin_path="$root/$bin_path" ;;
esac

fail() { echo "ARTIFACT-FAIL: $*" >&2; exit 1; }

[ -f "$bin_path" ] || fail "no mischief binary at $bin_path — the install leg produced no artifact"
[ -x "$bin_path" ] || fail "$bin_path exists but is not executable (mode 0755 expected)"

doc=$("$bin_path" version --json 2>&1)
rc=$?
[ "$rc" -eq 0 ] || fail "'$bin_path version --json' exited $rc: $doc"

version=$(printf '%s' "$doc" | sed -nE 's/.*"version":"([^"]*)".*/\1/p')
tool=$(printf '%s' "$doc" | sed -nE 's/.*"tool":"([^"]*)".*/\1/p')

[ -n "$version" ] || fail "no \"version\" field in '$doc'"
[ "$tool" = "mischief" ] || fail "\"tool\" is '${tool:-<absent>}', expected 'mischief' (got: $doc)"
[ "$version" != "unknown" ] || fail "version is 'unknown' — an unstamped build (bare \`go build\`); the install leg must stamp the tree's sha (make bin does)"

# A sha stamp must name THIS tree; a release tag stamp (vX.Y.Z-…) is a released
# build and is accepted as-is (a tarball install has no checkout to compare to).
if printf '%s' "$version" | grep -Eq '^[0-9a-f]{7,40}$'; then
  head_sha=$(git -C "$root" rev-parse --short=12 HEAD 2>/dev/null || true)
  if [ -n "$head_sha" ]; then
    [ "$version" = "$head_sha" ] || \
      fail "stamp $version does not match HEAD $head_sha in $root — the artifact was not built from this tree"
  fi
else
  printf '%s' "$version" | grep -Eq '^v[0-9]+\.[0-9]+' || \
    fail "stamp '$version' is neither a git sha nor a release tag"
fi

echo "ARTIFACT-OK: $bin_path version=$version tool=mischief"
