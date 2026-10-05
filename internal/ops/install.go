package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// This file owns install/uninstall (SPEC-11): plan construction from the
// canonical verb table, real execution, and the audit survey. Every path
// resolves through Root: "" is the production filesystem; a test override
// confines every action to a temp dir.

// Root is the filesystem root all logical paths resolve against. Empty =
// "/" (production). Package-level var so tests can pin a temp root; never
// mutated outside tests.
var Root = ""

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
	// mischief service account).
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

// dropInPath returns the drop-in path (fixed: /etc/sudoers.d/mischief —
// the prefix moves mischief's own state, never the sudoers drop-in).
func (o *InstallOpts) dropInPath() string { return DefaultDropInPath }

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
	body := RenderDropIn(opts.SudoersUser)
	manifest := RenderHelperManifest()

	if err := checkForeignFile(opts.dropInPath(), body); err != nil {
		return nil, err
	}
	if err := checkForeignFile(opts.manifestPath(), manifest); err != nil {
		return nil, err
	}

	p := &Plan{Op: "install", DryRun: opts.DryRun}
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

// checkForeignFile refuses an install over an existing file that is
// neither byte-identical nor a previous mischief render (drift). The
// error names the path and both recovery routes.
func checkForeignFile(logicalPath string, fresh []byte) error {
	raw, err := os.ReadFile(resolve(logicalPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil // absent: clean install
		}
		return &InstallError{Path: logicalPath, Reason: fmt.Sprintf("unreadable: %v", err)}
	}
	if string(raw) == string(fresh) {
		return nil // idempotent re-install
	}
	if isPreviousMischiefRender(raw) {
		return nil // drifted mischief artefact: refresh it
	}
	return &InstallError{
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
// Policy: run dirs older/newer than the retention window are REMOVED;
// within-window dirs are KEPT (the evidence stays auditable for the
// window). RetainAll=true (retention policy "keep") skips journal removal
// entirely.
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
	// expires (never blanket-delete the evidence).
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
// path. rmdir on the prefix refuses non-empty (residue is operator data).
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
