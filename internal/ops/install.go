package ops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// This file owns install/uninstall (SPEC-11): plan construction from the
// canonical verb table, real execution, and the audit survey. Every path
// resolves through Root: "" is the production filesystem; a test override
// confines every action to a temp dir.

// Root is the filesystem root all logical paths resolve against. Empty =
// "/" (production). Package-level var so tests can pin a temp root; never
// mutated outside tests.
var Root = ""

// UIDOf is the seam tests pin to drive the privileged pre-screen without
// changing the real process identity. Empty = os.Getuid() (production);
// tests set a literal uid (0 = root, anything else = unprivileged). Same
// discipline as Root: package-level, never mutated outside tests.
var UIDOf = func() int { return os.Getuid() }

// resolve maps a logical absolute path onto the active root.
func resolve(logical string) string {
	if Root == "" {
		return logical
	}
	return filepath.Join(Root, logical)
}

// InstallOpts carries install's inputs.
type InstallOpts struct {
	// SudoersUser is the account the drop-in grants (default: the
	// mischief service account). Validated by ValidateSudoersUser before
	// interpolation (MISCHIEF-DF-50): the grantee lands verbatim in the
	// drop-in's final grant line, so a crafted value is a privilege
	// escalation, and the visudo guard is a SYNTAX check that accepts it.
	SudoersUser string
	// Prefix is mischief's state prefix (default /var/lib/mischief).
	Prefix string
	// DryRun: build and return the plan; execute nothing.
	DryRun bool
	// SkipDropIn (test seam): plan everything except the drop-in itself —
	// used by tests that cannot write /etc on the dev host even under a
	// temp root is not enough because they ALSO assert the drop-in plan
	// line exists. Never set in production.
	SkipDropIn bool
	// ValidateDropIn: run visudo over the generated drop-in before it
	// lands (real installs set this via cmd wiring; the plan-only path
	// never needs it).
	ValidateDropIn bool
	// VisudoPath overrides the visudo binary (tests inject a stub).
	VisudoPath string
}

// defaults fills zero fields.
func (o *InstallOpts) defaults() {
	if o.SudoersUser == "" {
		o.SudoersUser = defaultSudoersUser
	}
	if o.Prefix == "" {
		o.Prefix = DefaultPrefix
	}
}

