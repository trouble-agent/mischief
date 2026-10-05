#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════
# bunker-plane.sh — mischief's ONE-COMMAND bunker test plane (MSF-021).
#
#   scripts/bunker-plane.sh run            # the whole pipeline, one command
#
# What one `run` does, in order, recording a JSONL evidence row after EVERY
# act (so a killed run leaves whole rows, never a zero-byte file):
#
#   preflight  bunker server reachable + ONLINE (else named SKIP, exit 3)
#   build      host builds the CGO_ENABLED=0 linux/amd64 binary (bare agents
#              have no C toolchain — the BINARY is the build)
#   spawn      `bunker spawn` (≤2 attempts, orphan-safe: spawns that fail the
#              exec reachability gate are destroyed before retry) → agent id
#              via the Go parser
#   ship       repo (frozen HEAD as one tar.gz) + the prebuilt binary onto
#              the agent via `bunker cp`, both verified on arrival
#   selftest   on the agent: reason-bearing sanction marker (SPEC-13's L1
#              production home) → `mischief selftest --all` (script shipped
#              verbatim via `bunker cp`, run via `exec sh <path>`) → L0
#              scratch land+revert proofs + the AC-19 L1 admission report
#   collect    journal + selftest stdout pulled back base64; sha256 +
#              landed-proof counters verified against the agent's own numbers
#   destroy    ≤3 attempts + `bunker list` re-check → TEARDOWN-GONE /
#              TEARDOWN-RECOVERED / TEARDOWN-LEAK (a leak grades FAIL)
#   fold       `mischief plane fold` renders the verdict:
#              exit 0 = PASS · 1 = FAIL · 3 = named SKIP (never a fake pass)
#
# Substrate choice (docs/TEST-PLANE.md §decision): bunker-qa.sh-style ONE-
# SHOT JIT, not bunker-soak.sh and not gauntlet — the selftest payload is
# bounded-fast (seconds, no duration axis to soak), gauntlet has no
# ephemeral-host concept at all, and the evidence logic lives in the tested
# Go package internal/plane (+ `mischief plane`), not in this script.
#
# Transport: the bunkerd CLI is the ONLY channel to the agent
# (`bunker exec` / `deploy` / `cp` / `destroy`). Raw ssh to the agent is NOT
# part of this server class's contract — measured 2026-10-05: spawns whose
# :2223 ssh never answers pass `bunker exec` in ~1s. Never gate reachability
# on a raw ssh probe here.
#
# SECRETS: `bunker spawn`'s connection bundle carries an API KEY and key
# PATHS. Spawn output is therefore NEVER quoted verbatim into evidence or
# logs — skip reasons carry only allowlisted refusal keywords.
#
# Environment (all optional):
#   BUNKER_PLANE_SERVER            bunker server name   (default bunker-mvp)
#   BUNKER_PLANE_TTL               agent TTL            (default 2h)
#   BUNKER_PLANE_EVIDENCE          evidence JSONL path  (default per-run
#                                  /tmp/mischief-plane-evidence-<ts>-$$.jsonl)
#   BUNKER_PLANE_REPO              repo to ship         (default: this script's
#                                  repo root, resolved from git)
#   BUNKER_PLANE_SELFTEST_TIMEOUT  remote selftest cap  (default 300s)
#   BUNKER_PLANE_KEEP=1            skip the destroy (recorded as a named skip)
#
# ch:trace row=MSF-021 spec=docs/TEST-PLANE.md evidence=scripts/bunker-plane.sh + internal/plane/ + cmd/mischief/plane.go witness=live-run-2026-10-05
set -uo pipefail

usage() {
  sed -n '2,64p' "$0" | grep -E '^#( |$)' | sed 's/^# \{0,1\}//'
}

