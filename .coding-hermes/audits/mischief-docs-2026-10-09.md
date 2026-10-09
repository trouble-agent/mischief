# Mischief documentation audit — 2026-10-09

Scope: source-derived CLI, operator/environment surface, four reader audiences, audience-facing docs and rot. Repo `/home/kara/mischief`, HEAD `6e7e20b71faf6dfd5eea0280009c490472536cd8`. This is evidence for open documentation findings; no docs were edited.

## Commands and literal observations

1. `$ nl -ba SECURITY.md | sed -n "38,42p"`
```
38\t## Supported versions
40\tThe project has not cut its first release yet; `main` is the only supported
41\tline. Fixes land on `main` and ship with the next tagged build.
```
2. `$ git grep -n -E "v0\\.1\\.0|first release|not cut" -- README.md SECURITY.md docs`
```
README.md:56:verify, and unpack (example for tag `v0.1.0`, amd64):
README.md:59:gh release download v0.1.0 -R trouble-agent/mischief
README.md:61:curl -LO https://github.com/trouble-agent/mischief/releases/download/v0.1.0/mischief_v0.1.0_linux_amd64.tar.gz
README.md:64:tar -xzf mischief_v0.1.0_linux_amd64.tar.gz
README.md:84:mischief version   # mischief v0.1.0 (a release stamps the tag; `make bin` stamps the git sha)
SECURITY.md:40:The project has not cut its first release yet; `main` is the only supported
docs/RELEASE.md:9:git tag -a v0.1.0 -m "mischief v0.1.0"   # annotated; the CUT is the foreman's
docs/RELEASE.md:10:git push origin v0.1.0
docs/RELEASE.md:76:  (`-X main.gitSha=v0.1.0`): the binary reports the version it was cut from.
```
3. `$ gh release view v0.1.0 --repo trouble-agent/mischief --json tagName,isDraft,publishedAt`
```
{"isDraft":false,"publishedAt":"2026-10-07T16:35:57Z","tagName":"v0.1.0"}
```
Witness: published release `v0.1.0` (non-draft).

4. `$ nl -ba skills/mischief-usage/SKILL.md | sed -n "8,18p"`
```
8\tmischief is a **design-stage** fault-injection project ("faults are data":
9\tlanded-proof + inverse + blast radius). There is NO binary. Do not look for a
10\tCLI to run; the usable surfaces are the catalog, the probe, and the YAMLs.
16\t- `docs/FAULT-CATALOG.md` — 104 primitives, 8 layers (P14 S14 R12 F16 N18 C12
17\t  I14 T4). Counts verified exactly 2026-10-02 (recompute, never eyeball).
```
5. `$ ./bin/mischief --help | sed -n "1,22p"` (after `$ make bin`; build output: `CGO_ENABLED=0 go build -ldflags '-X main.gitSha=6e7e20b71faf' -o bin/mischief ./cmd/mischief`)
```
mischief — fault injection for a fleet of Linux daemons (M1 chassis)

Usage:
  mischief plan    -f <exp.yaml> [--json] [--scratch-dir D]
  mischief status  [--dir D]
  mischief revert  (--all | --hold <id>) [--dir D]
  mischief doctor  [--catalog-dir D] [--json] [--self-check]  (exit 1 = a check FAILED: catalog load, or a --self-check rail)
  mischief battery [--quick] [--project P] [--primitive ID] [--out D] [--file-rows] [--dry-run]
  mischief chaos   matrix --project P [--tier-max N] [--out D] | check --lane F | finding --project P --board D --title T --reason R
  mischief sim     --shape I-00N [--path P] [--log F] [--allow-real --resource TAG --spend-cap USD]
  mischief serve   --dir D          (reverter daemon: TTLs + boot reconcile + health.json)
  mischief install    [--user U] [--prefix P] [--dry-run] [--no-drop-in]
  mischief uninstall  [--prefix P] [--dry-run] [--keep-count N] [--keep-days D]
  mischief audit      [--prefix P] [--json]
  mischief retention  [--apply] [--runs-dir D] [--keep-count N] [--keep-days D] [--dry-run]
  mischief plane     parse-agent-id | record | selftest-verdict | fold   (bunker test-plane evidence CLI)
  mischief version
```
6. `$ nl -ba docs/prd/mischief-v0.1.md | sed -n "155,169p"` shows the CLI table still claims the old `run`, `catalog`, `monkey`, and `mischiefd` interfaces at lines 160, 163, 165, 168; the corresponding commands are absent from the built binary's help above. Current CLI dispatch is `cmd/mischief/main.go:68-115` (source-derived; excludes internal `__owner`).
7. `$ grep -RInE --exclude="*.html" --exclude="*.jsonl" "MISCHIEF_REVERTER_|MCFAULT_BUDGET|MCFAULT_PROOF" README.md docs CONTRIBUTING.md SECURITY.md skills || true`
```
[no output]
```
Source anchors: `internal/reverter/owner.go:18-29` defines four `MISCHIEF_REVERTER_*` variables; `internal/backend_signal/rule.go:205-219` adds `MCFAULT_BUDGET` and `MCFAULT_PROOF` to child environments. No reader-doc hit.
8. `$ test -f docs/traceability-doctrine.md; printf "trace_contract_rc=%s\\n" "$?"`
```
trace_contract_rc=1
```
9. `$ git grep -n -E 'v0\\.1\\.0|first release|not cut' -- README.md SECURITY.md docs` reproduced output listed above; `$ git ls-remote origin refs/heads/main` and `$ git rev-parse HEAD` both returned `6e7e20b71faf6dfd5eea0280009c490472536cd8`.
10. `$ ~/.local/bin/boardctl -C /home/kara/mischief validate` returned `RESULT: OK (58 warning(s))`; this is pre-write board baseline, mostly legacy schema/event warnings.

## Audience and source-to-doc gaps

- End-user/CLI audience: README CLI list at `README.md:47` covers a subset and omits operational verbs; existing README-5 already covers its omission finding, so no duplicate filed. Existing README-6 covers the quickstart sanction mismatch; no duplicate filed.
- Developer audience: `CONTRIBUTING.md:33-44` documents make/build/test and generated docs; reasonable home present.
- Operator audience: operational guides exist, but no public reader doc inventories the reverter/child-shim environment variables above; row DOC-10.
- Integrator/spec audience: `docs/prd/mischief-v0.1.md:155-168` includes retired/unimplemented verbs while the binary help shows the current interface; row DOC-9.
- Public policy audience: `SECURITY.md:40-41` falsely says no release exists, contradicted by README and the actual published release; row DOC-8.
- Agent usage skill is stale: says no binary and no CLI despite buildable CLI and published release; row DOC-7.
- Traceability contract: target repo does not contain the required `docs/traceability-doctrine.md`; row DOC-6.

## Unverified

- Did not run selftest/battery or touch any live fault target; safety contract intentionally keeps those out of this documentation audit.
- Did not exercise every verb's stateful behavior, flags, every backend, release archive install on a fresh host, or inspect all generated HTML twins/examples. CLI surface was executed from the current source build; remote release status checked for the SECURITY rot finding.
- No re-triage of pre-2026-09-30 P1-P3 documentation findings was needed in the sampled existing DOC-/README- rows: older rows are complete; current open rows are P4/P5. Other generic board rows were not exhaustively semantically re-triaged.