// ch:trace row=MISCHIEF-DF-50 spec=internal/ops/install.go#user-validation test=go-test-internal-ops evidence=internal/ops/install_test.go witness=cmd:go-test
// ValidateSudoersUser gates the one value install interpolates into the
// privileged drop-in (MISCHIEF-DF-50). RenderDropIn writes the grantee
// verbatim into the final grant line, so a crafted --user value is a
// sudoers injection: `install --user "mischief ALL=(root) NOPASSWD:
// MISCHIEF_CMNDS\nkara ALL=(root) NOPASSWD: ALL #"` renders a second
// grant line (full passwordless root for kara) with the trailing template
// text neutralized by the embedded '#', and visudo answers "parsed OK" —
// the documented guard checks SYNTAX, not intent. Fail closed on:
//   - empty values (an unvalidated grantee must never reach the render);
//   - "ALL" and any other sudoers KEYWORD (ALL, NOPASSWD, PASSWD, the
//     Alias keywords — case-insensitive, the canonical matching is) as a
//     full token: host-wide grants and alias trickery are policy, not a
//     username;
//   - newlines, carriage returns, or shell/sudoers metacharacters
//     (;/|&$`()<>) that could terminate or reshape the grant line;
//   - any whitespace beyond a single space between characters (a value
//     with spaces is never an account name).
//
// The accepted shape is a plain account name: [A-Za-z0-9_-], 1–32 chars
// (getconf LOGIN_NAME_MAX-ish conservative bound). Template is untouched —
// the render stays canonical; only the input is gated.
func ValidateSudoersUser(u string) error {
	const grantee = DefaultDropInPath
	if u == "" {
		return &InstallError{Path: grantee, Reason: "empty --user: the sudoers grantee must be an explicit account name (default: " + defaultSudoersUser + ")"}
	}
	if strings.EqualFold(u, "ALL") {
		return &InstallError{Path: grantee, Reason: "--user ALL is a host-wide grant, not an account — install refuses it"}
	}
	for _, kw := range []string{"ALL", "NOPASSWD", "PASSWD", "Cmnd_Alias", "User_Alias", "Host_Alias", "Runas_Alias"} {
		if strings.EqualFold(u, kw) {
			return &InstallError{Path: grantee, Reason: "--user carries the sudoers keyword " + u + " — policy is set by the verb table, never by the flag"}
		}
	}
	for _, r := range u {
		switch {
		case r == '\n' || r == '\r':
			return &InstallError{Path: grantee, Reason: "--user carries a line break — a second grant line in the drop-in is sudoers injection"}
		case strings.ContainsRune(";|&$`()<>", r):
			return &InstallError{Path: grantee, Reason: fmt.Sprintf("--user carries the metacharacter %q — refuse, never interpolate", string(r))}
		case r == ' ':
			return &InstallError{Path: grantee, Reason: "--user carries whitespace — the grantee is one account name, not a grant spec"}
		case unicode.IsSpace(r):
			return &InstallError{Path: grantee, Reason: "--user carries whitespace (tab/other) — the grantee is one account name"}
		}
	}
	if len(u) > 32 {
		return &InstallError{Path: grantee, Reason: fmt.Sprintf("--user is %d characters (max 32) — not a plausible account name", len(u))}
	}
	for _, r := range u {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return &InstallError{Path: grantee, Reason: fmt.Sprintf("--user carries %q — the grantee must be [A-Za-z0-9_-] only", string(r))}
	}
	return nil
}

// dropInPath returns the drop-in path (fixed: /etc/sudoers.d/mischief —
// the prefix moves mischief's own state, never the sudoers drop-in).
func (o *InstallOpts) dropInPath() string { return DefaultDropInPath }

// privilegedDropInBlocked is the pre-screen: the drop-in target is FIXED
// (/etc/sudoers.d/mischief — dropInPath ignores the prefix by design), so
// an unprivileged caller planning an install is guaranteed to fail at the
// drop-in write (or half-install on a real /etc). Return the advisory line
// naming the blocker when both facts hold; "" when not applicable.
func (o *InstallOpts) privilegedDropInBlocked() string {
	if UIDOf() == 0 {
		return "" // root can write /etc: nothing to screen
	}
	if o.SkipDropIn {
		return "" // no drop-in action: no privileged target in the plan
	}
	if isPrivilegedPath(o.dropInPath()) {
		return fmt.Sprintf("%s requires root (sudo install) — an unprivileged prefix (%s) cannot carry %s", DefaultDropInPath, o.Prefix, DefaultDropInPath)
	}
	return ""
}

// isPrivilegedPath: only the top-level system dirs an unprivileged account
// cannot write. Anything deeper (temp roots, /tmp, $HOME) is writable.
func isPrivilegedPath(logical string) bool {
	top := strings.SplitN(strings.TrimPrefix(logical, "/"), "/", 2)[0]
	switch top {
	case "etc", "usr", "boot", "var", "proc", "sys":
		return true
	}
	return false
}

// manifestPath returns the helper manifest path under the prefix.
func (o *InstallOpts) manifestPath() string {
	return filepath.Join(o.Prefix, "helper-manifest.json")
}

