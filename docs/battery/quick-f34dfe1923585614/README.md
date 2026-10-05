# mischief battery — quick-run evidence (MSF-011)

This directory is the LIVE `mischief battery --quick` run that proves the
SPEC-10 parity surface executes end to end (S-5's artefact half; PRD §12).

- run id: `f34dfe1923585614` (content-derived; `journal.RunID` over the cell
  set, the SPEC-02 derivation family)
- built at: commit wt/MSF-011 (this tree), `go build ./cmd/mischief`
- command: `mischief battery --quick --file-rows --dir <run-dir> --out <run-dir>`

Contents:

| file | what it is |
|---|---|
| `matrix.json` / `matrix.md` | the fault × detection × recovery artefact (S-5); the parity table names the five bunker-qa.sh chaos cells and the primitive replacing each |
| `battery.jsonl` | the run journal (SPEC-02 record format; scrub-on-write) |
| `findings.jsonl` | AC-18 rows — EMPTY on this run (4 pass, 1 named skip, 0 fail: no adverse verdict ⇒ an empty file, not an absence) |

Outcome: `4 pass, 1 skip, 0 fail (of 5 cells)`. The skip is `chaos-shutdown`
→ `I-002`: the replacing primitive is tier L3 (docker API); on the
unprivileged L0 scratch harness the cell records the named AC-11 skip — the
replacement exists and is named, its live actuator arrives with the
container backend. The detection axis records the not-wired reason on every
cell: the trouble-ledger integration is future work (AC-14's Detector seam
is the assertion point; nothing fakes a live read).

Deletion of the five bash cells happens in a LATER milestone, after parity
is proven live — this artefact is the evidence that milestone cites.
