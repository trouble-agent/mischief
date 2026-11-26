README health sweep evidence for published origin/main 456c6d4374995773854a27ed2476251a5c04b6aa.

Freshness and release boundary

Commands:
git describe --tags --abbrev=0 origin/main
Output: v0.1.0
git log -1 --format='%H %cI %s' origin/main -- README.md
Output: d5a15c7781d637b3ea45f60be5f4368e686f673b 2026-11-24T20:44:12-05:00 MISCHIEF-DF-69: make the fresh-HOME build path self-contained and the bunker install check product-aware.

The README is present at the published tip and is newer than the v0.1.0 tag. A numeric claim in the README is stale: README.md:11 says approximately 1,600 lines of Go for internal/catalog. The exact current count was measured with:
wc -l internal/catalog/*.go
Output:
  757 internal/catalog/catalog_test.go
  678 internal/catalog/loader.go
    7 internal/catalog/status_unknown_primitive.go
  484 internal/catalog/types.go
1926 total

The four current catalog source files total 1,926 lines, not approximately 1,600. At v0.1.0 the same package files totalled 1,735 lines. This is a reader-facing hard number, not a release-boundary claim.

Documented release install path

Commands executed in isolated directories:
gh release download v0.1.0 -R trouble-agent/mischief
Output: exit 0
grep "mischief_v0.1.0_linux_amd64.tar.gz" checksums.txt | sha256sum -c -
Output: mischief_v0.1.0_linux_amd64.tar.gz: OK; exit 0
tar -xzf mischief_v0.1.0_linux_amd64.tar.gz
Output: exit 0

The documented curl alternative was executed literally:
curl -LO https://github.com/trouble-agent/mischief/releases/download/v0.1.0/mischief_v0.1.0_linux_amd64.tar.gz
Output: exit 0, 3.19M downloaded
grep "mischief_v0.1.0_linux_amd64.tar.gz" checksums.txt | sha256sum -c -
Output: grep: checksums.txt: No such file or directory; sha256sum: 'standard input': no properly formatted checksum lines found; exit 1
tar -xzf mischief_v0.1.0_linux_amd64.tar.gz
Output: exit 0

The curl alternative downloads only the archive although the following verification command requires checksums.txt. The gh alternative succeeds because gh downloads the release asset set. The published release object is v0.1.0 with checksums.txt, mischief_v0.1.0_linux_amd64.tar.gz, and mischief_v0.1.0_linux_arm64.tar.gz.

Stranger install path

A full public clone and source build succeeded:
git clone https://github.com/trouble-agent/mischief <fresh directory>
Output: exit 0
make bin
Output: toolchain go1.26.9 ...; bin: bin/mischief (mischief 456c6d437499, toolchain go1.26.9); exit 0
bin/mischief version
Output: mischief 456c6d437499; exit 0
bin/mischief version --json
Output: {"version":"456c6d437499","tool":"mischief"}; exit 0

The release tarball binary was installed into an isolated HOME. `mischief version` printed `mischief v0.1.0` and `mischief status` printed a run_dir, boot_id, holds: 0, proofs: 0, exit 0. This differs from the current source binary's fresh-HOME absent-state contract, which printed `state: absent`, exit 1, and created no state files; both commands were read back from their respective isolated HOME directories.

Current source command execution

mischief doctor
Output: doctor: catalog ok loaded (18 primitives, version 6ba6eda1a66b080788f89193011d5ee45e33281895553748b620a7dddf999556); exit 0.
mischief doctor --catalog-dir /home/kara/mischief/catalog/faults
Output: same 18-primitive catalog report; exit 0.
MISCHIEF_SANCTION=1 bin/mischief plan -f catalog/experiments/ex-001-noop-detector.yaml
Output: plan ex-001-noop-detector, run_id 84c8a84cecf315c1, primitive F-008, exit 0.
make docs-check
Output: row-drift PASS (29 refs vs 266 rows); gen-docs OK — 0 stale twin(s); cli-table PASS — 24 command(s), 0 mismatch(es), 1 warning(s), exit 0.
The remaining warning was: WARN version — binary accepts undocumented flag(s): version; the binary accepts --version and --json. This is already owned by the open row DOC-20-R2-R2 and was not filed again.

Links and badges

The local-link probe found every relative Markdown target present: internal/catalog/, docs/, docs/OPERATIONS.md, docs/reference/operator.md, docs/RELEASE.md, docs/LANES.md, docs/CHAOS-LANE.md, docs/traceability-doctrine.md. External probes returned HTTP 200 for the mischief repository, trouble repository, and mischief releases page. No badge is present.

Reader-question structure

The README answers what it is, why, build, run, test, install/deploy, and safety/known limits through its sections and linked docs. No structure finding was filed.

Existing open documentation rows reused instead of duplicated

The published board already carries open P4 rows for the stale scheduler count (DOC-11-R2), the environment-variable overclaim (DOC-17-R2-R2), the reverter journal schema (DOC-34-R2), the PRD verb table (DOC-16-R2), and the version flag warning (DOC-20-R2-R2). This sweep did not create siblings for those findings.

UNVERIFIED

The documented privileged install/uninstall commands were not run because they require root and mutate host-level state. The full arm/serve/land/revert workflow was not run because it mutates fault state and requires a sanctioned host. The README's curl path was executed through the checksum failure; the missing checksums download is the observed defect. The first command runner's local-link probe had a shell-tokenization error, then an independent file-based probe was run and completed.