// PlanInstall renders the full install plan (SPEC-11 order: dirs first,
// then files; drop-in last so a failure above it leaves no grant behind).
//
// Overwrite policy (fail closed): a write action over an EXISTING file is
// included only when the existing bytes are either byte-identical (idempotent
// re-install) or a previous mischief-generated artefact (drifted). An
// existing FOREIGN file at the drop-in path refuses the whole install —
// install never clobbers a file it did not generate.
func PlanInstall(opts InstallOpts) (*Plan, error) {
	opts.defaults()
	// MISCHIEF-DF-50: validate the grantee BEFORE interpolation. The
	// drop-in body is rendered from this value verbatim; the visudo
	// guard downstream is a syntax check and accepts crafted grants.
	if err := ValidateSudoersUser(opts.SudoersUser); err != nil {
		return nil, err
	}
	body := RenderDropIn(opts.SudoersUser)
	manifest := RenderHelperManifest()

	dropInPerm, err := checkForeignFile(opts.dropInPath(), body)
	if err != nil {
		return nil, err
	}
	// The manifest lives under the OPERATOR-owned prefix: a permission
	// refusal there is not the hardened-sudoers.d shape (the prefix is
	// this caller's own state), so it keeps failing closed like any
	// other unreadable file.
	if fact, err := checkForeignFile(opts.manifestPath(), manifest); err != nil {
		return nil, err
	} else if fact != nil {
		return nil, &InstallError{
			Path:   fact.logicalPath,
			Reason: "unreadable (permission denied) — the state prefix is operator-owned, so a permission refusal here is not the hardened-sudoers.d shape; fix the prefix permissions instead of planning blind",
		}
	}

	p := &Plan{Op: "install", DryRun: opts.DryRun}
	if adv := opts.privilegedDropInBlocked(); adv != "" {
		if dropInPerm != nil {
			// MISCHIEF-DF-27: fold the unmeasurable existing drop-in
			// into the DF-17 privileged advisory (one line — not a new
			// advisory kind). The overwrite policy is re-verified by
			// whichever identity performs the write (a sudo install
			// re-plans as root; the privileged helper re-verifies at
			// execution time).
			adv += fmt.Sprintf(" — existing %s unreadable (permission denied): the overwrite policy is re-verified by the privileged writer", DefaultDropInPath)
		}
		p.Advisories = append(p.Advisories, adv)
	} else if dropInPerm != nil {
		// The DF-17 screen is silent (root, or --no-drop-in) yet the
		// read was still refused: never plan silently over an
		// unverified target — the same fact stands alone as the
		// plan-warn line.
		p.Advisories = append(p.Advisories, fmt.Sprintf("%s unreadable (permission denied): existing content cannot be measured unprivileged — verify with a privileged reader", DefaultDropInPath))
	}
	p.Actions = append(p.Actions,
		Action{Kind: OpWriteDir, Path: opts.Prefix, Reason: "mischief state prefix"},
	)
	if !opts.SkipDropIn {
		p.Actions = append(p.Actions, Action{
			Kind: OpWriteFile, Path: opts.dropInPath(), Mode: "0440", Bytes: body,
			Reason: fmt.Sprintf("sudoers drop-in for %q generated from the canonical verb table (%d sudo verbs, %d wildcard)", opts.SudoersUser, len(SudoVerbs()), countWildcards()),
		})
	}
	p.Actions = append(p.Actions, Action{
		Kind: OpWriteFile, Path: opts.manifestPath(), Mode: "0644", Bytes: manifest,
		Reason: "privileged helper manifest (the allowlist a future helper binary enforces)",
	})
	return p, nil
}

// countWildcards counts the deliberately-broad sudo grants (audit metric).
func countWildcards() int {
	n := 0
	for _, v := range SudoVerbs() {
		if v.wildcard() {
			n++
		}
	}
	return n
}

// permDenied is the plan-warn fact checkForeignFile returns for an
// existing file it cannot read because of permissions (nil = the read
// succeeded or the file is absent): not a refusal, not a fake clean.
type permDenied struct{ logicalPath string }

func (p *permDenied) Error() string {
	return fmt.Sprintf("%s unreadable (permission denied)", p.logicalPath)
}

// readVerdict is the DF-27 read-error classification.
type readVerdict int

