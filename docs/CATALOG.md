# internal/catalog — developer guide

`internal/catalog` implements SPEC-01: the fault model and the primitive
catalog. It is the shipped Go half of [docs/FAULT-CATALOG.md](FAULT-CATALOG.md)
— the catalog is **data**: every primitive is a YAML descriptor file, and a
primitive that passes the loader needs no engine code change.

The package's job is narrow:

1. **Load** a directory of descriptor YAML files (`LoadDir`, `LoadDefault`),
   validating each against the fault contract and refusing a malformed one
   with a named reason (which file, which primitive, which field) via
   `LoadError`. Duplicate ids are refused.
2. **Model** the descriptor schema as typed Go structs (`Descriptor` and the
   types it is built from), so the rest of the engine never parses YAML.
3. **Version** the loaded set by content (see
   [Catalog versioning](#catalog-versioning) below).
4. **Derive** per-primitive availability status (`Descriptor.Status`) from the
   descriptor plus a caller-supplied `CapabilityRegistry` — the package never
   probes the host itself (AC-11).

## Loading a descriptor set

`LoadDir` loads any directory of `.yaml` descriptor files (files are read in
sorted order; subdirectories and non-`.yaml` files are ignored). It returns a
`*Catalog` or a `*LoadError` — always check the error with `errors.As` if you
need the file/id fields:

```go
package mischief_demo

import (
	"errors"
	"fmt"

	"github.com/trouble-agent/mischief/internal/catalog"
)

func loadDemo() error {
	// From an explicit directory:
	cat, err := catalog.LoadDir("catalog/faults")
	if err != nil {
		var le *catalog.LoadError
		if errors.As(err, &le) {
			return fmt.Errorf("catalog refused: %v", le) // "file (id): reason"
		}
		return err
	}

	// Or from the in-repo catalog, resolved from the working directory
	// upward to the repo root (the directory holding go.mod):
	cat, err = catalog.LoadDefault()
	if err != nil {
		return err
	}

	fmt.Println(cat.VersionScheme, cat.Len(), cat.SourceDir)
	for _, id := range cat.IDs { // sorted; stable iteration order
		d := cat.Get(id)
		if d.Tier != nil && d.Tier.Min == 0 {
			fmt.Println(id, "runs at L0")
		}
	}
	return nil
}
```

From a loaded catalog you can ask two questions of each primitive. Whether it
exists and what its contract says: `cat.Get(id)` returns the `*Descriptor`
(or nil). Whether it can run on this host:

```go
reg := catalog.RegistryFunc(func(kind string) bool {
	return kind == "NET_ADMIN" // pretend this host has NET_ADMIN only
})
st := d.Status(reg)
// st.Status == catalog.StatusReady, or
// st.Status == catalog.StatusCapabilityUnavailable && st.UnavailableNames == "docker"
```

`CapabilityRegistry` is the seam (AC-11): availability is derived, never
probed here. A primitive with capability `none` is ready regardless; anything
else is asked of the registry, and a "no" keeps the primitive listed with
`StatusCapabilityUnavailable` and the absent capability named — the verb stays
available either way.

## Descriptor schema overview

A descriptor is one YAML file per primitive. Required fields (the loader
refuses a file missing any of them, naming the field):

| Field | Go type | Meaning |
|---|---|---|
| `id` | `string` | unique primitive id (`P-001`) |
| `name` | `string` | short human name |
| `what` | `string` | what the fault does (prose) |
| `breaks` | `string` | what it breaks — the failure mode under test |
| `params` | `Params` | declared param schema (see below); required non-empty |
| `landed_proof` | `LandedProof` | `{kind, check}` — the independent observable proving the fault landed (AC-13 shape) |
| `inverse` | `Inverse` | reverter contract: action (primitive id or named action), optional `params`, optional `verify` condition |
| `capability` | `Capability` | `none` or a named host capability (`NET_ADMIN`, `cgroup2`, `docker`, ...), with optional prose detail |
| `tier` | `*Tier` | scope-ladder placement; any `L0..L5` token in the string sets `Tier.Min` (the minimum scope) |
| `backend` | `string` | injection backend (`sig`, `shim`, `net`, `docker`, `sim`, ... — legend in FAULT-CATALOG.md) |
| `maturity` | `string` | declared maturity (`planned`, `landed`, ...) |
| `selftest` | `SelftestState` | `green`/`missing`/`failing`/`unknown`; **absent decodes to `unknown`**, which refuses outside L0 (AC-19) |

`params` is a mapping of name to declaration. A scalar declaration is parsed:
a trailing `?` marks the param optional, `|`-separated tokens are known kinds
(`str`, `int`, `float`, `bool`, `dur`, `ip:port`, `selector`, `list`, `any`)
or enumeration values, and a declaration naming no known kind keeps the value
verbatim in `Decl` with kind `any` (nothing is guessed into a type). A sequence
declaration is a value list (`syscalls: [clock_gettime, gettimeofday]` → kind
`list` with `Values`).

Note: `params` is required and must be non-empty — a descriptor with an empty
`params` mapping is refused. The fault contract itself — every primitive MUST
state `landed_proof`, `inverse` and `capability` — is enforced by the loader,
not by convention. SPEC-01's not-list (what the catalog does *not* promise) is
in the package doc comment (`go doc ./internal/catalog`).

## Catalog versioning

A catalog's `Version` is **content-derived**, scheme `content-v1` (named in
`VersionScheme`): `hex(sha256)` over the sorted `id <NUL> raw-file-bytes`
pairs of every loaded descriptor. Properties that matter:

- **Changing bytes changes the version.** Adding, editing or removing a
  descriptor file changes `Version`; the engine never needs a manual
  version bump.
- **It tracks the on-disk inputs exactly.** The hash covers the raw file
  bytes *before* the loader's YAML pre-pass repairs, so two catalogs with
  identical bytes have identical versions regardless of platform.
- **It proves nothing beyond the bytes.** The version is not a semantic
  version and carries no compatibility meaning — it identifies descriptor
  content, nothing more.

Read it for cache keys, run reports, or "which catalog did this run use"
questions:

```go
cat, err := catalog.LoadDefault()
if err != nil {
	return err
}
fmt.Println(cat.VersionScheme) // "content-v1"
fmt.Println(cat.Version)       // 64 hex chars
```

## Where to look next

- `docs/FAULT-CATALOG.md` — the human-readable catalog of all primitives.
- `docs/SPEC-PLAN.md` — SPEC-01's contract and not-list.
- `go doc ./internal/catalog` — the full godoc surface.
