#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════
# bunker-qa-mischief.sh — mischief's DOCUMENTED bunker-qa.sh invocation.
#
# Why a wrapper exists (MISCHIEF-DF-69): the fleet harness infers the install
# leg from the repo shape, and for any go.mod repo it picks `go build ./...`
# with a NAMED "no artifact expected" verdict. That is honest, but it grades
# nothing about mischief: the product's documented install path is `make bin`
# → bin/mischief, and nothing asserted that the leg left a STAMPED binary. Two
# harness knobs close that gap — this wrapper pins both so an operator never
# has to remember them:
#
#   BUNKER_QA_INSTALL_CMD     the leg runs the documented build and then the
#                             product's own artifact check (scripts/install-check.sh)
#   QA_EXPECTED_ARTIFACT      fresh-install asserts bin/mischief BY NAME was
#                             installed by this leg (never a toolsd-only lookup,
#                             never "any new executable")
#
# Usage: scripts/bunker-qa-mischief.sh <bunker-qa.sh args…>
#   e.g. scripts/bunker-qa-mischief.sh run "$PWD" --server bunker-mvp --keep
#        scripts/bunker-qa-mischief.sh launch --server bunker-mvp
#        scripts/bunker-qa-mischief.sh collect --evidence /tmp/<evidence>.jsonl
#
# The harness itself is HOST infra (it is not vendored in this repo):
# $BUNKER_QA > ~/.hermes/scripts/bunker-qa.sh > `bunker-qa.sh` on PATH.
# Exit: the harness's own exit status; 2 when the harness cannot be found.
# ═══════════════════════════════════════════════════════════════════════════
set -uo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

qa="${BUNKER_QA:-}"
if [ -z "$qa" ]; then
  if [ -x "$HOME/.hermes/scripts/bunker-qa.sh" ]; then
    qa="$HOME/.hermes/scripts/bunker-qa.sh"
  else
    qa=$(command -v bunker-qa.sh 2>/dev/null || true)
  fi
fi

if [ -z "$qa" ] || [ ! -f "$qa" ]; then
  {
    echo "bunker-qa-mischief: the fleet harness (bunker-qa.sh) was not found."
    echo "  looked at: \$BUNKER_QA, \$HOME/.hermes/scripts/bunker-qa.sh, PATH"
    echo "  fix:       BUNKER_QA=/path/to/bunker-qa.sh scripts/bunker-qa-mischief.sh $*"
  } >&2
  exit 2
fi

# `make bin` is the documented install path (README "Option B"); install-check.sh
# is the product-aware artifact assertion. Both run ON THE AGENT, in the synced
# repo root, so the relative script path is correct there.
export BUNKER_QA_INSTALL_CMD="${BUNKER_QA_INSTALL_CMD:-make bin && ./scripts/install-check.sh}"
export QA_EXPECTED_ARTIFACT="${QA_EXPECTED_ARTIFACT:-mischief}"

echo "bunker-qa-mischief: harness=$qa install=[$BUNKER_QA_INSTALL_CMD] artifact=$QA_EXPECTED_ARTIFACT (repo $root)" >&2
exec "$qa" "$@"