const (
	readAbsent     readVerdict = iota // no existing file: clean install
	readPermDenied                    // existing but unmeasurable (hardened dir): plan-warn
	readRefused                       // any other read failure (EIO, EISDIR, ...): refuse
)

// classifyReadError maps one ReadFile failure onto the DF-27 verdict.
// Only two error classes plan through: absence (nothing to clobber) and
// permission denial (hardened sudoers.d — see checkForeignFile). Every
// other failure keeps the fail-closed refusal.
//
// errors.Is, not os.IsPermission: measured on this toolchain (go1.26.5),
// os.IsPermission returns FALSE for a %w-wrapped EACCES while errors.Is
// unwraps it correctly — the read path wraps, so the un-wrap-capable
// matcher is the fail-safe one.
func classifyReadError(err error) readVerdict {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return readAbsent
	case errors.Is(err, fs.ErrPermission):
		return readPermDenied
	default:
		return readRefused
	}
}

// checkForeignFile refuses an install over an existing file that is
// neither byte-identical nor a previous mischief render (drift). The
// error names the path and both recovery routes.
//
// Read-error classification (MISCHIEF-DF-27, fail closed per class):
//   - os.IsNotExist: absent — clean install (unchanged).
//   - EACCES-class (os.IsPermission): a hardened drop-in dir (Debian's
//     default 0750 root:root /etc/sudoers.d) leaves the EXISTING file
//     unmeasurable by an unprivileged planner. That is a plan-warn fact,
//     not corruption: PlanInstall proceeds with an advisory (the SPEC-11
//     DF-17 privileged path — a sudo install re-plans as root and the
//     overwrite policy is re-verified by whichever identity writes).
//     Returned as a non-nil *permDenied fact with a nil error. The
//     manifest path keeps failing closed (see PlanInstall): the
//     operator-owned prefix cannot produce this shape.
//   - anything else (EIO, EISDIR at the path, ...): refused — an
//     unmeasurable target is never written over silently.
func checkForeignFile(logicalPath string, fresh []byte) (*permDenied, error) {
	raw, err := os.ReadFile(resolve(logicalPath))
	if err != nil {
		switch classifyReadError(err) {
		case readAbsent:
			return nil, nil // absent: clean install
		case readPermDenied:
			return &permDenied{logicalPath: logicalPath}, nil // hardened dir: plan-warn
		default:
			return nil, &InstallError{Path: logicalPath, Reason: fmt.Sprintf("unreadable: %v", err)}
		}
	}
	if string(raw) == string(fresh) {
		return nil, nil // idempotent re-install
	}
	if isPreviousMischiefRender(raw) {
		return nil, nil // drifted mischief artefact: refresh it
	}
	return nil, &InstallError{
		Path:   logicalPath,
		Reason: "a file mischief did not generate already exists at this path — install refuses to clobber it (remove it explicitly, or take it over by making it byte-identical to a fresh render)",
	}
}

// isPreviousMischiefRender reports whether raw carries the generated
// provenance header (a previous `mischief install` wrote it).
func isPreviousMischiefRender(raw []byte) bool {
	return strings.Contains(string(raw), "GENERATED by `mischief install` (SPEC-11)") ||
		strings.Contains(string(raw), `"helper": "mischief-privileged-helper"`)
}

// InstallError is a named install refusal.
type InstallError struct {
	Path   string
	Reason string
}

func (e *InstallError) Error() string {
	return fmt.Sprintf("install: %s: %s", e.Path, e.Reason)
}

