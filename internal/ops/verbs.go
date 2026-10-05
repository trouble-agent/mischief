package ops

import (
	"fmt"
	"sort"
	"strings"
)

// This file owns THE CANONICAL VERB TABLE — the single source of truth for
// the privileged surface (SPEC-11): the sudoers drop-in is generated from
// it, the enforcement check consults it, the audit diffs the installed
// drop-in against a render of it. One table; everything else derived.

// ExecKind classifies how a table row is granted/executed.
type ExecKind string

const (
	// ExecSudo: the verb runs via the sudoers drop-in (a root-level
	// command with an argument shape fixed by the table).
	ExecSudo ExecKind = "sudo"
	// ExecRootlessDelegated: the verb needs no privilege — it runs inside
	// a delegated, mischief-owned subtree (cgroup v2 files). The drop-in
	// deliberately carries NO grant for it; the audit and doctor report
	// the delegation with its reason instead.
	ExecRootlessDelegated ExecKind = "rootless-delegated"
)

// Verb is one row of the canonical privileged-verb table.
type Verb struct {
	// ID is the table key ("netns_add") — stable, sorted by.
	ID string
	// Exec is how the verb is granted (sudo drop-in row vs delegation).
	Exec ExecKind
	// Binary is the absolute path of the program the verb runs.
	Binary string
	// Args is the fixed argument shape. One-element rows carrying a
	// leading "*" are WILDCARD rows: the drop-in grants that binary with
	// any arguments and enforcement refuses by policy (wildcards are
	// measured in the audit, never silent).
	Args []string
	// Backend names the SPEC-06/07/08 backend that consumes the verb
	// (documentation for the audit reader).
	Backend string
	// Tier is the scope-ladder level the verb's primitives run at.
	Tier int
	// Why is the one-line reason the verb is on the table (rendered as a
	// comment in the drop-in so the installed artefact is self-explaining).
	Why string
}

// wildcard reports whether the row's args are a deliberate wildcard grant.
func (v Verb) wildcard() bool {
	return len(v.Args) == 1 && v.Args[0] == "*"
}

// sudoersUser is the user (or group form) the drop-in grants. The
// deployment on a sanctioned host runs mischief under its own unprivileged
// account; install overrides it.
const defaultSudoersUser = "mischief"

// VerbTable is the canonical table, in table order (netns → tc → device
// mapper → loop → filesystem → freeze → cgroup). ADD ROWS HERE ONLY: the
// drop-in, the enforcement and the audit all follow.
//
// Argument shapes: each grant pins the subcommand shape its backend
// issues. Wildcard rows are the two tc arms (tc's filter/ qdisc grammar is
// larger than any static argument list and the netem arms are the netns
// pair's inside-the-ns twin) — they are marked in the audit output so an
// operator sees exactly where the grant is broad.
func VerbTable() []Verb {
	return []Verb{
		{ID: "netns_add", Exec: ExecSudo, Binary: "/usr/sbin/ip", Args: []string{"netns", "add", "*"},
			Backend: "backend_net (N-001 partition)", Tier: 2,
			Why: "create the per-experiment network namespace"},
		{ID: "netns_del", Exec: ExecSudo, Binary: "/usr/sbin/ip", Args: []string{"netns", "del", "*"},
			Backend: "backend_net (N-001 inverse)", Tier: 2,
			Why: "delete the experiment's network namespace (the recorded inverse)"},
		{ID: "tc_netem", Exec: ExecSudo, Binary: "/usr/sbin/tc", Args: []string{"*"},
			Backend: "backend_net (N-001 netem arms)", Tier: 2,
			Why: "apply/clear netem latency-loss rules inside the experiment netns (wildcard: tc grammar is verb-shaped, not arg-list-shaped)"},
		{ID: "dmsetup", Exec: ExecSudo, Binary: "/usr/sbin/dmsetup", Args: []string{"*"},
			Backend: "backend_resource (S-002 dm-error)", Tier: 3,
			Why: "create/remove the throwaway dm-error table on the experiment's scratch device (wildcard: dmsetup table grammar)"},
		{ID: "losetup", Exec: ExecSudo, Binary: "/usr/sbin/losetup", Args: []string{"*"},
			Backend: "backend_resource (scratch loop devices)", Tier: 3,
			Why: "attach/detach the scratch file's loop device (wildcard: attach and detach share one binary)"},
		{ID: "mkfs", Exec: ExecSudo, Binary: "/usr/sbin/mkfs.ext4", Args: []string{"*"},
			Backend: "backend_resource (scratch filesystems)", Tier: 3,
			Why: "format the experiment's OWN scratch loop device — never a named host filesystem"},
		{ID: "fsfreeze", Exec: ExecSudo, Binary: "/usr/sbin/fsfreeze", Args: []string{"--freeze", "*"},
			Backend: "backend_resource (S-003 fsfreeze)", Tier: 3,
			Why: "freeze the scratch mount only; the inverse (unfreeze) rides the same grant with the inverse arg"},
		{ID: "fsfreeze_unfreeze", Exec: ExecSudo, Binary: "/usr/sbin/fsfreeze", Args: []string{"--unfreeze", "*"},
			Backend: "backend_resource (S-003 inverse)", Tier: 3,
			Why: "unfreeze the scratch mount (the recorded inverse)"},
		{ID: "cgroup_create", Exec: ExecRootlessDelegated, Binary: "(cgroupfs subtree)", Args: nil,
			Backend: "backend_resource (R-001 scopes)", Tier: 1,
			Why: "create the experiment's cgroup under the user's DELEGATED subtree — rootless by design; no sudo grant exists for cgroupfs writes"},
	}
}

