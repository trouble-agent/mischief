# MISCHIEF-DF-67 — verification artifact (worker, wave mischief-foreman-2026-10-21-18-18-09 #3)

Row: MISCHIEF-DF-67 — the SPEC-13 sanction gate covered plan/arm/selftest/battery
only; `sim` armed shapes, served faults and wrote request-log landed proofs
unsanctioned (rc=0), and `chaos matrix` ran with no recorded host posture.

## Landed change

- `cmd/mischief/sim.go` — SPEC-13 gate (`sanction.Check`) inserted AFTER usage
  validation (shape + destroy-class vocabulary) and BEFORE the AC-17 gate,
  the simulator start, the bind and every write: unsanctioned → stderr
  `rails: host_not_sanctioned: …`, exit 2, no request log, nothing armed.
- `cmd/mischief/chaos.go` — chaos matrix keeps its evidence-plane admission
  posture (it never arms a fault) but now stamps `host_posture`
  (`sanctioned` | `unsanctioned`) into the artifact, adds the same line to
  the markdown render, and names the posture in the summary line
  ("… executable at L0 on this unsanctioned chaos host").
- `internal/chaos/matrix.go` — `MatrixArtifact.HostPosture` field +
  `PostureSanctioned`/`PostureUnsanctioned` constants; `ProjectMatrix` takes
  the caller's posture (evidence recorded, not enforced).
- `internal/chaos/lane.go` — `ValidateMatrixArtifact` refuses a posture-less
  (pre-stamp) or unknown-value artifact: a lane consuming a matrix must know
  which host posture derived it; refusal names `host_posture` and the
  offending value, never silently defaults (null-with-reason law).
- `cmd/mischief/main.go` — safety-floor help no longer blanket-claims
  "fail-closed": it names the five gated landing verbs (plan, arm, selftest,
  battery, sim) and chaos matrix's evidence-plane posture explicitly.
- `docs/TEST-PLANE.md` — the marker bullet records both postures (DF-67
  measured date) so the plane doc matches the shipped behavior.

Source-truth note: the row preferred fail-closed sim (implemented) and an
explicit chaos-matrix posture. Chaos matrix is a derivation artifact —
SPEC-13 scopes host admission to processes whose work IS the fault; the
matrix never arms, binds or lands — so posture is RECORDED (artifact +
stdout) rather than enforced, matching the PRD's landing-verb list. The
posture-less legacy artifact is refused at lane validation, closing the
"silent unknown-posture matrix" hole.

## Regression tests (RED first)

`cmd/mischief/sanction_gate_test.go`:
- `TestSimRefusesUnsanctionedHost` — RED proof measured pre-fix: exit 0 with
  landed proof (the defect), GREEN post-fix: exit 2, stderr names
  `host_not_sanctioned`, request log NEVER created (os.Stat IsNotExist).
- `TestSimProvesOnSanctionedHost` — positive arm: marker present → exit 0,
  `ac17-redirect … proves it` (fail-closed, not fail-everything).
- `TestChaosMatrixPostureNamesHost` — unsanctioned: rc=0, summary names
  "unsanctioned chaos host", matrix.json carries `host_posture`.
- `TestChaosMatrixSanctionedPostureStamped` — sanctioned arm stamps
  `"host_posture": "sanctioned"`.

`internal/chaos/lane_test.go`:
- `TestValidateMatrixArtifactHostPosture` — both postures validate; blank and
  `blessed-by-vibes` refuse naming the field/value.

Existing tests updated for hermeticity only: the 7 `cmdSim`-exercising tests
in `cmd/mischief/sim_test.go` now provision `writeSanctionMarker(t)` (the
plane's own sanctioned shape); `internal/chaos` tests pass the sanctioned
constant to the new `ProjectMatrix` parameter. No assertion loosened.

## Live probes (this worktree build, /tmp only, unsanctioned env)

```
$ env -u MISCHIEF_SANCTION ./df67-mischief sim --shape I-003 --log /tmp/df67-probe-fresh/req.log
rails: host_not_sanctioned: host "karaHermes-mde-7840hs" is not sanctioned for mischief (SPEC-13): …
rc=2 ; req.log does NOT exist            # the landed-proof claim is gone
$ env -u MISCHIEF_SANCTION MISCHIEF_SANCTION_FILE=<tmp marker> … sim --shape I-003 --log /tmp/df67-probe-fresh/req2.log
ac17-redirect: I-003 refused the real plane and landed in the simulator: /fault -> 429 … proves it
rc=0 ; req2.log has the faulted entry    # sanctioned path unchanged
$ env -u MISCHIEF_SANCTION … chaos matrix --project mischief --tier-max 0 --out /tmp/df67-probe-fresh/mx
chaos matrix: mischief — 3/11 cells executable at L0 on this unsanctioned chaos host (artifact: …)
"host_posture": "unsanctioned"           # in matrix.json + matrix.md; sanctioned run stamps "sanctioned"
$ env -u MISCHIEF_SANCTION … plan -f catalog/experiments/ex-001-noop-detector.yaml
rc=2                                     # control: gate unchanged on plan
```

## Gates

`gofmt` clean on all touched files (3 flagged files are pre-existing,
untouched here); `go build ./...` clean; `go vet ./...` clean;
`go test ./... -count=1 -p 1` — every package ok (serial, full suite).
