# Dogfood integration report — 2026-10-23 (run 31, tick mischief-dogfood-2026-10-23-04-45-49)

HEAD `162bc20d64bb62074cc1c62cc7cf259818243af9`, bin rebuilt on the checkout
(`make bin` → `bin/mischief version 162bc20d64bb`; the pre-tick bin was stale at
5233f4f43387/Oct 20 — always `make bin` before driving HEAD). Fresh-machine leg on
ephemeral bunker agent 6d7d3843 (bunker-mvp), substitute = `git archive HEAD` tarball
(git-archive named-as-such: the forge fetch was NOT proven — see below).

## The promise, tested as a user

"Declare a fault, land it in a named target, prove it landed, hold it under a TTL the
serve reverter reverts, and get a verdict on whether the target recovered."

Held, end to end, twice:

1. **Main host loop (sanctioned scratch):** `arm S-001` → hand-landed (`printf FAULTED`),
   `serve` background → watchdog reverted at **TTL+~4s** (declared 15s; fold is a ~5s tick,
   re-confirms the usage skill's window): journal `revert_proof outcome=reverted
   reason=ttl_expiry detail=check: same duration=219µs`, target restored to `original`.
   arm 9ms, plan 7ms, status instant.
2. **Fresh-box loop (agent 6d7d3843):** untar → `make bin` (4s warm-toolchain, incl. module
   fetches 38s on FIRST go invocation) → version/doctor/status ok → unsanctioned `plan`
   refuses rc=2 with the SPEC-13 text and zero writes → sanctioned `arm` rc=0 → `revert --all`
   `1 holds reverted, wall=1ms` rc=0.

Plus per-verb: battery --quick 5/5 pass 1.9s; selftest --all green-or-skip rc=0 (10.2s);
`revert --retry` recovery leg (arm → sabotage backup → plain revert rc=1 revert_failed →
--retry rc=1 → heal (mv + chmod 644) → --retry rc=0 reverted, content restored);
retention --dry-run honest; chaos matrix 8/11 executable at tier-max 1; sim fallback
landing (AC-17) with per-shape request log; `plan --journal-dir` appends run_planned (opt-in
zero-side-effect honored); `plan --json` snake_case (DF-26 contract).

## Errors hit (a user's list)

- `arm` kind/gate refusals disagree with the usage skill's rc table (rc=1 not rc=2) →
  **MISCHIEF-DF-71**.
- `arm` usage refusals mint `boot.json` in the run dir despite promising "nothing was
  written" → **MISCHIEF-DF-72**.
- battery's journal goes to `~/.mischief/runs/default/battery.jsonl` regardless of `--out`
  → **MISCHIEF-DF-73**.
- sim I-004's "proof" mixes fault families (2x malformed-5xx + 1x endless-page) without
  per-line attribution → **MISCHIEF-DF-74**.
- Known open nits re-verified live at HEAD (no new rows, already open):
  DF-57 (protected-target refusal names a nonexistent override flag — reproduced verbatim),
  DF-46 (no per-cell time fields in matrix.json — keys enumerated), DF-70 (pinned --log
  appends). NOTE for DF-70: the per-line summary NOW carries the shape in the text but the
  ac17-redirect line still reads as per-attempt proof; unchanged premise.
- Usage-skill coaching re-verified: sanction-env leak hazard (real — hit it myself in the
  transposed-var leg), retry admission rules, absent-dir JSON shape, kill-switch parse
  `outcome=` — all still accurate at HEAD. The rc=2 claims on kind refusals are the one
  stale bullet → DF-71 (filed, not edited: lane contract).

## What the board says vs. reality (premise checks, evidence for the foreman)

- **QA-MISCHIEF-005** (open, failed): `TestOwnerSurvivesKill9AndRevertsAtTTL` PASSES
  natively at HEAD (`go test ./internal/reverter -run ... -count=1` → ok 5.85s) and the full
  `go test ./... -count=1` is green (21 pkgs, RC=0) — the row's failure premise does not
  reproduce at 162bc20d.
- **QA-MISCHIEF-006** (open, pending): the documented `MISCHIEF_REVERTER_DETACH_MODE` knob
  export + `go test ./internal/reverter -count=1` → ok 12.8s; ambient env is clean — the red
  does not reproduce at HEAD.
- **MISCHIEF-DF-63** (open): `make row-drift` PASSES (31 row refs, no closed row cited as
  current) — the AC list just landed on 162bc20 as this tick's acceptance commit; the gate
  exists and is wired (`make row-drift`). The rc=2 coaching bullet (DF-71) is what the gate
  cannot see (it checks row status, not measured rc claims).

## Fresh-machine (bunker) leg

- Host bunker-mvp ONLINE 8/50; agent 6d7d3843 spawned (ttl 1h, ~40s), destroyed cleanly
  (TEARDOWN-GONE, key removed, list re-check empty).
- Substitute disclosed: `git archive HEAD` tarball (992KB, 18 top entries) — the ssh
  cross-host clone (kara@100.99.183.108) is refused by agent-key transport (publickey;
  the agent's per-agent key has no auth to the dev host, correct), and origin
  (github trouble-agent/mischief) is private-first — so **forge fetch NOT proven**;
  the fresh-toolchain build path (go1.26.9 auto-download + go build, 38s first-touch /
  4s warm) + smoke IS proven. No credentials minted, no visibility changes.
- Two failed leg-script runs were MY script bugs (rm glob ate the tarball; tarball
  re-copy raced the cleanup line), not product behavior — disclosed here.

## Verdict

**SHIPPABLE** (loop works, honestly, fast, on a fresh box), with 4 new usability/honesty
rows above for the foreman. Time-to-first-success reading only the README's Option B:
~2min on a toolchain box (clone→make bin→smoke), zero re-reads of source.
