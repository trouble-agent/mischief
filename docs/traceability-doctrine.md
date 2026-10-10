# Traceability doctrine (ch:trace)

Every claim this repo closes — a board row, an audit finding, a release, a fix —
carries a `ch:trace` marker: one line that links the claim to the spec it came
from, the run that produced it, and the evidence that proves it. The marker is
the contract. If a claim cannot carry one, it does not close.

```
ch:trace row=<board-row> spec=<path#anchor> wave=<manifest#step> \
         test=<path::Test> evidence=<artifact> witness=<surface> \
         verdict=<id> commit=<sha>
```

Fields, in the order a reader needs them:

| field | what it names | real example from this repo |
|---|---|---|
| `row=` | the board row or finding id (REQUIRED) | `row=MSF-010` |
| `spec=` | the promise the work implements | `spec=docs/SPEC-PLAN.md#SPEC-09` |
| `wave=` | the dispatch that produced it | `wave=.coding-hermes/waves/mischief-foreman-2026-10-05-10-40-40.json#MSF-011` |
| `test=` | the test (or suite) that gates it | `test=cmd/mischief/sim_test.go::TestSimVerbEachShapeProvesInRequestLog` |
| `evidence=` | the artifact the run emitted | `evidence=docs/battery/quick-f34dfe1923585614/matrix.json` |
| `witness=` | a surface we do not control that shows it | `witness=http:200+ci-run-37652576697` |
| `verdict=` | an independent judge's ruling | `verdict=d816dcec` |
| `commit=` | the commit that carries the work | `commit=5d79f52` |

Optional fields seen in practice: `doc=` (the reader-facing page),
`prompt=` (the procedure followed), `memory=` (a DuckBrain key, never instead
of a repo-local field), `witness2=` (a second surface). Markers live in board
rows (`.coding-hermes/board/tasks.jsonl`, `events.jsonl`), close events, audit
findings, and as git commit trailers.

## Rules

1. **`row=` is required.** Every marker hangs off a board row. No row, no claim.
2. **Every closed claim carries a witness.** Either a real `witness=` surface,
   or `witness=none:<reason>` — a named reason. `witness=none:tier2-judge-402-balance`
   is honest; a missing witness is silence, and silence is not a reason.
3. **T3 is the evidence floor for "done".** Evidence classes:
   - **T1 — reference.** A doc, a row id, a verdict id. Points at the claim;
     proves nothing.
   - **T2 — reproduction.** A command anyone can rerun, with its output quoted
     in the claim.
   - **T3 — invocation.** The thing itself was invoked and left an artifact the
     run produced: a battery matrix, a selftest journal, a CI run id, release
     assets with a matching checksum.
   A claim closes at T3. T1/T2 evidence supports; only T3 proves.
4. **No claim closes by reference alone.** `evidence=<another row>` or
   `evidence=<a report that itself cites rows>` closes nothing — that is a
   box in a box. Follow the chain and the last link must be an artifact or a
   witness surface, not another pointer.
5. **A `verdict=` binds to a `commit=`.** A judge ruling attaches to the commit
   it judged, and that commit must be reachable from the pushed tip. A verdict
   on an unmerged or unpublished commit is not evidence.
6. **Unfilled fields say why.** Any field that cannot be filled becomes
   `none:<reason>` (`verdict=none:not-judged`, `commit=none:open`,
   `wave=none:standalone-audit`). A stated gap is readable; an empty field is a lie.

## Witness kinds

A witness is a surface **we cannot write** — the world outside this repo:

- `release:<tag>@origin` / `tag:<tag>@origin` — what actually shipped
  (`witness=release:v0.1.0@origin`, `witness=tag:none@origin`)
- `http:<code>+<object>` — a URL that returned, naming the object
  (`witness=http:200+ci-run-37652576697`)
- `live:<endpoint or command>` — a live system resolved now
  (`witness=live:gh-release-v0.1.0`, `witness=live:mischief-selftest---all-exit-0`)
- `path@origin:<path>` — a file at the pushed ref, not the local tree
  (`witness=path@origin:internal/doctor/json.go`)

Prefer a witness over prose whenever the claim is about the world rather than
about our own output.

## How markers are verified

**Workers** put the marker in the commit trailer and the row summary before
committing — with the evidence path they actually produced, not the one they
hope exists.

**Foremen** verify at close: the `evidence=` path exists in the tree or run
dir; the class is T3 (or the gap is named via `none:`); the `witness=`
resolves out-of-band (re-check the CI run, the release, the tag); `verdict=`
and `commit=` agree and the commit is on the pushed tip. A close that fails
this check is not closed — it is re-opened with the missing field named.

**Judges** verify independently: they re-run the acceptance against the
`commit=`, not against the row's prose. The marker tells them where to look;
it never substitutes for looking.

**Auditors** sample the chain end to end: row → spec → wave → test →
evidence → witness. A chain that ends in another pointer, or a witness that
does not resolve when fetched, is a finding — filed as a row, with its own
marker.
