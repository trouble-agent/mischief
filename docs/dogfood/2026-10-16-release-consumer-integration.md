# Dogfood run 16b — the release-consumer path (README Option A), first drive

- Lane: mischief-chaos-dogfood, tick mischief-chaos-dogfood-2026-10-16-14-13-18
- Date: 2026-10-16. Surface: **the released v0.1.0 artifact driven through README
  Option A verbatim** — download → checksum verify → unpack → PATH install →
  smoke (version/status/doctor) → the documented plan replay → the L1 recipe
  selftest. This is the angle NO prior tick took: every previous install leg
  built from source (`make bin`) and ran repo-built binaries; the "non-Go user
  downloads the tarball" path had zero real-use coverage across 15 runs.

## Why now (delta vs last entry from THIS lane)

Last entry from this lane ended at tick 13 (HEAD eb3fcb3). Product code is
unequal to nothing since: eb3fcb3..f7f29be contains only board/dogfood-doc
commits (bf74179 board rows, dd6bcb8 + f7f29be sibling dogfood artifacts) —
zero product delta. Sibling run 16a (commit c2b8ea6 + bd708aa, log "16th tick
(lane mischief-chaos-dogfood) — REAL apply half …") drove the REPO-BUILT
lifecycle verbs (install/uninstall/retention) on repo-built binaries; this run
drives the artifact a real consumer downloads. No overlap; cross-cited, not re-swept.

## Promise statement

"This project claims that a user without a Go toolchain can install and run
mischief v0.1.0 from the release tarball by following README Option A."

## Results by leg (dev host + fresh agent df-mc-1016 on bunker-mvp)

### Dev host /tmp/df-r16-release (consumer scratch)

- Download tarball 0.64s, checksums.txt 2.96s; `sha256sum -c` OK rc=0 —
  the README's arm64-grep caveat prevented the classic ERROR trap.
- Untar: the four documented files, exactly as listed.
- `install -m 0755 mischief ~/.local/bin/` rc=0 (dev host masks the finding:
  ~/.local/bin already exists here).
- `mischief version` → `mischief v0.1.0` rc=0 (release-stamped, as documented).
- `mischief status` rc=0 — folds hold b7e9f744dfb5a67d `armed`,
  ttl_remaining=-49h43m27s (expired ~2 days past Oct 14 TTL): consumer-visible
  echo of MISCHIEF-DF-30's stale-view family on the released binary.
- `mischief doctor` rc=1, `catalog load FAILED: catalog/faults not found from
  cwd upward` — exactly the README's own caveat, message matches docs.
- `MISCHIEF_SANCTION=1 mischief plan -f catalog/.../ex-001...yaml` rc=0,
  0.00s: full preview (F-008 ready, landed-proof, inverse, probes, budget).
  **But the README's `plan` bullet promises "creates a run dir and journal
  entry" — the journal is 0 bytes, mtime predates the run, and the printed
  run_id 84c8a84cecf315c1 appears NOWHERE on disk.** Parity probe: same plan
  with the repo-built HEAD binary → same behavior (journal untouched).
  Source contradicts the doc wording: internal/plan/plan.go:13 "does not
  land, arm or journal: zero side effects by construction" — and the same
  README's docs table agrees ("plan (zero side effects, AC-1)").
  → MISCHIEF-DF-37 (P2), the docs-vs-code split inside one README.
- Actually-used-surface note (dev host): the plan runs rode cwd
  /home/kara/mischief, so the catalog resolved in-repo.

### Fresh agent df-mc-1016 (bare Debian user, x86_64, 1h TTL)

- gh MISSING, curl/cc/gcc/tar present, /tmp writable. Download 0.53s +
  0.45s, checksum verify OK, untar OK.
- **`install -m 0755 mischief ~/.local/bin/mischief` rc=1: "cannot create
  regular file: No such file or directory" — ~/.local/bin does not exist
  for a fresh consumer and the README never says to create it** (the CLI
  stop-history: version/status/doctor all rc=127 until rescue). One-line
  consumer rescue: mkdir -p ~/.local/bin, install rc=0.
  → MISCHIEF-DF-36 (P2): the silent ~/.local/bin assumption.
- Smoke after rescue (timed): version 0.01s cold, status 0.01s (holds 0,
  fresh boot_id), doctor rc=1 with the same documented catalog caveat.
- Shim placement (README's own aside): mkdir /tmp/mischief-shim + install
  the tarball's libfault-anchored.so rc=0.
- L1 recipe verbatim (reason-bearing marker file + MISCHIEF_SANCTION_FILE) →
  `mischief selftest --all` **rc=1 (0.53s)**: 13 PASS + clean capability
  skips, BUT `F-008 FAIL: unknown primitive "F-008": not in the selftest
  corpus (the catalog ids it covers)`. Discriminating probe, one variable
  (catalog vintage): shipped the tag's OWN catalog (git archive v0.1.0, 10
  fault files vs HEAD's 11) → same binary, same agent: **rc=0, selftest
  contract green-or-skip holds.** The release pairing is coherent; the
  README's own advice ("run it from a checkout" / --catalog-dir at a
  checkout) steers Option A users exactly into the catalog-newer-than-
  binary match that fails. F-008 was added to the catalog after the Oct 7
  tag (10→11 files). → MISCHIEF-DF-38 (P2): release/consumer versioning
  skew surfaces as a product-shaped failure with no version-skew hint.
- status after the run: holds 0, proofs 0, clean.

## PERF (step 2b)

- plan cold (agent, first run with catalog present): 0.01s. Warm ×5: 0.01s
  each (wall, /usr/bin/time). Dev host: plan 0.00s warm. selftest --all on
  agent: 0.53s cold, 0.65s with the tag catalog (identical modality).
- Nothing a user would wait on; **no PERF rows filed deliberately** (a win
  nobody can feel is not a finding).

## Value judgment (run 16b)

- **Does it work?** Yes on the released artifact: install, smoke, plan
  replay, selftest (with matching catalog) all hold the documented
  contracts; checksums, stamp, and the catalog-caveat message are exactly
  as documented. The three P2s are friction, not blockers.
- **Useful?** Yes — this is the only install path that avoids the Go
  toolchain entirely, and it holds.
- **Usable?** Time-to-first-success: 3 stalls (blocker-level for a fresh
  consumer): (1) rc=1 install on a missing ~/.local/bin + three rc=127
  smokes (rescued: mkdir), (2) plan/catalog on-agent steps requiring a
  checkout the README mentions only in a doctor caveat, (3) selftest red
  with a HEAD catalog despite the option-A install. All three are one
  fix each, and all three live in the README Option A section.
- **Trustworthy?** The checksum c's, exit codes, and status folding held
  everywhere. The stale armed hold (DF-30 family) is the only observed
  display that misrepresents state to a consumer.
- **Fast enough?** Comfortably (measured above).

**Verdict: 🟡 PROMISING-BUT-ROUGH on the release-consumer surface — the
released artifact itself is solid and honest, and Option A's mechanical
path works once rescued; the three README Option A frictions are
collectively the only thing between a non-Go user and first success.**

## Artifacts left

- docs/dogfood/2026-10-16-release-consumer-integration.md (this run's full
  integration report; agent transcript verbatim).
- docs/dogfood/diagnostics.md, extended with the release-consumer arc.
- skills/mischief-usage/SKILL.md: "driving the released artifact" section.
- Board: MISCHIEF-DF-36 / 37 / 38 (P2s, format+namespace matched, ids
  censused: 35 max pre-append) + EVT-1017 on MISCHIEF-DF-30 (aligned
  consumer-visible echo, row byte-untouched).
- Ephemeral agent df-mc-1016 (bunker-mvp, 1h TTL): destroyed at end of run
  (bunker destroy df-mc-1016; destroy leg verified from bunker list).
- Scratch: /tmp/df-r16-release (dev), agent $HOME/df-r16 + df-r16-v010
  catalogs (agent destroyed, no residue).