# ── config ───────────────────────────────────────────────────────────────────
SERVER="${BUNKER_PLANE_SERVER:-bunker-mvp}"
TTL="${BUNKER_PLANE_TTL:-2h}"
SELFTEST_TIMEOUT="${BUNKER_PLANE_SELFTEST_TIMEOUT:-300}"
SYNC_MAX_BYTES="${BUNKER_PLANE_SYNC_MAX_BYTES:-314572800}"
BIN_MAX_BYTES="${BUNKER_PLANE_BIN_MAX_BYTES:-104857600}"
SPAWN_ATTEMPTS="${BUNKER_PLANE_SPAWN_ATTEMPTS:-2}"
REPO="${BUNKER_PLANE_REPO:-}"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
EVIDENCE="${BUNKER_PLANE_EVIDENCE:-/tmp/mischief-plane-evidence-$TS-$$.jsonl}"

log() { echo "→ $*"; }
die_usage() { echo "bunker-plane: $*" >&2; usage >&2; exit 2; }

ACTION="run"
case "${1:-}" in
  run|help|-h|--help) ACTION="${1:-run}"; shift || true ;;
  "") ;;
  *) die_usage "unknown action '${1}' (run | help)" ;;
esac
[ "$ACTION" = "help" ] && { usage; exit 0; }
for arg in "$@"; do
  case "$arg" in
    --server=*) SERVER="${arg#*=}" ;;
    --ttl=*) TTL="${arg#*=}" ;;
    --evidence=*) EVIDENCE="${arg#*=}" ;;
    --repo=*) REPO="${arg#*=}" ;;
    *) die_usage "unknown flag '$arg'" ;;
  esac
done

# ── the plane binary: built fresh, used for every evidence act ───────────────
WORK="$(mktemp -d /tmp/mischief-plane-XXXXXX)"
BIN="$WORK/mischief"
cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  if [ -n "${AGENT_ID:-}" ] && [ "${DESTROYED:-0}" = 0 ] && [ "${KEEP:-0}" = 0 ]; then
    log "driver exit rc=$rc with agent $AGENT_ID live — trap destroy"
    bunker destroy "$AGENT_ID" --server "$SERVER" >/dev/null 2>&1 \
      && "$BIN" plane record --file "$EVIDENCE" --step destroy --status pass \
           --detail "trap destroy after driver exit rc=$rc (agent $AGENT_ID)" >/dev/null 2>&1 \
      || "$BIN" plane record --file "$EVIDENCE" --step destroy --status fail \
           --detail "trap destroy FAILED after driver exit rc=$rc — agent $AGENT_ID may hold a slot until its ${TTL} TTL reaps it; manual: bunker destroy $AGENT_ID --server $SERVER" >/dev/null 2>&1
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

# ── evidence helper: EVERY row is written by the tested Go verb ──────────────
row() { # row <step> <pass|fail|skip> <detail>
  "$BIN" plane record --file "$EVIDENCE" --step "$1" --status "$2" --detail "$3" || \
    echo "bunker-plane: EVIDENCE WRITE FAILED for $1/$2" >&2
}
finish() { # finish <exit-code> — fold then exit (the fold's rc IS the verdict)
  "$BIN" plane fold --file "$EVIDENCE"
  exit "$1"
}

# ── agent channel: the bunkerd CLI is the ONLY transport ─────────────────────
# Transport facts MEASURED on this daemon (2026-10-05):
#   * plain `exec` re-joins the argv into ONE remote shell line — a quoted
#     compound (`sh -c 'a && b'`) shatters; single-token commands are fine;
#   * `exec --script` SHELL-ESCAPES the upload onto disk (every ' becomes
#     '\'' plus a trailing line) — safe ONLY for quote-free scripts
#     (readback-verified: sha and line count differ for quoted content);
#   * `bunker cp` is byte-verbatim (an 11.8 MB binary survives sha-identical).
# Consequence: compound agent work rides an UPLOADED SCRIPT — quote-free
# helpers may use exec --script; anything carrying quotes (the selftest
# script) ships via cp and runs via single-token `exec sh <path>`.
agent_exec() { # agent_exec <timeout-s> <single-token-cmd...>
  [ -n "$AGENT_ID" ] || { echo "agent_exec: no agent" >&2; return 1; }
  local t="$1"; shift   # BUGFIX (run-7 lesson): without the shift, "$@"
  bunker exec --server "$SERVER" --timeout "$t" "$AGENT_ID" -- "$@"
}
agent_script() { # agent_script <timeout-s> <local-script-file> — QUOTE-FREE scripts only (the daemon escapes '); rc = script's rc
  [ -n "$AGENT_ID" ] || { echo "agent_script: no agent" >&2; return 1; }
  bunker exec --server "$SERVER" --timeout "$1" "$AGENT_ID" --script "$2"
}

