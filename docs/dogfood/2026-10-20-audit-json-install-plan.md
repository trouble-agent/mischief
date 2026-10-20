# Dogfood 2026-10-20 — audit and manifest-plan surface

## Promise

A new operator can inspect mischief's privileged surface with `audit`, consume the same result as JSON, and preview a rootless manifest install with `install --no-drop-in --dry-run` without changing host state.

## Real-use setup

This run used a clean `git archive HEAD` copy at product commit `72017726b67c6`, built with `CGO_ENABLED=0 go build` into a scratch directory. The source archive was intentionally isolated from the live checkout. All prefixes and run state were under `/tmp/df-mc-r22-*`; no real sudoers file or user data was touched.

## Workflow driven

1. `mischief version` on the archive-built binary returned rc=0. The blank stamp is an artifact of building a source archive without `.git`, not the documented clone or release path; it was not treated as a product finding.
2. `mischief audit --prefix <scratch-prefix>` returned rc=0 and `condition: not_installed`, with the expected absent drop-in and absent helper manifest.
3. `mischief audit --prefix <scratch-prefix> --json` returned rc=0 with the same `condition`, explicit boolean fields, a nine-verb table, and an empty `setuid_binaries` array.
4. The JSON form was run twice and produced the same shape and verdict. `jq` consumers can read `condition`, `drop_in_installed`, `helper_manifest_installed`, `setuid_binaries`, and `verbs` directly.
5. `mischief install --prefix <scratch-prefix> --no-drop-in --dry-run` returned rc=0 and printed a two-action plan: create the prefix and write the helper manifest. The following audit confirmed the dry run touched nothing: the manifest remained absent and the condition remained `not_installed`.

## Results

The audit text and JSON surfaces agree. The dry-run promise held byte-for-byte at the observable boundary: no prefix artifact was created. The unprivileged manifest-only plan is understandable and separates the root-required drop-in from the rootless manifest path. No new defect was found on this surface.

## Performance

The user-visible audit JSON operation was measured with `/usr/bin/time -p` on the scratch binary: `real 0.02`, `user 0.00`, `sys 0.00` seconds on the first measured invocation. The repeated warm invocation was also sub-20ms by observation. This is process-startup scale and not user-noticeable; no PERF row was filed.

## Fresh-machine install leg

Proven, not skipped, using `scripts/bunker-plane.sh run` against `bunker-mvp` with frozen HEAD `72017726b67c`. Evidence journal: `/tmp/df-mc-r22-plane.jsonl`. All 8 evidence rows were `pass`: preflight, build, spawn, two ship rows, collect, selftest, and destroy. Agent `2275f0eb` was destroyed and absent from the bunker list. The remote selftest reported 13 pass, 9 skip, 0 fail of 22.

## Verdict

SHIPPABLE on the audit/JSON/manifest-plan surface. The documented inspection and no-side-effect preview work as a real operator would use them. Existing privileged lifecycle limitations remain tracked by prior board rows; this run did not duplicate them.

Read-only run hygiene: no product source changed, no existing board row was edited, no scheduler state or permissions changed, and no credentials were created.
