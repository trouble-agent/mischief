// Package ops is SPEC-11: the operations layer — install/uninstall, the
// privileged helper surface (helper manifest + sudoers drop-in), the
// privilege audit, and journal retention/rotation.
//
// Design authority: docs/SPEC-PLAN.md (SPEC-11 operations) and
// docs/prd/mischief-v0.1.md §7 (the safety doctrine: the allowlist is the
// control, not a sudo prompt) and §9 (platform audit: AC-11 names what is
// absent; AC-6's kill switch stays measured).
//
// THE CONTROL (this package's central object) is VerbTable: one canonical,
// in-code table of the privileged verbs mischief's backends may execute via
// sudo. The sudoers drop-in is GENERATED from that table, the enforcement
// check (Check) consults the SAME table, and the privilege audit diffs the
// INSTALLED drop-in against a render of the SAME table — so the drop-in and
// the enforcement can never drift: there is one table, and everything else
// is derived from it. The test DropInMatchesVerbTable pins the round trip
// (render → parse → compare against the table) with a mutation arm proving
// the comparison can fail.
//
// WHAT SHIPPED (explicit, per the brief): this is the MANIFEST milestone —
// the helper BINARY is a later milestone. The privileged surface that ships
// today is exactly:
//
//   - the verb table (internal/ops/verbs.go), rendered to a sudoers
//     drop-in by install, and
//   - the helper MANIFEST: a JSON rendering of the same table installed
//     into the state dir, which a future helper binary will execute
//     against (its allowlist is already fixed here).
//
// No setuid binary anywhere in this design: the privileged surface is the
// one drop-in listing the allowed commands, and nothing else.
//
// Cgroup create/write is deliberately NOT a sudoers command: cgroup v2
// delegation (SPEC-07's R-001 scopes) is rootless — a mischief-prefixed
// subtree under the user's own delegated cgroup needs no privilege, and
// granting raw cgroupfs writes through sudoers cannot be expressed safely
// (a file-write grant is a shell escape). The table carries the cgroup rows
// marked RootlessDelegated so the audit and the doctor report the
// DELEGATION (with its reason) instead of pretending a grant exists.
//
// DRY-RUN DISCIPLINE: every filesystem-touching operation (install,
// uninstall, retention --apply) is expressed as a Plan ([]Action) first;
// --dry-run returns the plan unexecuted. Tests run every operation against
// a Root override (a temp dir); the package's own tests never write outside
// t.TempDir().
//
// NOT-LIST (what SPEC-11 does not promise here):
//
//   - No privilege escalation of its own: install never elevates; writing
//     the drop-in requires the caller to already hold the rights (root, or
//     a privileged deployment step on the sanctioned host). A failed write
//     is a named refusal, not a retry-with-more-rights.
//   - The helper binary is not here: nothing in this package executes the
//     table's commands. Check is the enforcement-side matcher for the
//     milestone that ships it.
//   - No sudoers syntax rewriting: real-mode install validates the
//     generated drop-in with visudo before it lands, and refuses (fail
//     closed) when visudo is unavailable — it never installs an unchecked
//     drop-in.
//   - Retention is a rotation over run dirs by name→mtime; it never
//     inspects journal contents (that is internal/journal's boundary).
//
// ch:trace row=MSF-012 spec=docs/SPEC-PLAN.md#SPEC-11 evidence=internal/ops/ witness=none:no-real-install-run-on-dev-host
package ops