# ═══════════════════════════ run ═════════════════════════════════════════════
log "mischief bunker test plane — server=$SERVER ttl=$TTL evidence=$EVIDENCE"

# ── stage 0: build the plane binary FIRST (it records every later row) ──────
REPO="${REPO:-$(cd "$(dirname "$0")/.." && pwd)}"
cd "$REPO" || { echo "bunker-plane: repo $REPO missing" >&2; exit 1; }
[ -f "$REPO/go.mod" ] || { echo "bunker-plane: $REPO is not the mischief repo root" >&2; exit 1; }
SHA="$(git -C "$REPO" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
if CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.gitSha=$SHA" -o "$BIN" ./cmd/mischief; then
  log "built plane binary ($SHA, $(wc -c < "$BIN") bytes)"
else
  echo "bunker-plane: host build FAILED — the plane cannot record evidence" >&2
  exit 1
fi

# ── stage 1: preflight — the bunker server must answer and be ONLINE ────────
status_out="$(timeout 30 bunker status --server "$SERVER" 2>&1)"
if printf '%s' "$status_out" | grep -q 'ONLINE'; then
  row preflight pass "server $SERVER reachable: $(printf '%s' "$status_out" | grep -m1 'Agents:' | sed 's/^ *//')"
  log "preflight: $SERVER ONLINE"
else
  reason="bunker server $SERVER not reachable/ONLINE (status: $(printf '%s' "$status_out" | grep -viE 'key|token' | tail -1)) — no agent attempted"
  row preflight skip "$reason"
  log "preflight SKIP: $reason"
  finish 3
fi

# ── stage 2: the ship binary (same static build serves as payload) ───────────
BIN_BYTES="$(wc -c < "$BIN")"
if [ "$BIN_BYTES" -gt "$BIN_MAX_BYTES" ]; then
  row build fail "binary $BIN_BYTES > cap $BIN_MAX_BYTES"
  finish 1
fi
row build pass "CGO_ENABLED=0 linux/amd64 mischief $SHA: $BIN_BYTES bytes (no agent toolchain needed)"
log "ship binary ready: $BIN_BYTES bytes"

# ── stage 3: spawn (orphan-safe; secrets redacted from reasons) ──────────────
AGENT_ID=""
spawn_out=""
for attempt in $(seq 1 "$SPAWN_ATTEMPTS"); do
  spawn_out="$(timeout 120 bunker spawn --server "$SERVER" --ttl "$TTL" 2>&1)"
  id="$(printf '%s' "$spawn_out" | "$BIN" plane parse-agent-id)"
  if [ -z "$id" ]; then
    [ "$attempt" -lt "$SPAWN_ATTEMPTS" ] && sleep 5
    continue
  fi
  # reachability gate BEFORE hand-off: an agent that fails `bunker exec`
  # is destroyed before retry, never leaked to the caller as a success.
  # (Raw ssh is NOT the gate: this server class does not answer it — the
  # 2026-10-05 measurement that rewrote this stage.)
  if timeout 180 bunker exec --server "$SERVER" --timeout 60 "$id" -- true >/dev/null 2>&1; then
    AGENT_ID="$id"
    break
  fi
  echo "spawn attempt $attempt produced agent $id but failed the exec gate — destroyed before retry" >&2
  bunker destroy "$id" --server "$SERVER" >/dev/null 2>&1 || echo "WARN: destroy of unreachable $id failed (may hold a slot until TTL)" >&2
