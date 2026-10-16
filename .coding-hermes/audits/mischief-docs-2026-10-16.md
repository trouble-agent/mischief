# Mischief documentation audit — 2026-10-16 (mischief-docs lane tick)

Scope: source-derived verbs/flags, env-var surface, catalog counts, four reader
audiences, rot. Repo `/home/kara/mischief`, HEAD `1e52b88` (clean tree except the
in-flight MISCHIEF-DF-30 worker patches). Evidence only — no docs were edited.
Result: **NOTHING-TO-DO (0 new rows filed)** — every gap found was already a live
board row owned by another lane, and the one latent issue is a deploy artifact in
flight.

## Commands and literal observations

1. `$ make bin && ./bin/mischief version`
```
CGO_ENABLED=0 go build -ldflags '-X main.gitSha=1e52b887d090' -o bin/mischief ./cmd/mischief
mischief 1e52b887d090
```
2. `$ ./bin/mischief --help` — top-level usage lists: plan, status, arm, revert,
   doctor, selftest, serve, install, uninstall, audit, retention, battery, chaos, sim, plane, version.
   `selftest` (MISCHIEF-DF-20, missing from help) has landed; `arm` is documented
   at README "Arming a hold" §. Both stale rows' premises are now false — no new filing.
3. Env-var completeness: `$ grep -rnoE 'MISCHIEF_[A-Z_]+' --include='*.go' --include='*.c' internal cmd tools | grep -v _test | sort -u` →
   `MISCHIEF_REVERTER_{DIR,HOLD,EXPIRY,DETACH_MODE,SYSTEMD_RUN_FAKE}` only
   (SANCTION/SANCTION_FILE counted separately, README-covered).
   `$ grep -oE 'MISCHIEF_[A-Z_]+|MCFAULT[A-Z_]*' docs/reference/operator.md | sort -u` →
   the reverter five are present; the shim trio (`MCFAULT`, `MCFAULT_BUDGET`,
   `MCFAULT_PROOF`, source `internal/backend_signal/rule.go:205-219`) is documented under
   "Shim (backend shim)". Operator reference is COMPLETE vs source. DOC-10 closed-loop verified.
4. Catalog counts recomputed against descriptor YAMLs, not eyeballed:
   `$ ls catalog/faults/*.yaml` → 11 files
   (C-009 F-008 F-009 I-002 N-001 N-012 P-001 R-001 S-001 S-004 T-001);
   `$ for f in catalog/faults/*.yaml; do … grep -cE "\| ${id} " docs/FAULT-CATALOG.md; done`
   → every in-repo descriptor has exactly its FAULT-CATALOG.md row.
   `$ grep -oE '\| [A-Z]-[0-9]{3}' docs/FAULT-CATALOG.md | sort -u | wc -l` → 104
   (P14 S14 R12 F16 N18 C12 I14 T4 — matches the detail line at `README.md:43`
   and `internal/catalog` per `docs/CATALOG.md`). README-4's 104-vs-113 count
   finding is accurate as filed (113 counts the 9 sim-case rows).
5. Every shipped subcommand answers its own `--help` (spot: plan, doctor, selftest,
   battery, chaos, plane, install). README/MISCHIEF-CLI-001 premise stale — correct as filed.
6. Release artifact verified live: `gh release view v0.1.0 --repo trouble-agent/mischief`
   → published (non-draft), 3 assets present; README install path is real (option A).
7. Decommissioned-verb reconciliation (DOC-9 class): PRD §6.1 no longer advertises
   `run`/`catalog`/`monkey`/`mischiefd` conflicts against the shipped dispatch
   (`./bin/mischief run --help` → `mischief: unknown verb "run"`); the worker's Doc
   9 reconcile landed. No reopening.
8. Deploy skeleton latency (NOT FILED): `$ ./bin/mischief install --help` and
   `uninstall --help` crash with
   `internal/daemon/daemon.go:243:13: d.lf.StatusSnapshot undefined (type *reverter.Lifecycle has no field or method StatusSnapshot)`.
   That skeleton is artifact of the uncommitted MISCHIEF-DF-30 fix in the tree —
   NOT a shipped state. Pulled out of the way: `git stash` → `make bin` green →
   `git stash pop` (DF-30 patches restored).
9. Board state: `boardctl -C /home/kara/mischief validate` → `RESULT: OK (173 warning(s))`
   (legacy-shape event-with-pending warnings); 8 pending rows, all P2+ live functional
   findings (DF-24..DF-38, RELEASE-010) — none doc-born.
10. Skill trigger check: `internal/reverter/owner.go:168` guards
    `MISCHIEF_REVERTER_SYSTEMD_RUN_FAKE` (tests-only binary-probe env) and
    `docs/reference/operator.md` mentions the variable's service contract in prose — covered.
11. Rot sweep: audit lane URL doc unchanged; no new refs into removed sources
    (git grep for stale refs to `probe/libfault-probe.c` as shipped stays historical-only
    per `docs/RELEASE.md`). No new rot findings this tick.

## Re-triage (pre-2026-09-30 P1-P3 doc rulings)

Doc/README rows older than the ruling are all `complete` (verified in the tasks.jsonl
scan); no P1-P3 doc-rot rows remained to re-file, so the 2-row budget is unspent.

## Unverified

- Did not run selftest/battery or arm any fault — out of scope for a docs audit by
  the safety contract.
- Did not verify the HTML twins (`docs/*.html`) regenerate byte-consistently from
  the `.md` via `tools/gen-docs.py` this tick (prior audits did; no .md source changed
  since).

## Deferred (watch item for next tick)

- If the MISCHIEF-DF-30 wave closes without landing `StatusSnapshot`, the release
  tarball's documented `mischief install` command will refuse at package load.
  File then at P4 (doc severity by ruling — the bug itself is DF-30's P1).

ch:trace row=none:nothing-filed spec=docs/prd/mischief-v0.1.md#61-verbs wave=none:standalone-audit test=none:documentation-audit doc=none:n/a prompt=~/.hermes/skills/coding-hermes-docs/SKILL.md evidence=.coding-hermes/audits/mischief-docs-2026-10-16.md witness=none:tick-surface-diff-clean verdict=none:nothing-to-judge commit=none:audits-only memory=none:not-written
