# MISCHIEF-DF-43 — verification artifact (foreman, wave mischief-foreman-2026-10-19-22-15-21)

Row: MISCHIEF-DF-43 — the finding rows emitted by `battery --file-rows` and `chaos finding`
carried ids the fleet board tooling refuses (`MSF-BAT-mischief/F-009`, `MSF-CHAOS-2dfb1056d701`).

## Landed change

- Worker commit `ea15d6f` (worktree `/home/kara/worktrees/mischief-MISCHIEF-DF-43`), merged no-ff as
  `8e38d5f`.
- New `internal/battery/findingid.go` mints ids for BOTH emitters: project/cell components are
  normalized into UPPERCASE hyphen-separated tokens, the chaos content hash is uppercased.
  `internal/battery/runner.go` and `internal/chaos/lane.go` are rewired to it.
- Tests: `internal/battery/findingid_test.go` (`TestFindingIDBoardCompatible` incl. the observed
  malformed shape `mischief/F-009`, `TestFindingIDDeterministicAndDistinct`,
  `TestBatteryFindingIDRepresentativeCells`), `internal/battery/filing_test.go`,
  `internal/chaos/lane_test.go`.

## Foreman live + format verification (merged tree)

```
$ /tmp/mf-bin chaos finding --project mischief --board /tmp/mf-check-43 \
    --title 'battery finding: F-009 on mischief (not_recovered) - probe' \
    --reason 'foreman tick check' --priority P1 --primitive F-009
chaos finding: MSF-CHAOS-MISCHIEF-71BE7A811AAA filed on /tmp/mf-check-43/tasks.jsonl (1 row)

fleet id law ^[A-Z][A-Z0-9]+(-[A-Z0-9]+)+$ against the shapes:
  PASS     MSF-CHAOS-MISCHIEF-71BE7A811AAA      (new chaos emitter, live)
  PASS     MSF-BAT-MISCHIEF-F-009                (new battery emitter shape)
  REFUSED  MSF-BAT-mischief/F-009                (the old slash/lowercase shape)
  REFUSED  MSF-CHAOS-2dfb1056d701                (the old lowercase-hex shape)
```

Gate on the merged tree: `go build ./...`, `go vet ./...`, `go test ./... -count=1` all clean/green.
