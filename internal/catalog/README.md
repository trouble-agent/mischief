# internal/catalog — SPEC-01

The fault model and the primitive catalog: what a primitive descriptor is,
what the loader refuses, how the catalog versions itself, and how AC-11 /
AC-19 are derivable from the schema alone. Design authority is
`docs/SPEC-PLAN.md` (SPEC-01) and `docs/prd/mischief-v0.1.md`; this file only
summarizes the package contract.

## The descriptor

One YAML file per primitive in `catalog/faults/` (read-only inputs — the
loader never writes them). The Go shape is `Descriptor`:

| field | type | required | notes |
|---|---|---|---|
| `id` | string | yes | the primitive id ("F-009"); duplicates refused |
| `name` | string | yes | |
| `what` | string | yes | prose; may wrap over indented lines |
| `breaks` | string | no | what the fault breaks (prose) |
| `params` | map name -> decl | yes | see "Param declarations" |
| `landed_proof` | {kind, check} | **yes** | the fault contract |
| `inverse` | action / mapping | **yes** | the fault contract |
| `capability` | string | **yes** | the fault contract; split into Kind+Detail |
| `tier` | string | yes | must name an L0..L5 level; `Min` = lowest named |
| `backend` | string | yes | ("fs", "shim", "net", "docker", ...) |
| `maturity` | string | yes | ("proven", "L1-only", ...) |
| `selftest` | enum | no | `green`/`missing`/`failing`/`unknown`; absent = `unknown` |

### The fault contract

Every primitive MUST state `landed_proof` + `inverse` + `capability`. The
loader REFUSES a descriptor missing any of the three, naming the field
(`inverse: missing (the fault contract: ...)`). A primitive that passes the
loader needs no engine code change: the catalog is data.

### Inverse forms

- bare action string: `inverse: restore-original`
- mapping: `inverse: {action: delete-qdisc, verify: "..."}`
- self-inverse primitive: `inverse: {primitive: P-001, params: {signal: SIGCONT}, verify: "..."}`
  (normalised to `Action: "P-001"` + `Params`)

### Param declarations

A mapping of name -> declaration string. A declaration is: optional `?`
suffix, pipe-separated tokens; each token is either a known kind (`str`,
`int`, `float`, `bool`, `dur`, `ip:port`, `selector`, `list`, `any`) or an
enum value. Examples from the corpus:

- `path: str` -> typed str, required
- `mode: replace|unlink-then-create|restore-snapshot` -> enum of 3, str
- `peer: ip:port?` -> typed ip:port, optional
- `syscalls: [clock_gettime, gettimeofday]` -> value list
- `memory_max: 64M` -> single example token: verbatim Decl, untyped (`any`)

Nothing is guessed: a declaration that names no kind reports `Kinds: [any]`
and `Typed: false`; an empty declaration is refused by name.

### Selftest (AC-19)

`SelftestState.RefusesOutsideL0()` is the AC-19 refusal predicate: any value
except `green` refuses to run above an L0 scratch scope. An absent field
defaults to `unknown` (refuses) — the schema can express "no green selftest"
without a convention of a missing field meaning allowed.

## Capability surface (AC-11)

`capability: none` means nothing beyond the backend machinery. Any other
value ("NET_ADMIN", "cgroup2 + user scope", "docker API") is split into a
Kind token + Detail prose. Availability is derived, not stored:

```go
reg := catalog.RegistryFunc(func(kind string) bool { return hosts.CanDo(kind) })
st := d.Status(reg) // StatusReady, or StatusCapabilityUnavailable + UnavailableNames
```

The catalog never probes the host; the engine (or a test) supplies the
`CapabilityRegistry` seam. A primitive with an unavailable capability stays
listed — `mischief catalog` keeps it, and the status names what is absent.

## Versioning

`Catalog.Version` is content-derived (scheme `content-v1`):
hex(sha256) over the sorted `id<NUL>raw-file-bytes` pairs. Adding a primitive
or editing a byte changes the version; the engine never does. The version
proves nothing beyond the bytes it hashes (see NOT-LIST).

## The repair pre-pass (read this before "fixing" the YAML)

All 10 `catalog/faults/*.yaml` inputs are syntactically INVALID YAML (strict
yaml.v3 refuses every file). The corpus was authored with prose continuations
inside plain scalars. The loader carries a small, named repair pre-pass that
runs BEFORE strict parsing and rewrites exactly four mechanical classes —
nothing else, and everything left is still parsed strictly:

| rule | shape | repair |
|---|---|---|
| `prose-continuation-fieldish` | `breaks: ...` indented under `what:` prose | re-pointed as the mapping entry it was meant to be |
| `prose-continuation-coloned` | prose with an embedded `": "` (`(the TRBL-031 shape: ...)`) | key line + segment merged into one quoted scalar |
| `flow-scalar-reserved-char` | flow value with a bare `:` (`ip:port?`) or trailing `?` (`dur?`) | the whole value quoted |
| — | `landed_proof:` children (`kind:`, `check:`) | never touched (block opener, not a scalar) |

Every applied repair lands in `Catalog.Warnings` (`"rule: file"`). The
upstream fix is a one-line edit per file (indent `breaks` flush, quote the
prose/flow values) — at which point the pre-pass fires nowhere and the
warnings list goes empty. Until then the repairs are load-bearing.

## NOT-LIST (what SPEC-01 does not promise)

- The catalog does not promise a primitive can land on this host; it promises
  the descriptor states what landing and reverting must prove
  (`landed_proof` / `inverse`) and what the host must supply (`capability`).
- It does not promise every fault is revertible; it promises every descriptor
  carries an inverse record, and refuses a primitive whose inverse cannot be
  expressed.
- It does not promise a green selftest: the schema expresses selftest state
  (`SelftestState`, default `unknown`) and downstream code enforces the AC-19
  refusal; this package neither runs nor grades selftests.
- It does not grade or execute anything: no landing, no inverse execution, no
  host probing — `Status()` consults the registry seam.
- Param declarations are taken verbatim (`Decl`); a param whose declaration
  names none of the known kinds reports `Kinds: [any]`, `Typed: false`
  rather than being guessed into a type.
- The content version proves nothing beyond the bytes it hashes.
