# DOC-17 verification evidence

Scope: documentation-only update to `docs/reference/operator.md`.

Source/spec anchors:

- `internal/sanction/sanction.go`: `EnvSanctionValue`, `DefaultMarkerPath`,
  `markerPath`, `parseReasonLine`, and `Check`.
- `internal/sanction/sanction_test.go`:
  `TestSanctionRefusalTable`, `TestRefusalTextNamesHostAndMissingMarker`,
  `TestRefusalNamesEmptyMarkerReason`, and `TestSanctionCheckWritesNothing`.
- `docs/SPEC-PLAN.md` §SPEC-13, especially the sanction marker table and
  refusal contract.
- `README.md` §"First sanctioned run (the L1 container recipe)" and
  `docs/TEST-PLANE.md` §3.

Verification command:

`env -u MISCHIEF_SANCTION -u MISCHIEF_SANCTION_FILE go test ./internal/sanction -count=1` → PASS (`ok`, 0.003s).

Repository gate:

`go build ./... && go vet ./... && env -u MISCHIEF_SANCTION -u MISCHIEF_SANCTION_FILE go test ./... -count=1 -short -parallel 4` → PASS (all listed packages green).

Census command and result:

`python3 -c '...required clause and source-constant census...'` → PASS: `doc_vars=True source_constants=True required=15/15 old_sentence_present=False missing=[]`.

`git diff --check` → PASS.

Evidence path: `docs/verification/DOC-17.md`.

ch:trace row=DOC-17 spec=docs/SPEC-PLAN.md#SPEC-13 test=go-test-./internal/sanction evidence=docs/verification/DOC-17.md witness=none:repository-local-source-and-test-proof verdict=none:pending commit=none:pending wave=mischief-foreman-2026-10-24-14-52-45#DOC-17 memory=none:not-written
