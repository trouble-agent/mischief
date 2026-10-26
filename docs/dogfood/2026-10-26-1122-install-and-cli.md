# Dogfood: 2026-10-26 11:22 UTC — install and CLI surface

Tick: mischief-dogfood-2026-10-26-11-08-49
Product HEAD: 0fc4b9df442f5bba640ebfad406d25a3068e7e92

Promise

A fresh user can build or install mischief, run the documented CLI, and receive truthful state, safety, and JSON responses without mutating real targets.

Real-use evidence

The product checkout was materialized as detached worktree `/tmp/mischief-df-20261026-1122` at the exact HEAD above. The initial build with the isolated HOME failed immediately because the Go toolchain resolver tried to find `go1.26.9` under `$HOME/sdk`:

`go1.26.9: not downloaded. Run 'go1.26.9 download' to install to /tmp/mischief-df-20261026-1122-home/sdk/go1.26.9`

This was an environment/setup distinction, not treated as a product failure: repeating the documented build with the existing real Go toolchain cache (`HOME=/home/kara`) completed in 0.85 seconds and stamped `mischief 0fc4b9df442f`. A subsequent build with the isolated HOME failed again for the same missing-toolchain reason, proving that the isolated HOME changes toolchain availability.

With the built binary and isolated runtime HOME:

- `version` returned rc=0 and `mischief 0fc4b9df442f`.
- `status --dir <absent>` returned rc=1 with `run_dir` and `state: absent`, and wrote no state.
- `status --json --dir <absent>` returned rc=1, emitted one parseable JSON object, and `jq -e .` returned rc=0.
- `doctor --catalog-dir catalog/faults --self-check` returned rc=1 and named the unsanctioned-host refusal; no target was touched.
- `audit --json --prefix <scratch-prefix>` returned rc=0 with parseable JSON and `condition: not_installed`.
- `retention --dry-run --runs-dir <scratch-runs>` returned rc=0 and reported zero actions.

Performance

The successful cold build was 0.85 seconds wall time. CLI startup operations were below the resolution of the recorded `time -p` runs (0.00 seconds wall for version, absent status, doctor, audit, and retention); no user-noticeable product latency was found and no PERF row is warranted.

Ephemeral install leg

Bunkers were reachable: `bunker2` reported `bunker-las-02 active` and `bunker-mvp` was reachable earlier in the tick. The attempted `bunker-qa.sh run /home/kara/mischief --keep` produced evidence `/tmp/bunker-qa-evidence-20261026T112624Z-3854552.jsonl`, but its final record says `collect` failed with `no launch record`; this run therefore does not claim a clean-machine install PASS. The evidence contains `fresh-install FAIL` with the harness-shaped message `install rc=0 but no artifact found (toolsd not in PATH...)`, while the same run reports `upgrade OK` and a native `ci-pass FAIL` involving build failures. This is partial evidence, not a silent pass, and the agent/process residue is outside the product repository.

No product source or board state was modified by this lane. No credentials, permissions, visibility, or real target were changed. Scratch evidence remains at `/tmp/mischief-df-20261026-1122` and the bunker evidence path above for the owning writable foreman to classify and file.