// Execute applies the plan (unless DryRun) and returns the verification
// snapshot: for every write, the sha256 of what is NOW on disk. A failed
// action aborts the remaining list (order matters) and names the path.
func (p *Plan) Execute() (map[string]string, error) {
	sums := map[string]string{}
	if p.DryRun {
		return sums, nil
	}
	for _, a := range p.Actions {
		switch a.Kind {
		case OpWriteDir:
			if err := os.MkdirAll(resolve(a.Path), 0o755); err != nil {
				return sums, fmt.Errorf("install: mkdir %s: %w", a.Path, err)
			}
		case OpWriteFile:
			m, err := parseMode(a.Mode)
			if err != nil {
				return sums, err
			}
			real := resolve(a.Path)
			// The parent must exist for production too — on a real host,
			// /etc/sudoers.d exists; on a bare root it does not. Install
			// creates the parent (0755) so a plan carries no implicit
			// ordering constraint on directories it did not name.
			if dir := filepath.Dir(real); dir != "." && dir != "/" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return sums, fmt.Errorf("install: mkdir %s: %w", dir, err)
				}
			}
			// An existing file with no write bit (a 0440 drop-in from a
			// previous install) cannot be truncated by os.WriteFile —
			// os.Remove then write, atomically-shaped: the window is one
			// unlink+create, identical to what visudo-driven editors do.
			if _, statErr := os.Lstat(real); statErr == nil {
				if err := os.Remove(real); err != nil {
					return sums, fmt.Errorf("install: replace %s: %w", a.Path, err)
				}
			} else if !os.IsNotExist(statErr) {
				return sums, fmt.Errorf("install: stat %s: %w", a.Path, statErr)
			}
			if err := os.WriteFile(real, a.Bytes, m); err != nil {
				return sums, fmt.Errorf("install: write %s: %w", a.Path, err)
			}
			sums[a.Path] = sha256hex(a.Bytes)
		case OpRemove, OpRemoveDir:
			return sums, fmt.Errorf("install: plan carries a removal action — execute via uninstall/retention, not install")
		default:
			return sums, fmt.Errorf("install: unknown action kind %q", a.Kind)
		}
	}
	return sums, nil
}

// PlanUninstall renders the uninstall plan: EVERY artefact install may
// have placed (drop-in, manifest, prefix dir), each guarded by existence,
// plus journal run-dir rotation applied per the retention policy (the
// brief: "uninstall removes EVERY artefact including the drop-in and
// journals per retention policy").
//
// Policy: run dirs older/newer than the retention window are REMOVED
// recursively (OpRemoveTree — a real run dir holds journal files, so the
// plain file removal died ENOTEMPTY here while `retention` applied the
// identical policy successfully; MISCHIEF-DF-33); within-window dirs are
// KEPT (the evidence stays auditable for the window). RetainAll=true
// (retention policy "keep") skips journal removal entirely.
func PlanUninstall(opts InstallOpts, runsDir string, pol RetentionPolicy) (*Plan, error) {
	opts.defaults()
	p := &Plan{Op: "uninstall", DryRun: opts.DryRun}
	if !opts.SkipDropIn {
		p.Actions = append(p.Actions, Action{
			Kind: OpRemove, Path: opts.dropInPath(),
			Reason: "sudoers drop-in (the entire privileged surface)",
		})
	}
	p.Actions = append(p.Actions, Action{
		Kind: OpRemove, Path: opts.manifestPath(),
		Reason: "privileged helper manifest",
	})
	// Journals per retention policy: remove only the dirs the policy
	// expires (never blanket-delete the evidence). The dirs arrive as
	// OpRemoveTree from PlanRetention — the same plan shape ExecuteRetention
	// applies, so both verbs rotate identically.
	if runsDir != "" && pol.ApplyOnUninstall {
		exp, err := PlanRetention(runsDir, pol)
		if err != nil {
			return nil, err
		}
		for _, a := range exp.Actions {
			a.Reason += " (uninstall: expired per retention policy)"
			p.Actions = append(p.Actions, a)
		}
	}
	// The prefix dir itself goes last, only when empty (rmdir fails
	// closed on residue — uninstall refuses to eat unknown files).
	p.Actions = append(p.Actions, Action{
		Kind: OpRemoveDir, Path: opts.Prefix,
		Reason: "state prefix (rmdir: refuses on residue)",
	})
	return p, nil
}

