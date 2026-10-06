# mischief battery — quick-run evidence (MSF-016)

This directory is the LIVE `mischief battery --quick` run that closes the
MSF-016 (M4) parity proof on the host that executes the deletion: all five
parity cells ran on the L0 scratch harness with measured landed-proofs —
5 pass, 0 skip, 0 fail.

- run id: `c26bd0e3c51a85d1` (content-derived; `journal.RunID` over the cell
  set, the SPEC-02 derivation family)
- built at: commit wt/MSF-016 (this tree), `go build ./cmd/mischief`
- command: `mischief battery --quick --file-rows --dir <run-dir> --out <run-dir>`
- host posture: docker daemon present, so `chaos-shutdown` → `I-002`
  executed its real docker cold-return actuator (the sibling run
  `quick-f34dfe1923585614` from MSF-011 shows the same matrix on a host
  WITHOUT the daemon: `I-002` records the named AC-11 skip there — both
  behaviours are the honest set)

Contents:

| file | what it is |
|---|---|
| `matrix.json` / `matrix.md` | the fault × detection × recovery artefact (S-5); the parity table names the five bunker-qa.sh chaos cells and the primitive replacing each |
| `battery.jsonl` | the run journal (SPEC-02 record format; scrub-on-write): `run_planned` → per-cell `fault_landed`/`fault_reverted` proofs → `verdict` → `run_closed` |
| `findings.jsonl` | AC-18 rows — EMPTY on this run (5 pass, 0 fail: no adverse verdict ⇒ an empty file, not an absence) |

Verdict summary (the commit body's evidence):

    q1     N-012  PASS  recovered [replaces chaos-disconnect]
    q2     I-002  PASS  recovered [replaces chaos-shutdown]
    q3     F-009  PASS  recovered [replaces chaos-corruption]
    q4     R-001  PASS  recovered [replaces chaos-resource]
    q5     F-009  PASS  recovered [replaces chaos-errorpath]
    battery: 5 pass, 0 skip, 0 fail (of 5 cells)

Every cell's verdict rests on a measured landed proof (proxy hit-ledger,
container exit-137 + same-address return, inode identity, cgroup counter)
— never on an exit code (AC-13 / S-1's contract; the no-landed-proof arm
grading no_op is pinned by `TestS1ReplayNoLandedProofGradesNoOp`).

Deletion of the five bash cells in `~/…/bunker-qa.sh` is a
FLEET-SIDE follow-up (the file is outside this repo); it executes the
checklist in `docs/BATTERY-PARITY.md`.
