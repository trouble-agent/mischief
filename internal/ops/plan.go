package ops

import (
	"fmt"
	"sort"
	"strings"
)

// This file owns the PLAN shape (SPEC-11's dry-run contract) and the
// allowlist Check (the enforcement-side matcher the helper milestone will
// call). Every filesystem-touching operation renders its full Action list
// BEFORE executing anything; --dry-run stops there.

// OpKind is the closed set of filesystem actions a plan may carry.
type OpKind string

const (
	// OpWriteDir: mkdir -p.
	OpWriteDir OpKind = "mkdir"
	// OpWriteFile: write bytes with the given mode (fail closed on
	// unexpected existing content — overwrite only what this package
	// generated).
	OpWriteFile OpKind = "write"
	// OpRemove: remove one file (retention / uninstall).
	OpRemove OpKind = "remove"
	// OpRemoveDir: remove one EMPTY dir (uninstall).
	OpRemoveDir OpKind = "rmdir"
)

// Action is one planned filesystem mutation. Path is the ROOT-RELATIVE
// logical path ("/etc/sudoers.d/mischief"); Root maps it to the real
// filesystem in tests and dry-run preview.
type Action struct {
	// Kind is the action type (closed vocabulary above).
	Kind OpKind
	// Path is the logical absolute path the action touches.
	Path string
	// Mode is the file mode for OpWriteFile ("0644" drop-in vs "0440"
	// sudoers semantics — the real file must be 0440 or sudo ignores it).
	Mode string
	// Bytes carries the payload for OpWriteFile (nil otherwise). Rendered
	// in DryRunString as a length, never as content.
	Bytes []byte
	// Reason names WHY the action exists (one line — the dry-run plan is
	// operator-readable).
	Reason string
}

// DryRunString renders the action as one operator-readable plan line —
// byte lengths, never payloads (the drop-in body is not a secret, but the
// plan log is not the place to dump file contents).
func (a Action) DryRunString() string {
	switch a.Kind {
	case OpWriteFile:
		return fmt.Sprintf("write %s mode=%s bytes=%d — %s", a.Path, a.Mode, len(a.Bytes), a.Reason)
	case OpWriteDir:
		return fmt.Sprintf("mkdir %s — %s", a.Path, a.Reason)
	case OpRemove:
		return fmt.Sprintf("remove %s — %s", a.Path, a.Reason)
	case OpRemoveDir:
		return fmt.Sprintf("rmdir %s — %s", a.Path, a.Reason)
	default:
		return fmt.Sprintf("%s %s — %s", a.Kind, a.Path, a.Reason)
	}
}

// Plan is an ordered list of actions plus the operation's identity. A plan
// with zero actions is legitimate (uninstall when nothing is installed).
type Plan struct {
	// Op names the operation ("install", "uninstall", "retention").
	Op string
	// DryRun: true when the caller asked for the plan only.
	DryRun bool
	// Actions is the ordered action list (order matters: mkdir before
	// write, files before their dir rmdir).
	Actions []Action
}

// Render renders the plan as operator-readable text.
func (p Plan) Render() string {
	var b strings.Builder
	state := "PLAN"
	if p.DryRun {
		state = "DRY-RUN PLAN (nothing executed)"
	}
	fmt.Fprintf(&b, "%s %s: %d action(s)\n", state, p.Op, len(p.Actions))
	for _, a := range p.Actions {
		b.WriteString("  " + a.DryRunString() + "\n")
	}
	if len(p.Actions) == 0 {
		b.WriteString("  (nothing to do)\n")
	}
	return b.String()
}

