package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This file owns the privilege audit render (SPEC-11: "dumps the effective
// privileged surface — drop-in contents as installed vs expected, any
// setuid binaries in the install prefix, helper path/permissions — as JSON
// for an operator to diff").

// sha256hex is the content hash used for install verification snapshots.
func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// AuditOpts carries the audit's inputs.
type AuditOpts struct {
	// SudoersUser must match the user install granted (render comparison).
	SudoersUser string
	// Prefix is the install prefix to survey (default /var/lib/mischief).
	Prefix string
}

// RunAudit surveys the LIVE privileged surface against the canonical verb
// table. Every measurement failure is a NAMED finding inside the audit —
// the audit's job is to report, never to refuse the operator their view.
func RunAudit(opts AuditOpts) (*Audit, error) {
	if opts.SudoersUser == "" {
		opts.SudoersUser = defaultSudoersUser
	}
	if opts.Prefix == "" {
		opts.Prefix = DefaultPrefix
	}
	expected := RenderDropIn(opts.SudoersUser)
	manifest := RenderHelperManifest()

	a := &Audit{
		DropInPath:         DefaultDropInPath,
		HelperManifestPath: filepath.Join(opts.Prefix, "helper-manifest.json"),
		// SetuidBinaries/Verbs start as EMPTY slices (not nil) so the
		// audit's JSON renders [] — "measured, found none" — never null
		// ("not measured"). The null-vs-empty distinction is data here.
		SetuidBinaries: []string{},
		Verbs:          []AuditVerb{},
		Notes:          nil,
	}
	_ = manifest

	// 1. helper manifest: measured FIRST so the drop-in grade below can
	// classify a manifest-only install (MISCHIEF-DF-35) instead of calling
	// it not_installed. (The manifest is advisory context for every other
	// grade — the drop-in is still the graded artefact.)
	mraw, err := os.ReadFile(resolve(a.HelperManifestPath))
	switch {
	case err == nil:
		a.HelperManifestInstalled = true
		a.HelperManifestMatches = string(mraw) == string(manifest)
	case os.IsNotExist(err):
		a.HelperManifestInstalled = false
	default:
		a.HelperManifestInstalled = false
		a.Notes = append(a.Notes, fmt.Sprintf("helper manifest unreadable: %v", err))
	}

	// 2. drop-in: installed? matches? — and the MISCHIEF-DF-16 install
	// condition: the drop-in is the graded artefact, so its state is
	// classified right here (the helper manifest is advisory context).
	// Absent = healthy pre-install, NEVER drift; drift and unreadable
	// are the present-but-divergent grades.
	raw, err := os.ReadFile(resolve(a.DropInPath))
	switch {
	case err == nil:
		a.DropInInstalled = true
		a.DropInMatches = string(raw) == string(expected)
		if a.DropInMatches {
			a.Condition = ConditionOK
		} else {
			a.Condition = ConditionDrift
			a.DropInDiff = diffLines(string(expected), string(raw))
		}
	case os.IsNotExist(err):
		a.DropInInstalled = false
		if a.HelperManifestInstalled {
			// MISCHIEF-DF-35: the manifest-only install (the ONE
			// install mode an unprivileged operator can run —
			// `mischief install --no-drop-in`) is a real, landed
			// state, not a pre-install one. Grading it as
			// not_installed erases it: the operator cannot tell
			// "nothing here yet" from "manifest landed, drop-in
			// absent by design". Grade it its own condition.
			a.Condition = ConditionManifestOnly
			a.DropInDiff = "drop-in not installed (manifest-only install at " + a.HelperManifestPath + "; the drop-in write needs root)"
		} else {
			a.Condition = ConditionUninstalled
			a.DropInDiff = "drop-in not installed (expected at " + a.DropInPath + ")"
		}
	default:
		a.DropInInstalled = false
		a.Condition = ConditionUnreadable
		a.DropInDiff = fmt.Sprintf("drop-in unreadable: %v", err)
	}

	// 3. setuid sweep over the prefix
	findings, err := SurveySetuid(opts.Prefix)
	if err != nil {
		a.Notes = append(a.Notes, "setuid sweep failed: "+err.Error())
	} else {
		for _, f := range findings {
			a.SetuidBinaries = append(a.SetuidBinaries, f.Path+" (mode "+f.Mode+")")
		}
	}

	// 4. per-verb grant posture measured against the INSTALLED drop-in
	var granted []string
	if a.DropInInstalled {
		granted = ParseDropInVerbs(string(raw))
	}
	for _, v := range SortedVerbs() {
		av := AuditVerb{
			ID:     v.ID,
			Exec:   string(v.Exec),
			Binary: v.Binary,
			Reason: v.Why,
		}
		if v.Exec == ExecSudo {
			line := v.Binary
			if !v.wildcard() {
				var parts []string
				parts = append(parts, v.Binary)
				parts = append(parts, v.Args...)
				line = strings.Join(parts, " ")
			}
			for _, g := range granted {
				if g == line {
					av.GrantedInDropIn = true
					av.WildcardGrant = v.wildcard()
				}
			}
		} else {
			av.Reason = "rootless delegation (no sudo grant by design): " + v.Why
		}
		a.Verbs = append(a.Verbs, av)
	}

	// 5. posture notes
	if a.DropInInstalled {
		a.Notes = append(a.Notes, fmt.Sprintf("drop-in mode %s (sudoers requires 0440)", fileModeString(a.DropInPath)))
	}
	if !a.HelperManifestInstalled {
		note := "helper manifest absent at " + a.HelperManifestPath
		if a.Condition == ConditionUninstalled {
			// MISCHIEF-DF-16: nothing is installed — a healthy
			// pre-install state. State the fact; a remediation
			// aimed at drift ("run mischief install") would
			// misreport healthy absence as something to fix.
			note += " (nothing installed — healthy pre-install state)"
		} else {
			note += " (run `mischief install` to complete the install)"
		}
		a.Notes = append(a.Notes, note)
	}
	return a, nil
}

// Drop-in install-condition tokens (MISCHIEF-DF-16). Exported: the cmd
// layer's exit grade keys off them. The audit's grade: NotInstalled is a
// healthy state (exit 0); Drift and Unreadable are present-but-divergent
// findings (exit 1); OK is the fully-installed clean state (exit 0).
// The JSON renders the lowercase token values.
const (
	ConditionUninstalled = "not_installed"
	ConditionOK          = "ok"
	ConditionDrift       = "drift"
	ConditionUnreadable  = "unreadable"
	// MISCHIEF-DF-35: the manifest landed (a real `mischief install
	// --no-drop-in` — the ONE install mode an unprivileged operator can
	// run) but the drop-in is absent (its write needs root). A distinct,
	// still-healthy state: not a pre-install host, not drift.
	ConditionManifestOnly = "manifest_only"
)

// RenderAuditJSON renders the audit as indented JSON for an operator diff.
func RenderAuditJSON(a *Audit) []byte {
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		// The audit struct is JSON-safe by construction; this is a
		// compile-time-invariant, but never fabricate output on failure.
		return []byte(fmt.Sprintf("{\"error\": %q}\n", err.Error()))
	}
	return append(b, '\n')
}

// diffLines renders an operator-readable line diff (expected vs found).
func diffLines(expected, found string) string {
	el := strings.Split(expected, "\n")
	fl := strings.Split(found, "\n")
	es := make(map[string]bool, len(el))
	for _, l := range el {
		es[strings.TrimSpace(l)] = true
	}
	fs := make(map[string]bool, len(fl))
	for _, l := range fl {
		fs[strings.TrimSpace(l)] = true
	}
	var b strings.Builder
	b.WriteString("--- expected (rendered from verb table)\n+++ found (installed)\n")
	for _, l := range el {
		if l == "" {
			continue
		}
		if !fs[strings.TrimSpace(l)] {
			fmt.Fprintf(&b, "-%s\n", l)
		}
	}
	for _, l := range fl {
		if l == "" {
			continue
		}
		if !es[strings.TrimSpace(l)] {
			fmt.Fprintf(&b, "+%s\n", l)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// fileModeString reads a file's permission bits as "0440" ("" unreadable).
func fileModeString(logical string) string {
	fi, err := os.Stat(resolve(logical))
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%04o", fi.Mode().Perm())
}
