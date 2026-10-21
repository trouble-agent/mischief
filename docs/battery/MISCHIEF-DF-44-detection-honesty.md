# MISCHIEF-DF-44 — detection-contract honesty evidence

Date: 2026-10-21 (worktree wt/MISCHIEF-DF-44, base b3a07f8)

## The defect, measured

The shipped battery artefacts sell fault × detection × recovery, but the
detection axis is not measured: no production path constructs a detector.

Census over every committed quick artefact (jq, run in the worktree):

    == docs/battery/quick-c26bd0e3c51a85d1/matrix.json
    c26bd0e3c51a85d1 quick=true cells=5 states=not_wired note_len=126
    == docs/battery/quick-f34dfe1923585614/matrix.json
    f34dfe1923585614 quick=true cells=5 states=not_wired note_len=126

→ 10/10 cells across both committed runs record detection.state =
"not_wired", each with the contract reason ("no detector wired on this
cell: the trouble-ledger integration is future work (the Detector seam is
the AC-14 assertion point)"). matrix.md carries 5 not_wired rows per run.
Production wiring census: `NewRunner` (internal/battery/runner.go:315) is
the only constructor cmd/mischief/battery.go uses and it wires zero
detectors; `internal/observe.Ledger` (the real read-only assertion) is
constructed NOWHERE outside its own tests — its own package doc names the
production wiring "the fleet-rollout step" (v0.2).

## Chosen contract path (honest not-measured, not fake wiring)

The in-repo seam (battery.Detector) and the assertion engine
(internal/observe) both exist, but no cell carries an expect_record
declaration and no ledger URL/namespace exists in-repo — wiring a "real"
detector would have required inventing a network/service dependency and
fabricating detected rows. Rejected. Instead:

1. The user-facing artifacts now say detection is NOT measured:
   - docs/BATTERY-PARITY.md: new section "Detection axis: NOT measured
     in v0.1" — only `detected (observer)` is a detection claim; AC-14
     named as OPEN at the integration level despite MSF-011's closure.
   - README.md: the battery verb row now states the detection axis is
     not measured in v0.1.
   - cmd/mischief/battery.go doc comment: same statement, test-pinned.
2. The artefact render distinguishes the states (internal/battery/
   artefact.go): a detected row renders "detected (observer)" — a bare
   "detected" without an observer name is no longer renderable; a state
   legend line after the matrix table defines detected / gap /
   not_wired ("NOT measured") / error, and states only `detected` is a
   detection claim.
3. The runner refuses the false-green at the source (internal/battery/
   runner.go detect()): a wired detector whose Name() is empty records
   state "error" with a reason naming the unattributable wiring defect —
   it can never produce a "detected" entry.
4. Landing/recovery behavior untouched: recoveryEntry, gradePass,
   gradeFail, skip handling and finding derivation are byte-identical
   (only the detect() guard and the markdown render changed).

## Regression coverage (the four coupled pins)

internal/battery/detection_honesty_test.go — all PASS:

    TestNewRunnerWiresNoDetector            — constructor wires none;
                                              fails when a real detector
                                              lands, forcing the docs/
                                              artefact reconciliation to
                                              move with it
    TestQuickMatrixEveryCellRecordsNotWiredWithReason
                                            — every quick-matrix cell:
                                              not_wired + contract reason
                                              + empty observer
    TestArtefactRendersStatesDistinctly     — markdown: note stated,
                                              "detected (trouble-ledger)"
                                              renders with observer, the
                                              state legend names all four
                                              states
    TestUnnamedObserverCannotClaimDetection — unnamed detector ⇒ "error"
                                              with reason, never detected
    TestCommittedQuickArtefactsPinNotWiredEvidence
                                            — every docs/battery/quick-*/
                                              matrix.json: not_wired cells
                                              with reasons + non-empty
                                              detection_note

## Verification runs (this worktree)

    go build ./...                       → OK
    go vet ./...                         → OK
    go test ./... -count=1               → all packages ok EXCEPT
                                           internal/backend_docker
                                           TestLiveNoScratchLeak —
                                           PRE-EXISTING: fails
                                           identically on the stashed
                                           clean tree (12 leaked
                                           mischief-l0-* scratch
                                           containers from earlier runs
                                           on this shared host; unrelated
                                           to this diff)
    go test ./cmd/mischief/ ./internal/battery/ -count=1 → ok
    go test ./internal/battery/ -run Detection -count=1 → ok (incl. the
                                           pre-existing AC-14 seam tests)
    gofmt -l internal/battery/ cmd/      → clean (three OTHER pre-existing
                                           unformatted files in internal/
                                           left untouched: ops/retention.go,
                                           reverter/record.go,
                                           sanction/sanction_test.go)

## Residual gaps (honest, not fixed by this row)

- Detection stays unmeasured until the v0.2 fleet-rollout step constructs
  an observe.Ledger per experiment and wires it into Runner.Detectors;
  AC-14's integration proof (a scratch trouble instance + a matched
  observe_result journal record) does not exist in v0.1.
- MSF-011's closed acceptance ("AC-14 detection assertion against
  trouble's ledger") overstates what shipped: the SEAM shipped; the
  assertion did not. This row does not reopen MSF-011 — it pins the
  honest state so the gap cannot silently revert.
- matrix.md files under docs/battery/quick-*/ are historical run
  evidence and were left byte-identical (they honestly record not_wired
  already); the render change affects future runs only.