// SortedVerbs returns the table ordered by ID (stable renders and diffs).
func SortedVerbs() []Verb {
	vs := VerbTable()
	sort.Slice(vs, func(i, j int) bool { return vs[i].ID < vs[j].ID })
	return vs
}

// SudoVerbs returns the sudo-granted rows (the ones a drop-in may carry).
func SudoVerbs() []Verb {
	var out []Verb
	for _, v := range SortedVerbs() {
		if v.Exec == ExecSudo {
			out = append(out, v)
		}
	}
	return out
}

// Lookup finds a table row by id.
func Lookup(id string) (Verb, bool) {
	for _, v := range VerbTable() {
		if v.ID == id {
			return v, true
		}
	}
	return Verb{}, false
}

// DropInName is the drop-in file's basename inside /etc/sudoers.d.
const DropInName = "mischief"

// DefaultDropInPath is where the sudoers drop-in installs (sudoers.d
// drop-in, never an edit of the main sudoers file).
const DefaultDropInPath = "/etc/sudoers.d/" + DropInName

// DefaultHelperManifestPath is where the helper manifest installs (inside
// the state prefix; a JSON render of the verb table a future helper
// binary executes against).
const DefaultHelperManifestPath = "/var/lib/mischief/helper-manifest.json"

// DefaultPrefix is the install prefix for mischief's own state.
const DefaultPrefix = "/var/lib/mischief"

// RenderDropIn renders the canonical sudoers drop-in from the verb table
// (plus header comments naming provenance). Deterministic: same table,
// same bytes — install can byte-compare an existing drop-in against a
// fresh render to detect drift. user is the sudoers grantee.
func RenderDropIn(user string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# /etc/sudoers.d/%s — GENERATED by `mischief install` (SPEC-11).\n", DropInName)
	b.WriteString("# DO NOT EDIT: regenerate with `mischief install` — the enforcement verb table\n")
	b.WriteString("# is the source of truth; a hand-edited drop-in fails the privilege audit.\n")
	b.WriteString("# Each line grants exactly one command shape; no ALL, no shell redirects,\n")
	b.WriteString("# no NOEXEC-free escapes. cgroup create/write is deliberately absent:\n")
	b.WriteString("# cgroup v2 delegation is rootless (see helper-manifest.json).\n")
	fmt.Fprintf(&b, "Cmnd_Alias MISCHIEF_CMNDS = \\\n")
	lines := sudoersLines(SudoVerbs())
	for i, l := range lines {
		b.WriteString("    " + l)
		if i < len(lines)-1 {
			b.WriteString(", \\\n")
		} else {
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "%s ALL=(root) NOPASSWD: SETENV: MISCHIEF_CMNDS\n", user)
	return []byte(b.String())
}

// sudoersLines renders one drop-in line per sudo verb: "binary args".
// A wildcard row renders just the binary (the sudoers wildcard), and its
// audit row says so.
func sudoersLines(vs []Verb) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		if v.wildcard() {
			out = append(out, v.Binary)
			continue
		}
		out = append(out, v.Binary+" "+strings.Join(v.Args, " "))
	}
	return out
}

// RenderHelperManifest renders the helper manifest: a JSON document of the
// FULL table (sudo + delegated rows) that a future helper binary executes
// against — its allowlist is already fixed by this table. Deterministic
// (sorted rows; struct fields in declared order).
func RenderHelperManifest() []byte {
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString("  \"helper\": \"mischief-privileged-helper\",\n")
	b.WriteString("  \"milestone\": \"manifest-only — the helper binary is a later milestone; the allowlist it must enforce is fixed here\",\n")
	b.WriteString("  \"verbs\": [\n")
	vs := SortedVerbs()
	for i, v := range vs {
		args := "null"
		if v.Args != nil {
			var qs []string
			for _, a := range v.Args {
				qs = append(qs, fmt.Sprintf("%q", a))
			}
			args = "[" + strings.Join(qs, ", ") + "]"
		}
		comma := ","
		if i == len(vs)-1 {
			comma = ""
		}
		fmt.Fprintf(&b, "    {\"id\": %q, \"exec\": %q, \"binary\": %q, \"args\": %s, \"backend\": %q, \"tier\": %d, \"why\": %q}%s\n",
			v.ID, string(v.Exec), v.Binary, args, v.Backend, v.Tier, v.Why, comma)
	}
	b.WriteString("  ]\n}\n")
	return []byte(b.String())
}

// ParseDropInVerbs extracts the granted command lines from a drop-in body
// (the audit's read side): returns the command-token lines with comments
// and the grant line stripped. A line inside the Cmnd_Alias continuation
// is a granted command; everything else is ignored.
func ParseDropInVerbs(body string) []string {
	var out []string
	inAlias := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "Cmnd_Alias") {
			inAlias = true
			line = strings.TrimPrefix(line, "Cmnd_Alias")
			line = strings.TrimSpace(line)
			// strip the alias name and "="
			if eq := strings.Index(line, "="); eq >= 0 {
				line = strings.TrimSpace(line[eq+1:])
			}
		}
		if !inAlias {
			continue
		}
		// A grant line ends the alias block when it no longer continues.
		cont := strings.HasSuffix(line, "\\")
		line = strings.TrimSuffix(line, "\\")
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, ",")
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
		if !cont {
			inAlias = false
		}
	}
	return out
}

// sanitizeOneLine collapses a multi-line string into one audit-safe line.
func sanitizeOneLine(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
