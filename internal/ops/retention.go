package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file owns retention/rotation (SPEC-11): journal run dirs rotated by
// age and count — default "keep the last 20 run dirs OR 30 days, whichever
// expires first". The policy is pure (unit-tested selection), the plan is
// renderable, and the apply is existence-guarded.

// RetentionPolicy is the rotation policy. Zero value is invalid (both
// bounds zero would expire everything — refused).
type RetentionPolicy struct {
	// KeepCount: keep at most the N newest run dirs (0 = unlimited).
	KeepCount int
	// KeepAge: expire run dirs older than this (0 = unlimited).
	KeepAge time.Duration
	// Now is the clock seam (tests pin it); zero = time.Now.
	Now time.Time
	// ApplyOnUninstall: uninstall also expires per this policy (the
	// brief's "journals per retention policy"). Install ignores it.
	ApplyOnUninstall bool
	// ApplyOnInstall: install also rotates (the brief: "applied on
	// install").
	ApplyOnInstall bool
}

// DefaultRetentionPolicy is the shipped default: keep the last 20 run dirs
// or 30 days, whichever first.
func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{KeepCount: 20, KeepAge: 30 * 24 * time.Hour, ApplyOnInstall: true, ApplyOnUninstall: true}
}

// RunDir is one journal run directory as retention sees it.
type RunDir struct {
	// Name is the basename (sorted by).
	Name string
	// ModTime is the dir's mtime (rotation proxy: a run dir's last write
	// is its last journal activity).
	ModTime time.Time
}

// SelectExpired is the PURE policy selection: given the run dirs (any
// order) and the clock, return the ones the policy expires.
//
// Whichever-first semantics: a dir is expired when (older than KeepAge) OR
// (beyond KeepCount newest). The count bound keeps the N NEWEST dirs even
// if they are old (a stale-but-recently-touched set stays until the count
// pushes it out); the age bound expires old dirs even when few exist.
func (p RetentionPolicy) SelectExpired(dirs []RunDir) []RunDir {
	if p.KeepCount <= 0 && p.KeepAge <= 0 {
		return nil // invalid policy configured to "keep everything": expire nothing
	}
	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	sorted := make([]RunDir, len(dirs))
	copy(sorted, dirs)
	// Newest first; ties broken by name for determinism.
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].ModTime.Equal(sorted[j].ModTime) {
			return sorted[i].ModTime.After(sorted[j].ModTime)
		}
		return sorted[i].Name > sorted[j].Name
	})
	var expired []RunDir
	for i, d := range sorted {
		expire := false
		if p.KeepAge > 0 && now.Sub(d.ModTime) > p.KeepAge {
			expire = true // older than the age window
		}
		if p.KeepCount > 0 && i >= p.KeepCount {
			expire = true // beyond the count window
		}
		if expire {
			expired = append(expired, d)
		}
	}
	return expired
}

// Validate refuses a policy that would expire everything (both bounds
// zero) — "keep nothing ever" is not a retention policy, it is rm -rf with
// extra steps.
func (p RetentionPolicy) Validate() error {
	if p.KeepCount <= 0 && p.KeepAge <= 0 {
		return fmt.Errorf("retention: invalid policy: both keep-count and keep-age are unset (would expire every run dir)")
	}
	if p.KeepCount < 0 || p.KeepAge < 0 {
		return fmt.Errorf("retention: invalid policy: negative bounds")
	}
	return nil
}

// ListRunDirs surveys a runs dir (one entry per SUBDIRECTORY — journal
// homes are dirs; files are ignored) newest-first.
func ListRunDirs(runsDir string) ([]RunDir, error) {
	ents, err := os.ReadDir(resolve(runsDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no runs dir yet: nothing to rotate
		}
		return nil, fmt.Errorf("retention: runs dir %s: %w", runsDir, err)
	}
	var out []RunDir
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // racy entry: skip, do not fail the survey
		}
		out = append(out, RunDir{Name: e.Name(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Name > out[j].Name
	})
	return out, nil
}

// PlanRetention renders the rotation plan for a runs dir under the
// policy. The runs dir itself is never removed.
func PlanRetention(runsDir string, pol RetentionPolicy) (*Plan, error) {
	if err := pol.Validate(); err != nil {
		return nil, err
	}
	dirs, err := ListRunDirs(runsDir)
	if err != nil {
		return nil, err
	}
	expired := pol.SelectExpired(dirs)
	p := &Plan{Op: "retention", DryRun: false}
	for _, d := range expired {
		// A run dir is removed recursively (it holds the journal files);
		// the plan carries the DIR as one action and execution walks it.
		p.Actions = append(p.Actions, Action{
			Kind: OpRemove, Path: filepath.Join(runsDir, d.Name),
			Reason: fmt.Sprintf("run dir expired (mtime %s beyond policy keep %d dirs / %s)", d.ModTime.UTC().Format(time.RFC3339), pol.KeepCount, pol.KeepAge),
		})
	}
	return p, nil
}

// ExecuteRetention applies a retention plan (recursive removal of expired
// run dirs only — the action path must sit INSIDE the runs dir it names,
// a guard against a malformed plan eating arbitrary paths).
func ExecuteRetention(runsDir string, p *Plan) ([]string, error) {
	var removed []string
	if p == nil || p.DryRun {
		return removed, nil
	}
	cleanRoot := filepath.Clean(resolve(runsDir))
	for _, a := range p.Actions {
		if a.Kind != OpRemove {
			return removed, fmt.Errorf("retention: unexpected action kind %q", a.Kind)
		}
		real := filepath.Clean(resolve(a.Path))
		// Confinement guard: the target must be a DIRECT child of the
		// runs dir (one path element below it).
		if filepath.Dir(real) != cleanRoot || real == cleanRoot {
			return removed, fmt.Errorf("retention: refusing %s (not a direct child of %s)", a.Path, runsDir)
		}
		if _, err := os.Lstat(real); err != nil {
			if os.IsNotExist(err) {
				continue // idempotent
			}
			return removed, fmt.Errorf("retention: stat %s: %w", a.Path, err)
		}
		if err := os.RemoveAll(real); err != nil {
			return removed, fmt.Errorf("retention: remove %s: %w", a.Path, err)
		}
		removed = append(removed, a.Path)
	}
	return removed, nil
}

// ParseRetentionFlags builds a policy from --keep-count/--keep-days CLI
// values ("" = unset). Both unset → the shipped default. Explicit 0 on
// either flag = "unlimited on this bound" ( Validate allows one bound).
func ParseRetentionFlags(keepCount, keepDays string) (RetentionPolicy, error) {
	if keepCount == "" && keepDays == "" {
		return DefaultRetentionPolicy(), nil
	}
	pol := RetentionPolicy{ApplyOnInstall: true, ApplyOnUninstall: true}
	if keepCount != "" {
		n, err := strconv.Atoi(keepCount)
		if err != nil || n < 0 {
			return pol, fmt.Errorf("retention: --keep-count %q: want a non-negative integer", keepCount)
		}
		pol.KeepCount = n
	}
	if keepDays != "" {
		d, err := strconv.ParseFloat(keepDays, 64)
		if err != nil || d < 0 {
			return pol, fmt.Errorf("retention: --keep-days %q: want a non-negative number", keepDays)
		}
		pol.KeepAge = time.Duration(d * 24 * float64(time.Hour))
	}
	if err := pol.Validate(); err != nil {
		return pol, err
	}
	return pol, nil
}

// stringsComma renders a string slice as ", "-joined (audit notes helper).
func stringsComma(s []string) string { return strings.Join(s, ", ") }
