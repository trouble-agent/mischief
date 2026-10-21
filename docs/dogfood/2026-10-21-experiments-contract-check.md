# Dogfood check — EXPERIMENTS.md contract page vs live `plan` refusals

Lane: `mischief-chaos-dogfood`, tick `mischief-chaos-dogfood-2026-10-21-02-36-57` (resume delivery).
Target: product HEAD `ac4bc1b71517ca29cbe6fd218235527c16845738`.
Build: `git clone /home/kara/mischief` + `make bin` on the clone → binary reports
`mischief ac4bc1b71517` (the sha stamp ties this evidence to the commit; the archive-export build
reports `unknown` because `.git` is absent).
Environment: sanctioned scratch shell (`MISCHIEF_SANCTION=1`, `MISCHIEF_SANCTION_FILE=<scratch>/marker`,
which carries a reason line), fresh scratch run dirs under `/tmp/mischief-df-20261021-0236/`.
Nothing was written outside those scratch dirs; no fault was landed on a real target.

## What the page claims

`docs/EXPERIMENTS.md` calls itself the contract binding all four shipped experiments ("Nothing on
this page overrides a header — read both before touching a target") and gives a runnable-today table
whose "why" column explains each not-runnable one with a single mechanism — SPEC-03 not having
landed:

- `:13` ex-002 — "container selector is unresolvable without SPEC-03 …"
- `:14` ex-003 — "process selector and the `<state_root>` probes are unresolvable without SPEC-03"
- `:15` ex-004 — "selectors and `<state_root>` / `<oracle>` placeholders are unresolvable without SPEC-03"
- `:36-37` — "the surrounding selectors do not resolve at all until SPEC-03 landed (see SPEC-03
  tracking task 6054b4d)"

## What a real operator meets

Four `plan` invocations, rc read directly, output kept verbatim (scratch
`plan-ex-00*.json` / `experiments.out`):

| experiment | rc | target as `plan` resolved it | refusal actually reported |
|---|---|---|---|
| ex-001-noop-detector | 0 | `host:.` | none (plan-clean — control) |
| ex-002-redis-loss-recovery | 2 | `container:trouble-hub` **resolved** (kind=container, selector echoed) | `scope_below_minimum` — "I-002: rails: scope_below_minimum: scope L0 is below the primitive's declared minimum tier L3 (L3 / L4)" |
| ex-003-spool-kill-between | 2 | `process:troubled` **resolved** (kind=process, selector echoed) | `P-006: not in the loaded catalog` + `scope_below_minimum` |
| ex-004-live-file-replace | 2 | `process:hermes-gateway` **resolved** (selector echoed) | `protected_target` — "protected target \"gateway\" refused: fleet control plane …" |

No `ResolveError` appears in any of the three refusals, and each target block carries a resolved
kind plus the declared selector — i.e. the selector resolved and the run was refused for a
different, specific reason. The page's own `:68-71` paragraph already concedes the resolver exists
in `internal/rails/` (and `plan` calls it today), so the "selectors do not resolve" clauses
contradict the same page.

## Why this is worth a row rather than a note

The page is the operator's gate before touching a target. A reader is told to wait for SPEC-03 — a
blocker that closed long ago — and never learns the three real prerequisites:

1. declare a scope at or above the primitive's own minimum tier (ex-002's I-002 is L3/L4; the
   scratch default is L0),
2. name a primitive that is present in the loaded catalog (P-006 is not shipped), and
3. stay off the protected list (ex-004's gateway selector is refused by design since the
   prefix-wide protected match landed — MISCHIEF-RV-1 / MISCHIEF-DF-58).

Same drift class as open `MISCHIEF-DF-63` (coaching out of step with row/commit status) but a
different file and a different fix, so it is filed as `MISCHIEF-DF-64` rather than attached to that
row.

## Reproduction

```sh
git clone /home/kara/mischief /tmp/mischief-df-20261021-0236/clone
cd /tmp/mischief-df-20261021-0236/clone && make bin
printf '%s\n' 'sanctioned: scratch check' > /tmp/mischief-df-20261021-0236/marker
export MISCHIEF_SANCTION=1 MISCHIEF_SANCTION_FILE=/tmp/mischief-df-20261021-0236/marker
for f in catalog/experiments/*.yaml; do
  bin/mischief plan -f "$f" --scratch-dir /tmp/mischief-df-20261021-0236/target2; echo "rc=$? file=$f"
done
```