done

if [ -z "$AGENT_ID" ]; then
  # REDACTION: spawn's connection bundle carries an API key — never quote
  # it. Reasons carry allowlisted refusal keywords only.
  safe="$(printf '%s' "$spawn_out" | grep -ioE 'capacity exhaust[a-z ]*\([0-9/]+\)|unauthenticated[^,]*|invalid token|offline|not found' | head -2 | tr '\n' ';')"
  [ -z "$safe" ] && safe="spawn produced no agent that passed the exec gate (output withheld: the connection bundle carries credentials)"
  reason="bunker agent not obtained on $SERVER after $SPAWN_ATTEMPTS attempts (blockers: ${safe%%;}) — AC 3/4: recorded SKIP, no selftest ran, nothing passed"
  row spawn skip "$reason"
  log "SKIP: $reason"
  finish 3
fi
row spawn pass "agent $AGENT_ID on $SERVER (ttl $TTL), exec gate passed"
log "agent: $AGENT_ID"

# ── stage 4: ship repo (frozen HEAD) + binary, both verified on arrival ──────
# The repo rides as ONE tar.gz (`bunker cp` is per-file SCP through a
# tunnel — measured 2026-10-05: a 191-file directory deploy cannot finish
# inside its timeout, a single 436 KB tar lands in ~8s and extracts in ~1s).
PAYLOAD_TGZ="$WORK/payload.tar.gz"
if ! git -C "$REPO" archive --format=tar.gz -o "$PAYLOAD_TGZ" HEAD; then
  row ship fail "git archive HEAD failed (empty repo?)"
  finish 1
fi
ARCHIVE_BYTES="$(wc -c < "$PAYLOAD_TGZ")"
if [ "$ARCHIVE_BYTES" -eq 0 ] || [ "$ARCHIVE_BYTES" -gt "$SYNC_MAX_BYTES" ]; then
  row ship fail "payload $ARCHIVE_BYTES bytes (empty or > cap $SYNC_MAX_BYTES)"
  finish 1
fi
LOCAL_FILES="$(git -C "$REPO" ls-files | wc -l)"
AGENT_HOME="/home/bunker-$AGENT_ID"
# every leg stays VISIBLE: a silent compound cannot name which leg failed
ship_out="$(timeout 300 bunker cp --server "$SERVER" "$PAYLOAD_TGZ" "$AGENT_ID:$AGENT_HOME/payload.tar.gz" </dev/null 2>&1)"; ship_rc=$?
# extract rides an UPLOADED SCRIPT (quoting survives verbatim; rc propagates)
cat > "$WORK/extract.sh" <<EOF
set -u
mkdir -p "$AGENT_HOME/plane"
tar -xzf "$AGENT_HOME/payload.tar.gz" -C "$AGENT_HOME/plane"
if [ -f "$AGENT_HOME/plane/go.mod" ]; then
  echo "SYNC-OK files=\$(find "$AGENT_HOME/plane" -type f | wc -l)"
else
  echo "SYNC-FAIL: no go.mod after extract"; exit 20
fi
EOF
extract_out="$(agent_script 120 "$WORK/extract.sh" 2>&1)"; extract_rc=$?
if [ "$ship_rc" -eq 0 ] && [ "$extract_rc" -eq 0 ] && printf '%s' "$extract_out" | grep -q SYNC-OK; then
  row ship pass "repo shipped (frozen HEAD=$SHA: ${ARCHIVE_BYTES}B tar.gz, go.mod verified, $LOCAL_FILES tracked files)"
else
  row ship fail "ship leg rc=$ship_rc ($(printf '%s' "$ship_out" | tail -1 | cut -c1-140)); extract leg rc=$extract_rc: $(printf '%s' "$extract_out" | tail -1 | cut -c1-160)"
  finish 1
