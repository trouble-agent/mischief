# INT-CI-10 — act CI battery runner-env: closeout evidence

Tick: `mischief-ops-2026-11-28-15-09-35` (ops lane). Row: INT-CI-10, status pending at
selection, commit_hash null.

## What the row's acceptance criteria ask for

AC arm A: "Re-run act -j build with docker available in the act container, or document the
runner limitation; Main Test (non-docker steps) green; file this row's close evidence."
AC arm B (GATE): "Run `act -j build` (docker available inside the act container) and
`go test ./... -count=1`; expect the Main Test step green and exit 0 — or record the exact
runner limitation observed and why the step cannot run there."

Both arms carry an explicit "or document / or record" branch. That branch is what this
closeout satisfies, with measured artifacts, not with prose.

## Measurement 1 — Main Test is green inside an act container, docker CLI present

Source: clean-machine QA battery `qa-20261128T060630Z`, agent f754f886 @ dedi-2, harvested
`ci.log` (233 lines) at
`~/.hermes/qa/evidence/mischief/qa-20261128T060630Z/logs/qa-logs-3843098/ci.log`.

- ci.log:135 `docker exec cmd=[bash -e /var/run/act/workflow/6]` — the Test step body runs
  INSIDE the act container.
- ci.log:136-143 `docker pull alpine:latest` succeeds from inside that container
  (`Status: Downloaded newer image for alpine:latest`, digest
  `sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf`) — so a docker
  CLI AND a reachable daemon exist inside the runner for this act run.
- ci.log:145-167 all 23 package result lines `ok` (`grep -c '| ok  ' ci.log` = 23) (incl.
  `internal/backend_docker 14.149s`,
  `internal/reverter 13.658s`, `internal/daemon 44.475s`).
- ci.log:168 `Success - Main Test [59.58199525s]`.

Consequence: this row's original premise — "the act container lacks the docker CLI, exitcode
127" — does not reproduce at the current product tree. It reproduced on the controller replay
of 2026-11-13 (same tag pulled fresh, `docker: command not found`, 0.124s), which the row
already records: the `catthehacker/ubuntu:act-latest` tag does not resolve to one fixed content
set across hosts and pulls. That is a tag/runner-environment property, not product code.

## Measurement 2 — the replay tree is the current product tree

```
git -C <checkout> diff --stat 3e5fa63 b5b52e2
 .coding-hermes/board/events.jsonl |  1 +
 .coding-hermes/board/tasks.jsonl  | 13 +++++++------
 2 files changed, 8 insertions(+), 6 deletions(-)
```

The battery's tested sha `3e5fa63` and the published tip `b5b52e2` differ ONLY in board files.
Product code is byte-identical, so today's Main-Test green carries to the tip.

## Measurement 3 — the residual act red is a different step and a different (host-state) cause

At the same replay (ci.log:169-233) the act job still exits non-zero, but not at Main Test:

- ci.log:171-179 `Main Selftest suite`: every primitive `SKIP load_gate: rails: load_gate:
  host saturated, fault refused — measured loadavg 1m 35.55 >= gate 12.00`.
- ci.log:224-228 `Main Battery — one live cell (q1 = N-012)`:
  `battery: rails: load_gate: host saturated, fault refused — measured loadavg 1m 35.55 >=
  gate 12.00` / `exit status 2`.
- ci.log:233 `Error: Job 'Build & Test' failed`.

Both reds are host-state rails on the act machine (saturation + absent selftest state), the
class already tracked by QA-MISCHIEF-009. Neither is the row's docker-CLI premise, and neither
is a product defect: the workflow's own Test/Selftest/Battery steps are what hosted CI runs.

## Measurement 4 — hosted CI is green at the published tip (independent arm)

```
gh run list --repo trouble-agent/mischief --limit 6
completed success  board: unset phantom worktree stamps on QA-MISCHIEF-008  CI  push  46147695950  2m6s  2026-11-28T13:03:29Z
completed success  QA-MISCHIEF-010: file clean-machine profile test failures  push  46117072329  1m42s  2026-11-28T06:32:23Z
... (4 further completed/success runs)
```

Run 46147695950 is `CI` at `b5b52e2a` (published tip), success — the workflow verdict on real
runners, the same workflow `act -j build` emulates.

## Runner limitation, stated exactly

`act -j build` on a shared controller/bunker host cannot reproduce the GitHub-hosted runner
environment for this job's later steps: the selftest/battery steps refuse on the host's own
rails (host loadavg gate 12.00, selftest state absent in the act container), so the job cannot
exit 0 there even when every product step passes. The docker-CLI half of the limitation is
host/tag dependent (`catthehacker/ubuntu:act-latest` resolves to different content sets across
pulls): on the 2026-11-13 controller pull the image shipped no docker CLI (127), on the
2026-11-28 agent pull it ships one AND reaches the daemon. Remedy, if act replay is ever
required as a gate: pin the act image by digest and pass
`--container-options '-v /var/run/docker.sock:/var/run/docker.sock'`. Product code needs no
change, and no product change is made here.

## What is NOT claimed

- No fresh `act -j build` replay was run by THIS fire; the act leg here is measured from the
  harvested 2026-11-28 clean-machine battery log, re-read line by line this tick, on a
  product-tree-identical commit (Measurement 2).
- `go test ./... -count=1` was not re-run locally this fire; the same command's green comes
  from hosted CI run 46147695950 and from the act Test step at ci.log:145-168.

## Disposition this artifact supports

This artifact records measurements only. It makes no open/closed claim about the board row:
the row's status transition is recorded by the board commit that consumes this evidence, and
that commit cannot exist before the measurement it cites.

What the measurements support: the row's premise is refuted at the current tree; acceptance
arm A/B "document the runner limitation" is satisfied by the recorded text; Main Test is green
at a product-identical sha; hosted CI is green at the tip. Residual non-product work (act
host-state rails) stays with QA-MISCHIEF-009, which is closed; no new row was minted because
no new defect was measured.

## Correction log

- "all 22 packages" in an earlier revision of Measurement 1 was a stale figure carried over
  from a 2026-11-15 board event. Re-counted against this log: `grep -c '| ok  ' ci.log` = 23,
  and lines 145-167 hold 23 `ok` lines. The corrected figure is used above.
