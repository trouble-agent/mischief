# Security policy

`mischief` is a fault-injection tool: it deliberately breaks things — files,
processes, containers, network paths — to prove a system recovers. Treat an
uncontrolled instance of it as an incident.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting:

<https://github.com/trouble-agent/mischief/security/advisories/new>

If you cannot use advisories, mail the maintainers via the address on the
`trouble` sibling's SECURITY policy with `mischief security` in the subject.
Please do not open a public issue for a suspected vulnerability.

Include, as far as you can:

- the commit or release the issue applies to (`mischief version` prints the
  stamped git sha; `0.0.0-dev` means an unstamped build from a tagless tree),
- the primitive/backend involved (e.g. `S-001` LD_PRELOAD shim, `docker` backend),
- what the tool did vs. what its journal and verdict claimed.

## Scope notes

- **Rehearsal artifacts are not attack surface.** Files under
  `catalog/experiments/` are rehearsal-only by design; their selectors do not
  resolve and their placeholders are never substituted. A report that only
  exists if someone edits a rehearsal YAML into a live target is still worth
  a note, but the safety headers on those files are the intended control.
- **The isolation contract is security-relevant.** `mischief` refuses to run
  on a host without an explicit sanction marker (fail-closed; SPEC-13). A way
  to bypass the sanction gate or land a fault outside a sanctioned host is a
  high-severity report.
- **No default target, ever.** Anything that lets a fault reach an implicit
  or inferred target is in scope.

## Supported versions

The project has not cut its first release yet; `main` is the only supported
line. Fixes land on `main` and ship with the next tagged build.
