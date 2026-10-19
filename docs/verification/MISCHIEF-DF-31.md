# MISCHIEF-DF-31 — verification artifact (foreman, wave mischief-foreman-2026-10-19-22-15-21)

Row: MISCHIEF-DF-31 — `arm`'s write-ahead message advertised a containment route
(`Detach`) that had zero production callers.

## Landed change

- Worker commit `6123ce8` (worktree `/home/kara/worktrees/mischief-MISCHIEF-DF-31`), merged no-ff as
  `f7be3c7` by the foreman in wave `mischief-foreman-2026-10-19-22-15-21`.
- `cmd/mischief/arm.go` write-ahead line now reads exactly:
  `write-ahead: inverse + check durable in the journal (SPEC-04); serve reverts at TTL`
- Regression test: `cmd/mischief/arm_test.go::TestArmOutputNamesOnlySupportedRevertRoute`
  (RED against the old text, GREEN after the change).

## Foreman live verification (this host, merged tree)

```
$ grep -rn 'Detach' cmd/mischief/arm.go
(no output)                      # 0 hits — the unreachable route is no longer advertised

$ /tmp/mf-bin arm --fault S-001 --target scratch:/tmp/mf-check-31b --ttl 60s \
    --inverse-kind restore-file --inverse-params path=/tmp/mf-check-31b/f,backup=/tmp/mf-check-31b/f.bak \
    --check-kind file-absent --check-params path=/tmp/mf-check-31b/f --dir /tmp/mf-check-31b/run
armed f88415c811ae898b fault=S-001 target=scratch:/tmp/mf-check-31b ttl=1m0s dir=/tmp/mf-check-31b/run
write-ahead: inverse + check durable in the journal (SPEC-04); serve reverts at TTL
```

Gate on the merged tree: `go build ./...` clean, `go vet ./...` clean,
`go test ./... -count=1` — every package `ok`.

CI: run on the pushed HEAD is green (see the tick report for the run list).
