# MISCHIEF-DF-47 — verification artifact (foreman, wave mischief-foreman-2026-10-19-22-15-21)

Row: MISCHIEF-DF-47 — the AC-17 real-provider gate's third leg was vacuous: any non-empty
`--resource` tag (whitespace, `*`, `aws:prod-db`, `prod-fleet-main-host`) admitted the real plane.

## Landed change

- Worker commit `fdd21a6` (worktree `/home/kara/worktrees/mischief-MISCHIEF-DF-47`), merged no-ff as
  `5e599fd`.
- New `internal/cloudallow` is the single authoritative allowlist: `SandboxTag = "aws:sandbox-lab"`,
  exact string match, no trimming/case-folding/globs, no host or network state; `CheckReason` names
  the offending tag and the allowlist contents.
- BOTH gates consult it: `internal/backend_sim/refusal.go` (`RealGate.Decide`) and
  `internal/rails/scope.go` (`checkL5Gate`); the allow-real and spend-cap legs are preserved.
- Tests: `internal/cloudallow/cloudallow_test.go`, `internal/backend_sim/allowlist_parity_test.go`
  (both gates agree on a 12-value probe), `internal/backend_sim/refusal_test.go`,
  `internal/rails/scope_test.go`, `cmd/mischief/sim_test.go`.
- Docs: `docs/SPEC-PLAN.md` L5 row and `docs/prd/mischief-v0.1.md` sim row now name
  `internal/cloudallow` and the exact v0.1 tag.

## Foreman live verification (merged tree, `sim --shape I-003 --allow-real --spend-cap 5`)

```
TAG[aws:sandbox-lab] -> backend_sim: I-003: real | ac17: real adapter is a v0.1 documented stub
TAG[aws:prod-db]     -> refused: 'not on the AC-17 allowlist; allowlisted: aws:sandbox-lab' -> simulator
TAG[*]               -> refused: 'not on the AC-17 allowlist; allowlisted: aws:sandbox-lab' -> simulator
TAG[   ]             -> refused: 'blank/whitespace is never allowlisted'                     -> simulator
```

Gate on the merged tree: `go build ./...`, `go vet ./...`, `go test ./... -count=1` all clean/green
(22 packages, incl. `internal/cloudallow`).

Judge note: the tier-2 judge for this row verified every one of the above clauses PASS on the first
pass and failed only the marker-bookkeeping clause of the criterion; this artifact plus the
`ch:trace` commit trailer and the corrected row marker are that remediation (see the row's
`worker_summary`).