fi
LOCAL_SHA="$(sha256sum "$BIN" | cut -d' ' -f1)"
bcp_out="$(timeout 600 bunker cp --server "$SERVER" "$BIN" "$AGENT_ID:$AGENT_HOME/plane/mischief" </dev/null 2>&1)"; bcp_rc=$?
# chmod+verify rides one script (single-token execs only, per the transport facts)
cat > "$WORK/binverify.sh" <<EOF
set -u
chmod +x "$AGENT_HOME/plane/mischief" || exit 30
sha256sum "$AGENT_HOME/plane/mischief"
EOF
bverify_out="$(agent_script 120 "$WORK/binverify.sh" 2>&1)"; bverify_rc=$?
REMOTE_SHA="$(printf '%s' "$bverify_out" | grep -oP '^[0-9a-f]{64}' | head -1)"
if [ "$bcp_rc" -eq 0 ] && [ "$bverify_rc" -eq 0 ] && [ -n "$REMOTE_SHA" ]; then
  bin_leg="cp rc=$bcp_rc, chmod+verify rc=$bverify_rc"
else
  bin_leg="cp rc=$bcp_rc ($(printf '%s' "$bcp_out" | tail -1 | cut -c1-120)), chmod+verify rc=$bverify_rc ($(printf '%s' "$bverify_out" | tail -1 | cut -c1-80))"
fi
if [ -n "$REMOTE_SHA" ] && [ "$REMOTE_SHA" = "$LOCAL_SHA" ]; then
  row ship pass "binary shipped sha256-verified: ${LOCAL_SHA:0:16}…"
else
  row ship fail "binary sha256 mismatch local=$LOCAL_SHA remote=${REMOTE_SHA:-<none>} — not executing a binary we cannot verify (legs: $bin_leg)"
  finish 1
fi

# ── stage 5: remote selftest — the script ships via `bunker cp` (VERBATIM:
#    sha-checked channel) and runs via single-token `exec sh <path>`.
#    `exec --script` is UNUSABLE for quoted scripts — measured 2026-10-05:
#    the daemon shell-escapes the upload onto disk (every ' becomes '\''
#    plus a trailing line), so any script carrying single quotes dies with
#    "Unterminated quoted string". cp + `sh` keeps content byte-identical.
cat > "$WORK/remote.sh" <<'REMOTE_EOF'
set -u
cd "$HOME/plane" || exit 90
# SPEC-13: the reason-bearing marker file is the sanction's L1 production home
printf "%s\n" "mischief bunker test plane (MSF-021): ephemeral L1 sanctioned agent $(id -un)" > "$HOME/mischief-sanction"
export MISCHIEF_SANCTION_FILE="$HOME/mischief-sanction"
timeout PLANE_SELFTEST_TIMEOUT ./mischief selftest --all --dir "$HOME/plane-run" > selftest.stdout 2> selftest.stderr
rc=$?
echo "SELFTEST_RC=$rc"
j="$HOME/plane-run/selftest.jsonl"
if [ -f "$j" ]; then
  echo "JOURNAL_SHA256=$(sha256sum "$j" | cut -d" " -f1)"
  echo "JOURNAL_LINES=$(wc -l < "$j")"
  echo "LANDED=$(grep -c "\"type\":\"fault_landed\"" "$j" || true)"
  echo "REVERTED=$(grep -c "\"type\":\"fault_reverted\"" "$j" || true)"
  echo "SKIPPED=$(grep -c "\"outcome\":\"skipped\"" "$j" || true)"
  echo "JOURNAL_B64=$(base64 -w0 "$j")"
else
  echo "JOURNAL_MISSING=1"
