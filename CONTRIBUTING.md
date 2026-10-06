# Contributing to mischief

Thanks for looking. This project is built spec-first, and that single rule
shapes everything below. The sibling project `trouble` follows the same
convention — read its CONTRIBUTING too if you contribute to both.

## The authority is the spec set, not the code

`docs/prd/mischief-v0.1.md` (the PRD), `docs/SPEC-PLAN.md` and the shipped
spec documents are the contract. Code lands against an acceptance criterion
(AC) that already exists there, or the spec is amended first — with a
letter-suffix section (`§3.4a`, never a renumbering of `§3.4`).

Before you write code:

1. Find the AC that covers your change in the spec set.
2. If no AC covers it, amend the owning spec (letter suffix) first.
3. State in your PR which AC the change satisfies, or which spec section you
   amended.

## Safety is not negotiable

mischief injects faults on purpose. Every contribution must preserve:

- **landed-proof**: a fault run that cannot prove it landed is a `no_op`,
  never a pass;
- **inverse before land**: a fault without a recorded inverse does not ship;
- **no default target**: target resolution is explicit or it refuses;
- **the isolation contract** (SPEC-13): development, selftest and battery runs
  happen on a sanctioned, ephemeral host — never point mischief at a host you
  did not sanction, including your own main machine.

## Build and test

```bash
make bin                 # build bin/mischief (stamped git sha)
go vet ./...
go test -count=1 ./...
make shim                # rebuild the LD_PRELOAD shim (drift gate in CI)
python3 tools/gen-docs.py --check   # docs/*.html twins must match docs/*.md
```

CI runs all of the above on every push and PR; the shim drift gate and the
docs-twin gate fail the build, not a warning. Please run them before pushing.

## Commits

- Small, path-limited commits; subject line says what changed and why.
- If a commit addresses a board row (`.coding-hermes/board/tasks.jsonl`),
  reference the row id in the body.

## Pull requests

- CI green is the baseline, not the review. The interesting part is the AC
  mapping and the failure-mode reasoning.
- New fault primitives need: catalog row with all five contract fields, a
  selftest, and an entry in the fault catalog docs (regenerate the HTML twin).
- Docs changes must regenerate `docs/*.html` twins (`tools/gen-docs.py`) so
  the two stay byte-consistent.

## License

By contributing you agree that your contributions are licensed under the
MIT License that covers this repository.