// Check is the enforcement-side allowlist matcher: may the helper execute
// `binary args...`? It consults the SAME VerbTable the drop-in is
// generated from, so a drop-in row and an accepted argv can never drift.
//
// Policy (fail closed):
//   - exact-shape rows: binary AND every declared arg must match, and the
//     wildcard tail row is accepted only for the declared prefix;
//   - wildcard rows (tc, dmsetup, losetup, mkfs): binary must match; the
//     ARGS are NOT checked here — the breadth is deliberate and measured
//     by Audit, not hidden;
//   - rootless-delegated rows are never satisfiable via sudo (an empty
//     match on those is a false grant).
func Check(binary string, args []string) error {
	for _, v := range SudoVerbs() {
		if v.Binary != binary {
			continue
		}
		if v.wildcard() {
			return nil
		}
		if argsMatch(v.Args, args) {
			return nil
		}
	}
	return fmt.Errorf("verbs: %s %s is not in the mischief verb table (the sudoers drop-in grants exactly the table; nothing else may run via sudo)", binary, strings.Join(args, " "))
}

// argsMatch: declared args are matched in order; a trailing "*" accepts
// any tail of >=1 arg (the netns/dm name positions).
func argsMatch(declared, given []string) bool {
	for i, d := range declared {
		if d == "*" {
			return len(given) > i // any non-empty tail
		}
		if i >= len(given) || given[i] != d {
			return false
		}
	}
	return len(given) == len(declared)
}

// Audit is the privilege audit's result (SPEC-11: "dumps the effective
// privileged surface as JSON for an operator to diff").
type Audit struct {
	// GeneratedAt omitted BY DESIGN: the audit is a DIFF surface, not a
	// log line — the operator diffs two runs with diff(1) and a clock
	// inside the JSON would make identical states differ.
	// DropInPath is where the drop-in is expected.
	DropInPath string `json:"drop_in_path"`
	// DropInInstalled: the drop-in file exists and is readable.
	DropInInstalled bool `json:"drop_in_installed"`
	// DropInMatches: the installed body is byte-identical to a fresh
	// render of the canonical verb table (false = DRIFT: hand-edited or
	// stale).
	DropInMatches bool `json:"drop_in_matches"`
	// DropInDiff carries the operator-readable difference (expected line
	// vs found line) when DropInMatches is false.
	DropInDiff string `json:"drop_in_diff,omitempty"`
	// HelperManifestPath is where the manifest is expected.
	HelperManifestPath string `json:"helper_manifest_path"`
	// HelperManifestInstalled: the manifest file exists and is readable.
	HelperManifestInstalled bool `json:"helper_manifest_installed"`
	// HelperManifestMatches: byte-identical to a fresh render.
	HelperManifestMatches bool `json:"helper_manifest_matches"`
	// SetuidBinaries lists setuid/setgid binaries found under the install
	// prefix (expected: NONE — the design has no setuid surface; a finding
	// here is a red flag, reported not deleted).
	SetuidBinaries []string `json:"setuid_binaries"`
	// Verbs is the effective table with per-verb grant posture.
	Verbs []AuditVerb `json:"verbs"`
	// Notes carries posture lines that are data, not verdicts (e.g. the
	// sudoers user the installed drop-in grants).
	Notes []string `json:"notes,omitempty"`
}

// AuditVerb is one table row's audit line.
type AuditVerb struct {
	ID     string `json:"id"`
	Exec   string `json:"exec"`
	Binary string `json:"binary"`
	// GrantedInDropIn: a drop-in line grants this row's binary+shape
	// (rootless-delegated rows are always false — see the Reason).
	GrantedInDropIn bool `json:"granted_in_drop_in"`
	// WildcardGrant: the drop-in grant for this row is a wildcard (broad
	// by design; measured here, never silent).
	WildcardGrant bool `json:"wildcard_grant"`
	// Reason is the one-line why (table's Why, or the delegation note).
	Reason string `json:"reason"`
}

// AuditPrefix (json:"install_prefix") is exported via the JSON builder in
// audit.go; the field lives there to keep this struct's zero value honest.

// AuditError is a named audit failure: the surface could not be measured.
type AuditError struct{ Reason string }

func (e *AuditError) Error() string { return "audit: " + e.Reason }

// sortStrings sorts in place (small local helper; keeps the audit output
// deterministic without importing sort at every callsite).
func sortStrings(s []string) { sort.Strings(s) }