fi
echo "STDOUT_B64=$(base64 -w0 selftest.stdout)"
echo "STDERR_B64=$(base64 -w0 selftest.stderr)"
echo REMOTE-DONE
REMOTE_EOF
# substitute the timeout knob OUTSIDE the heredoc (the script text stays
# static; a knob can never inject shell into it)
sed -i "s/PLANE_SELFTEST_TIMEOUT/${SELFTEST_TIMEOUT}/" "$WORK/remote.sh"
bash -n "$WORK/remote.sh" || { row selftest fail "generated remote script failed bash -n"; finish 1; }

rcp_out="$(timeout 60 bunker cp --server "$SERVER" "$WORK/remote.sh" "$AGENT_ID:$AGENT_HOME/plane/remote.sh" </dev/null 2>&1)"; rcp_rc=$?
[ "$rcp_rc" -ne 0 ] && { row selftest fail "remote script cp failed rc=$rcp_rc: $(printf '%s' "$rcp_out" | tail -1 | cut -c1-140)"; finish 1; }

log "running selftest on agent (cap ${SELFTEST_TIMEOUT}s)…"
EXEC_TIMEOUT=$((SELFTEST_TIMEOUT + 180))
remote_out="$(agent_exec "$EXEC_TIMEOUT" sh "$AGENT_HOME/plane/remote.sh" 2>&1)"; remote_rc=$?
# exec-layer failure (transport died) vs script failure (rc echoed inside)
if ! printf '%s' "$remote_out" | grep -q 'SELFTEST_RC='; then
  row selftest fail "remote script produced no verdict (exec rc=$remote_rc; $(printf '%s' "$remote_out" | grep -viE 'key|token' | tail -1 | cut -c1-160))"
  finish 1
fi

# ── stage 6: collect — decode at home, verify against the agent's numbers ────
RemoteVal() { printf '%s' "$remote_out" | grep -oP "^$1=\K.*" | head -1; }
SELFTEST_RC="$(RemoteVal SELFTEST_RC)"
JOURNAL_SHA="$(RemoteVal JOURNAL_SHA256)"
JOURNAL_LINES="$(RemoteVal JOURNAL_LINES)"
R_LANDED="$(RemoteVal LANDED)"
R_REVERTED="$(RemoteVal REVERTED)"
R_SKIPPED="$(RemoteVal SKIPPED)"
printf '%s' "$remote_out" | sed -n 's/^JOURNAL_B64=//p' | head -1 | base64 -d > "$WORK/selftest.jsonl" 2>/dev/null
printf '%s' "$remote_out" | sed -n 's/^STDOUT_B64=//p'  | head -1 | base64 -d > "$WORK/selftest.stdout" 2>/dev/null
printf '%s' "$remote_out" | sed -n 's/^STDERR_B64=//p'  | head -1 | base64 -d > "$WORK/selftest.stderr" 2>/dev/null

if [ ! -s "$WORK/selftest.jsonl" ]; then
  row collect fail "journal missing/empty after decode (JOURNAL_MISSING=$(RemoteVal JOURNAL_MISSING || echo '?'), selftest stderr tail: $(tail -1 "$WORK/selftest.stderr" 2>/dev/null | cut -c1-200))"
  finish 1
fi
LOCAL_JOURNAL_SHA="$(sha256sum "$WORK/selftest.jsonl" | cut -d' ' -f1)"
LOCAL_LINES="$(wc -l < "$WORK/selftest.jsonl")"
collect_problems=""
[ -n "$JOURNAL_SHA" ] && [ "$JOURNAL_SHA" = "$LOCAL_JOURNAL_SHA" ] || collect_problems+=" journal_sha_mismatch(remote=${JOURNAL_SHA:-none} local=$LOCAL_JOURNAL_SHA)"
[ "$JOURNAL_LINES" = "$LOCAL_LINES" ] || collect_problems+=" journal_line_mismatch(remote=${JOURNAL_LINES:-none} local=$LOCAL_LINES)"
L_LANDED="$(grep -c '"type":"fault_landed"' "$WORK/selftest.jsonl" || true)"
L_REVERTED="$(grep -c '"type":"fault_reverted"' "$WORK/selftest.jsonl" || true)"
L_SKIPPED="$(grep -c '"outcome":"skipped"' "$WORK/selftest.jsonl" || true)"
[ "$R_LANDED" = "$L_LANDED" ] || collect_problems+=" landed_counter_mismatch(remote=${R_LANDED:-none} local=$L_LANDED)"
[ "$R_REVERTED" = "$L_REVERTED" ] || collect_problems+=" reverted_counter_mismatch(remote=${R_REVERTED:-none} local=$L_REVERTED)"
[ "$R_SKIPPED" = "$L_SKIPPED" ] || collect_problems+=" skipped_counter_mismatch(remote=${R_SKIPPED:-none} local=$L_SKIPPED)"

