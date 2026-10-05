# mischief — Release flow (MSF-018)

One flow: a **v\*** tag. Nothing else cuts a release.

```
gates green on main (CI)
        │
        ▼
git tag -a v0.1.0 -m "mischief v0.1.0"   # annotated; the CUT is the foreman's
git push origin v0.1.0
        │
        ▼
.github/workflows/release.yml           # tag-triggered, gates rerun
  ├─ go build / vet / test ./internal/... -count=1
  ├─ mischief selftest --all             # exit 0 only green-or-skip
  ├─ mischief battery --primitive N-012  # one live cell (fail surfaces; no_op fails)
  ├─ tools/shim-build.sh --smoke         # per-arch .so + LIVE landing proof
  ├─ drift gate: tools/shim-extracted.c == ShimSourceC
  └─ goreleaser check && goreleaser release --clean
        │
        ▼
GitHub Release (DRAFT — a human reviews the archives, then publishes)
```

`ci.yml` also fires on v\* tag pushes (the tag trigger was added in MSF-018),
so the ordinary gate battery runs on the tag's commit even if the release
workflow is disabled; the release job itself is what packages and uploads.

## The shim .so — decided path

The shim is a **release artifact per architecture**, not a build-time
afterthought. The decided path (MSF-018 board row):

- **Source of truth:** the `ShimSourceC` constant in
  `internal/backend_signal/shim.go` — the same source the runtime compiles
  fresh at arm time. The legacy probe file (`probe/libfault-probe.c`) is a
  historical artifact and is NEVER shipped.
- **Compilers: system cc** (amd64, native) and **`gcc-aarch64-linux-gnu`**
  (arm64, the distro cross-gcc). **Zig is not used**: it is absent on this
  host and adds nothing for two linux/glibc targets of plain C. CI
  (`ubuntu-latest`) has `cc` on the base image; the arm64 side is
  `sudo apt-get install -y gcc-aarch64-linux-gnu libc6-dev-arm64-cross qemu-user`.
- **Proof before ship:** `tools/shim-build.sh --smoke` lands the fault on a
  real child per arch — amd64 native, arm64 under `qemu-user` — and asserts
  the target write returned EIO, the control write passed, and the shim's
  independent counter carries `hits=1` with this rule's text (the
  landed-proof contract). A release **refuses** (exit 1, naming the missing
  tool) rather than shipping without the shim or with an unverified one.
- **cc flags match the runtime**: `cc -shared -fPIC -O2 … -ldl`, byte-for-byte
  the flags `ArmShim`'s `buildShimLib` uses — a released archive's .so is the
  same shape the selftest compiles.
- **Packaging:** each `mischief_v*_linux_<arch>.tar.gz` carries its arch's
  `.so` as `libfault-anchored.so` (mode 0755). The archive glob is per-arch
  templated; a missing .so fails the release instead of packaging an empty
  slot. `checksums.txt` (sha256) covers the archives.
- **Drift gate:** `tools/shim-extracted.c` is the committed byte-copy of
  `ShimSourceC` (refreshed by `tools/shim-build.sh`; CI runs
  `git diff --exit-code` on it). Editing `ShimSourceC` without re-stamping
  the release source fails CI.

Local one-liners:

```sh
make shim                        # build dist/shim/*.so + SHA256SUMS
./tools/shim-build.sh --smoke    # + LIVE landing proof per arch
make release-dry                 # goreleaser check — config validation only
make dist                        # local snapshot dry-run: gates + goreleaser --snapshot --clean
```

## Version stamping

`mischief version` prints whatever `main.gitSha` was stamped with:

- `make bin` → the git sha (`-X main.gitSha=$(git rev-parse --short=12 HEAD)`).
- A real release (goreleaser from the tag) → **the literal tag**
  (`-X main.gitSha=v0.1.0`): the binary reports the version it was cut from.
- `--snapshot` dry-runs → goreleaser's synthetic `v0.0.0` (measured
  locally) — visibly a dry-run, never a borrowed tag.

## Cutting a release (foreman checklist)

1. Gates green on `main` (CI includes the shim build + live landing proof,
   the selftest suite, and one live battery cell).
2. `git tag -a vX.Y.Z -m "mischief vX.Y.Z" && git push origin vX.Y.Z`
   (`make tags` prints this — it never tags by itself).
3. Watch `.github/workflows/release.yml` rerun every gate; goreleaser only
   runs after all of them pass.
4. Review the DRAFT release: both archives present, each contains
   `mischief` + `libfault-anchored.so` (0755) + LICENSE + README,
   `checksums.txt` matches. Then publish.

Rollback/no-go: delete the tag (`git push origin :refs/tags/vX.Y.Z`) — the
release stays a draft and is deleted by hand; nothing consumes tags.

## Local dry-run (what `make dist` proves)

`goreleaser release --snapshot --clean` with the shim pre-built produces the
exact archive set a tag would (under `dist/release/`), cross-compiled for
both arches, each tarball carrying its arch's shim. What it cannot prove
locally is the GitHub upload step — that part is CI's, and the release lands
as a draft for exactly that reason.