// ExecuteUninstall applies an uninstall plan. Removals are
// existence-guarded HERE (not only in the plan): uninstall is idempotent —
// an absent artefact is success, a failed removal is an error naming the
// path. File-shaped actions remove one file; OpRemoveTree removes a run
// dir recursively (a populated run dir is exactly what plain rmdir-style
// removal cannot eat); OpRemoveDir on the prefix refuses non-empty
// (residue is operator data).
func (p *Plan) ExecuteUninstall() ([]string, error) {
	var removed []string
	if p.DryRun {
		return removed, nil
	}
	for _, a := range p.Actions {
		real := resolve(a.Path)
		switch a.Kind {
		case OpRemove:
			if _, err := os.Lstat(real); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return removed, fmt.Errorf("uninstall: stat %s: %w", a.Path, err)
			}
			if err := os.Remove(real); err != nil {
				return removed, fmt.Errorf("uninstall: remove %s: %w", a.Path, err)
			}
			removed = append(removed, a.Path)
		case OpRemoveTree:
			// Confinement guard (mirrors ExecuteRetention): a tree
			// removal must be a DIRECT child of the runs dir the plan
			// was rendered against, never an arbitrary path.
			cleanRoot := filepath.Clean(resolve(a.runsDir))
			target := filepath.Clean(real)
			if filepath.Dir(target) != cleanRoot || target == cleanRoot {
				return removed, fmt.Errorf("uninstall: refusing %s (not a direct child of %s)", a.Path, a.runsDir)
			}
			if _, err := os.Lstat(target); err != nil {
				if os.IsNotExist(err) {
					continue // idempotent
				}
				return removed, fmt.Errorf("uninstall: stat %s: %w", a.Path, err)
			}
			if err := os.RemoveAll(target); err != nil {
				return removed, fmt.Errorf("uninstall: remove-tree %s: %w", a.Path, err)
			}
			removed = append(removed, a.Path)
		case OpRemoveDir:
			if err := os.Remove(real); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				// e.g. ENOTEMPTY: residue refuses the rmdir (fail closed).
				return removed, fmt.Errorf("uninstall: rmdir %s: %w (residue refuses uninstall — inspect the dir)", a.Path, err)
			}
			removed = append(removed, a.Path)
		default:
			return removed, fmt.Errorf("uninstall: unexpected action kind %q", a.Kind)
		}
	}
	return removed, nil
}

// SetuidFinding is one setuid/setgid binary under the install prefix.
type SetuidFinding struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

// SurveySetuid walks the install prefix for setuid/setgid binaries. The
// design ships NO setuid surface; a finding is reported (never deleted) —
// it is the operator's red flag. A missing prefix is an empty survey, not
// an error.
func SurveySetuid(prefix string) ([]SetuidFinding, error) {
	real := resolve(prefix)
	if _, err := os.Stat(real); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, &AuditError{Reason: fmt.Sprintf("prefix %s unreadable: %v", prefix, err)}
	}
	var out []SetuidFinding
	err := filepath.WalkDir(real, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // unreadable entry: skip, the walk continues
		}
		m := info.Mode()
		if m&os.ModeSetuid != 0 || m&os.ModeSetgid != 0 {
			out = append(out, SetuidFinding{Path: path, Mode: fmt.Sprintf("%04o", m.Perm())})
		}
		return nil
	})
	if err != nil {
		return nil, &AuditError{Reason: fmt.Sprintf("prefix walk %s: %v", prefix, err)}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// parseMode parses "0440"-style mode strings.
func parseMode(s string) (os.FileMode, error) {
	var m uint32
	if _, err := fmt.Sscanf(s, "%o", &m); err != nil {
		return 0, fmt.Errorf("install: bad mode %q: %w", s, err)
	}
	return os.FileMode(m), nil
}