if [ -n "$collect_problems" ]; then
  row collect fail "destination coverage FAILED:$collect_problems"
  finish 1
fi
row collect pass "journal $LOCAL_LINES lines sha256-verified; landed-proof counters verified: landed=$L_LANDED reverted=$L_REVERTED skipped=$L_SKIPPED; selftest.stdout $(wc -c < "$WORK/selftest.stdout") bytes pulled"
log "collected: journal=$LOCAL_LINES lines landed=$L_LANDED reverted=$L_REVERTED skipped=$L_SKIPPED rc=$SELFTEST_RC"

# artifacts land next to the evidence file for the human
cp "$WORK/selftest.jsonl" "$EVIDENCE.journal"
cp "$WORK/selftest.stdout" "$EVIDENCE.selftest-stdout"
cp "$WORK/selftest.stderr" "$EVIDENCE.selftest-stderr"

# the selftest verdict rides its own tested mapping
ST_VERDICT="$("$BIN" plane selftest-verdict --exit-code "${SELFTEST_RC:--1}")"
row selftest "$ST_VERDICT" "remote selftest --all exit ${SELFTEST_RC:-unset} → $ST_VERDICT; summary: $(grep -m1 'selftest:.*pass' "$WORK/selftest.stdout" || echo '(no summary line)')"

# ── stage 7: destroy (retry + list re-check; leak = FAIL) ────────────────────
DESTROYED=0
KEEP=0
if [ "${BUNKER_PLANE_KEEP:-0}" = "1" ]; then
  KEEP=1
  row destroy skip "agent $AGENT_ID kept by BUNKER_PLANE_KEEP=1 — destroy it with: bunker destroy $AGENT_ID --server $SERVER"
  log "agent $AGENT_ID KEPT (evidence intact); fold follows"
else
  drc=0; destroy_failed=0
  for attempt in 1 2 3; do
    bunker destroy "$AGENT_ID" --server "$SERVER" >/dev/null 2>&1 && { drc=0; destroy_failed=0; break; }
    drc=$?; destroy_failed=1
    echo "destroy attempt $attempt failed rc=$drc for $AGENT_ID" >&2
    [ "$attempt" -lt 3 ] && sleep 5
  done
  if timeout 20 bunker list --server "$SERVER" 2>/dev/null | grep -qF "$AGENT_ID"; then
    row destroy fail "TEARDOWN-LEAK: agent $AGENT_ID still listed on $SERVER after 3 destroy attempts — holds a slot until its $TTL TTL reaps it; manual: bunker destroy $AGENT_ID --server $SERVER"
  elif [ "$destroy_failed" = 1 ]; then
    DESTROYED=1
    row destroy pass "TEARDOWN-RECOVERED: destroy reported failure (rc=$drc) but $AGENT_ID is gone from the $SERVER list"
  else
    DESTROYED=1
    row destroy pass "TEARDOWN-GONE: agent $AGENT_ID destroyed and absent from the $SERVER list (zero residue)"
  fi
fi

# ── stage 8: fold — the honest verdict ───────────────────────────────────────
"$BIN" plane fold --file "$EVIDENCE"
exit $?
