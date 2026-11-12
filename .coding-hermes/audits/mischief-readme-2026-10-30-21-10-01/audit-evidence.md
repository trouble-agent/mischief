README health sweep — mischief — published tip 0eea5e14817e215528eef4d810fe632b01697f6f

Scope and boundary

Commands:
`git ls-remote origin refs/heads/main` -> `0eea5e14817e215528eef4d810fe632b01697f6f`
`git rev-parse HEAD` -> `0eea5e14817e215528eef4d810fe632b01697f6f`
`git log -1 --format='%H %ad %s' --date=iso-strict -- README.md` -> `f625ff6a3ffcbf2d08ca82df715b351feb395b39 2026-10-28T15:08:13-05:00 DOC-24-R2: README legacy-vocabulary paragraph reflects merged fix 15a9b3e. Addresses DOC-24-R2.`
`git log -1 --format='%H %ad %s' --date=iso-strict` -> `0eea5e14817e215528eef4d810fe632b01697f6f 2026-10-30T03:19:51-05:00 qa: record mischief battery 2026-10-30-07-48-02`
`gh release list --repo trouble-agent/mischief --limit 10` -> `v0.1.0 Latest v0.1.0 2026-10-07T16:35:57Z`
`git tag --sort=-creatordate` -> only `v0.1.0`.

Finding README-10 — release is stale relative to shipped main

File/line: README.md:56-63 and README.md:86; README.md:283-298.
Exact commands/output:
`git log --oneline f625ff6..origin/main -- README.md cmd/mischief internal docs catalog Makefile .github/workflows/release.yml` -> changes after the last README edit include `35cc113 fix: watchdog TTL must use monotonic clock, not wall clock (MISCHIEF-DF-59)`, `5ec2dcf fix: normalize legacy arm vocabulary before validation. Addresses MISCHIEF-DF-68-R2-R2.`, `8ecef33 DF-56-CI...`, `fffcba9 Merge wt/MISCHIEF-DF-66...`, `be46a45 ... health shape`, `a801bce ... error printer`, `439ad2f ... version`, and `09b6e34 DOC-12...`.
`nl -ba README.md | sed -n '280,302p'` -> lines 291-298 say the CLI refuses `restore_file` and that MISCHIEF-DF-68-R2-R2 is still open.
`git merge-base --is-ancestor 5ec2dcf origin/main` -> RC=0.
`git show --no-patch --format='%H %s' 5ec2dcf` -> `5ec2dcf11a099c4412af192706691bd5177e6e4b fix: normalize legacy arm vocabulary before validation. Addresses MISCHIEF-DF-68-R2-R2.`

Fresh isolated legacy arm probe (published source clone): `bin/mischief arm --fault S-001 --target scratch:<isolated-dir> --ttl 30s --inverse-kind restore_file --inverse-params path=<target>,from=<backup> --check-kind file_matches_backup --check-params path=<target>,from=<backup> --dir <isolated-run-dir>` -> RC=0, `armed ... fault=S-001 ...`; `bin/mischief revert --all --dir <isolated-run-dir>` -> RC=0, `reverted ... outcome=reverted`; target readback -> `original`.
`grep -n 'restore_file\|ValidateForArm' cmd/mischief/arm.go internal/reverter/lifecycle.go` -> current source contains the pre-validation/normalization paths changed by the fix.
Published source fresh-clone probe: `git clone --depth 1 https://github.com/trouble-agent/mischief source` RC=0 at `0eea5e14817e215528eef4d810fe632b01697f6f`; `make bin` RC=0; `bin/mischief selftest --all` RC=2 unsanctioned; sanctioned selftest RC=0 with `13 pass, 9 skip, 0 fail (of 22)`.
This is a stale README statement: the published main includes the fix but the README still reports the pre-fix CLI behavior and open row. Severity P4 by documentation policy. Existing README-10 was minted free by this tick; it is the row for this published-tip freshness finding.

Finding README-11 — release README misstates the tarball's full stranger path

File/line: README.md:56-89, 179-184.
Exact commands/output from the fresh published source clone and release probes:
`git clone --depth 1 https://github.com/trouble-agent/mischief source` -> RC=0.
`make bin` -> RC=0, `CGO_ENABLED=0 go build ... -o bin/mischief ./cmd/mischief`.
`bin/mischief selftest --all` on a fresh unsanctioned clone -> RC=2 with `host_not_sanctioned` and `nothing was written`; the README does explain sanction gating later, so this is not a duplicate of closed README-6.
`curl -fL -o curl-amd64.tar.gz https://github.com/trouble-agent/mischief/releases/download/v0.1.0/mischief_v0.1.0_linux_amd64.tar.gz` -> RC=0.
`curl -fL -o checksums.txt https://github.com/trouble-agent/mischief/releases/download/v0.1.0/checksums.txt` -> not completed in the bounded probe because the prior `gh release download` path was authentication-blocked; the exact release asset URL was live and the archive was already fetched by curl.
`tar -tzf mischief_v0.1.0_linux_amd64.tar.gz` -> `LICENSE`, `README.md`, `libfault-anchored.so`, `mischief`.
Fresh release binary: `mischief version` -> `mischief v0.1.0`; `mischief status` from a fresh HOME -> RC=0 with `holds: 0`; `mischief doctor` from a tarball-only directory -> RC=1, `doctor: catalog load FAILED: catalog/faults not found from cwd upward`, followed by `run mischief install` advice.
Source stranger path after clone/build: `mischief doctor` -> RC=0, `catalog ok loaded (11 primitives, version 910b17...)`; source `mischief audit` -> RC=0 with `condition: not_installed`; `mischief install --dry-run` and `mischief uninstall --dry-run` both RC=0.
The README's release install section presents `mischief doctor` as a direct post-install check, but its own documented tarball-only caveat says doctor needs an in-repo catalog and exits 1 outside a checkout. This is internally disclosed but the stranger path has no clear successful release-only verification command; the README should make the release-only boundary explicit in the quickstart. Severity P4 by policy.

Finding README-12 — repository has no documented or executable docs-check target

File/line: Makefile (no `docs-check` target); README.md has no docs-check command.
Exact command/output: `make docs-check` -> `make: *** No rule to make target 'docs-check'. Stop.`, RC=2. The README health procedure explicitly prefers the repository's docs-check target when present; the repository provides none, so future README drift has no repeatable repository-local check. Severity P4 by documentation policy.

Freshness and structure checks

The README has sections answering what/why/build/run/test/install/safety/limits. `README.md` names `make bin`, `go.mod` says Go 1.24 with toolchain go1.26.9, and the release tarball is v0.1.0. `README.md` last moved 2026-10-28 while origin/main has subsequent source and board commits; the release remains v0.1.0. Links found in the published source README returned HTTP 200 for the repository, releases page, v0.1.0 amd64 archive, and sibling trouble repository.

The canonical source stranger flow and the canonical arm/serve flow were executed. `canonical-transcript.txt` records the arm, status, fault landing, serve TTL, final status, and target restoration. It observed arm RC=0, status armed, manual LAND, `serve` bounded by timeout RC=124 after the daemon was left running, final `status --all` RC=0 with `reverted`, and target content `original`. This verifies the user-visible flow at T5 for the documented safe scratch path. The legacy underscore arm probe `legacy-transcript.txt` observed RC=0 and canonical recovery, consistent with the merged fix; this is the out-of-band witness for README-10.

Unverified boundaries

The exact `gh release download` command was blocked by missing gh authentication. The release archive itself was fetched through the documented curl URL and read back; the separate checksum asset was not completed in that bounded probe. No privileged non-dry-run install/uninstall was run. No bunker/clean-machine stranger install was run. The README's `sudo` paths remain unverified. No changes were made to README or docs by this audit.

ch:trace row=README-10 spec=README.md#Install wave=none:standalone-audit test=none:published-tip-freshness doc=README.md prompt=~/.hermes/skills/coding-hermes-readme/SKILL.md evidence=.coding-hermes/audits/mischief-readme-2026-10-30-21-10-01/audit-evidence.md witness=path@origin:README.md verdict=none:open-finding commit=none:open memory=none:exact-key-not-found
ch:trace row=README-11 spec=README.md#Install wave=none:standalone-audit test=none:stranger-release-install doc=README.md prompt=~/.hermes/skills/coding-hermes-readme/SKILL.md evidence=.coding-hermes/audits/mischief-readme-2026-10-30-21-10-01/audit-evidence.md witness=release:v0.1.0@origin verdict=none:open-finding commit=none:open memory=none:exact-key-not-found
ch:trace row=README-12 spec=README.md#Install wave=none:standalone-audit test=Makefile::docs-check doc=README.md prompt=~/.hermes/skills/coding-hermes-readme/SKILL.md evidence=.coding-hermes/audits/mischief-readme-2026-10-30-21-10-01/audit-evidence.md witness=none:no-docs-check-target verdict=none:open-finding commit=none:open memory=none:exact-key-not-found
