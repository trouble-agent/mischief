package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/trouble-agent/mischief/internal/rails"
	"github.com/trouble-agent/mischief/internal/reverter"
	"github.com/trouble-agent/mischief/internal/sanction"
)

// ── arm ──────────────────────────────────────────────────────────────────────

// cmdArm is the CLI's write-ahead surface (DF-05): it arms a reverter hold —
// fault id, target, inverse, measured check, TTL — as a durable arm record in
// the run dir's journal (SPEC-04: the inverse is on disk BEFORE the fault may
// land). It writes NOTHING else: landing the fault is the caller's act (selftest,
// battery, or a future run verb); the serve verb (or a detached owner) reverts
// the hold at TTL expiry from the journal bytes alone.
//
// The sanction gate runs before the arm (SPEC-13/MSF-032 posture: arm is the
// fault-landing path's first half — a host that may not land a fault may not
// arm one either). Fail-closed, exit 2, nothing written.
//
// Decl kinds are the reverter's closed vocabulary (internal/reverter/exec.go):
// inverse — restore-file (params: path, backup) | remove-file (params: path);
// check — file-matches-backup (params: path, backup) | file-absent (params:
// path). Unknown kinds are refused here at arm time (the same fail-closed
// grade a revert would give them, surfaced a whole phase earlier).
//
// The fault id is checked the same way (MISCHIEF-RV-2): it must resolve in
// the catalog (--catalog-dir, default catalog/faults from cwd upward — the
// same resolution every catalog-consuming verb uses). An unknown id never
// becomes a durable hold.
//
// The declared target is resolved through the rails' own accessor
// (internal/rails Resolve — MISCHIEF-DF-58), so the protected-target list
// (scheduler, gateway, memory-daemon) and the structural exclusions refuse
// here exactly as they do on the plan path, before anything is written. There
// is no override flag on this verb; the override surface is a separate
// change.
func cmdArm(args []string) int {
	fs := flag.NewFlagSet("arm", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", defaultRunDir(), "run dir (journal home)")
	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
	fault := fs.String("fault", "", "fault id to arm a hold for (required)")
	target := fs.String("target", "", "target selector the fault lands on (required)")
	ttl := fs.Duration("ttl", 0, "how long the fault may be held (required, e.g. 30s)")
	invKind := fs.String("inverse-kind", "", "inverse action kind: restore-file | remove-file (required)")
	invParams := fs.String("inverse-params", "", "inverse params, k=v[,k=v...] (e.g. path=/tmp/f,backup=/tmp/f.bak)")
	chkKind := fs.String("check-kind", "", "measured post-revert check kind: file-matches-backup | file-absent (required)")
	chkParams := fs.String("check-params", "", "check params, k=v[,k=v...]")
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}

	// Usage validation BEFORE the sanction gate: a malformed arm is a usage
	// error, not a host-posture refusal — name the missing piece (the
	// catalog precedent: every refusal names the offending field).
	missing := make([]string, 0, 5)
	for name, val := range map[string]string{
		"--fault":          *fault,
		"--target":         *target,
		"--inverse-kind":   *invKind,
		"--check-kind":     *chkKind,
		"--inverse-params": *invParams,
		"--check-params":   *chkParams,
	} {
		if val == "" {
			missing = append(missing, name)
		}
	}
	if *ttl <= 0 {
		missing = append(missing, "--ttl")
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "arm: %s required (no default hold: arming nothing is not an arm)\n", strings.Join(missing, ", "))
		return 2
	}

	// MISCHIEF-RV-2: the fault id must exist in a catalog. An arm naming an
	// id no catalog knows would write a write-ahead hold that can never be
	// landed or reasoned about — the same fail-closed grade the malformation
	// gate gives an unknown inverse-kind: usage refusal, exit 2, naming the
	// offending field, nothing written. It fires before the sanction gate
	// (usage errors precede posture refusals).
	cat, err := loadCatalog(*catalogDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "arm:", err)
		return 1
	}
	if cat.Get(*fault) == nil {
		fmt.Fprintf(os.Stderr, "arm: unknown fault id %q (not in catalog)\n", *fault)
		return 2
	}

	inv := reverter.Decl{Kind: *invKind}
	if inv.Params, err = parseDeclParams(*invParams); err != nil {
		fmt.Fprintf(os.Stderr, "arm: --inverse-params: %v\n", err)
		return 2
	}
	chk := reverter.Decl{Kind: *chkKind}
	if chk.Params, err = parseDeclParams(*chkParams); err != nil {
		fmt.Fprintf(os.Stderr, "arm: --check-params: %v\n", err)
		return 2
	}

	// MISCHIEF-DF-72: validate the declaration while the run dir is still
	// untouched. reverter.Open MINTS the run dir (it MkdirAll's dir and
	// installs boot.json), so a refusal discovered only inside
	// Lifecycle.Arm — an unknown or cross-slot decl kind (the same
	// fail-closed grade a revert would give), or an inverse whose restore
	// backup is unreadable (MISCHIEF-DF-23's probe) — used to leave a
	// freshly minted boot.json + empty reverter.jsonl behind a refusal
	// whose message says nothing was written. The declaration is a pure
	// function of the flags, so a pre-open validation cannot contradict
	// Arm's own check (the same Hold.Validate + inverse-undo probes,
	// applied one phase earlier).
	pending := reverter.Hold{
		FaultID: *fault,
		Target:  *target,
		Inverse: inv,
		Check:   chk,
		TTL:     *ttl,
	}
	if err := reverter.ValidateForArm(pending); err != nil {
		fmt.Fprintln(os.Stderr, "arm:", err)
		return 2
	}

	// MISCHIEF-DF-58: the protected-target rail covers the arm path. Plan
	// refuses a protected selector at resolution (AC-7); arm — the
	// write-ahead half of the same loop — must agree, or a fleet control
	// plane gains a durable hold (status --all publishes it) that plan
	// would have refused in the same session. Resolve through the same
	// rails accessor with the same options plan passes
	// (internal/plan.Build), AFTER every usage-class check (missing flags,
	// unknown fault id, malformed params) and BEFORE the sanction gate and
	// any journal write — the write-ahead contract: a refused arm writes
	// nothing. No operator override here: the override surface is
	// MISCHIEF-DF-57's.
	decl := rails.DeclaredTarget{Kind: declaredTargetKind(*target), Selector: *target}
	if _, err := rails.Resolve(decl, rails.ResolveOptions{SelfPID: 0, SelfExe: ""}); err != nil {
		fmt.Fprintln(os.Stderr, "arm:", err)
		return rails.MapExit(err)
	}

	// SPEC-13 sanction gate (same shape as plan/selftest/battery): fail
	// closed, exit 2, NOTHING written to the run dir on refusal.
	if err := sanction.Check(sanction.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	store, err := reverter.Open(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "arm:", err)
		return 1
	}
	defer store.Close()
	id, err := reverter.New(store).Arm(pending)
	if err != nil {
		fmt.Fprintln(os.Stderr, "arm:", err)
		return 1
	}
	fmt.Printf("armed %s fault=%s target=%s ttl=%s dir=%s\n", id, *fault, *target, *ttl, *dir)
	fmt.Printf("write-ahead: inverse + check durable in the journal (SPEC-04); serve reverts at TTL\n")
	return 0
}

// declaredTargetKind derives the rails TargetKind for a --target selector
// value, with the same prefix rule internal/plan's kindFor applies to an
// experiment's declared selector: the prefix before the first ':' names the
// class when it is one of the five (process | pid→process | cgroup |
// container | host | cloud); anything else is a host selector. Arm refuses
// bare selectors with the usage gate before this is consulted, so the
// default never runs on an empty value.
func declaredTargetKind(selector string) rails.TargetKind {
	switch {
	case strings.HasPrefix(selector, "process:"), strings.HasPrefix(selector, "pid:"):
		return rails.TargetProcess
	case strings.HasPrefix(selector, "cgroup:"):
		return rails.TargetCgroup
	case strings.HasPrefix(selector, "container:"):
		return rails.TargetContainer
	case strings.HasPrefix(selector, "cloud:"):
		return rails.TargetCloud
	default:
		return rails.TargetHost
	}
}

// parseDeclParams parses a "k=v,k=v" flag value into a Decl's param map.
// An empty value is an empty map (the kind may need no params); a malformed
// pair (no '=') or an empty key/value is a usage error naming the pair —
// Decl.Validate would refuse them at arm time anyway, but naming the pair
// here keeps the error at the flag it came from.
func parseDeclParams(s string) (map[string]string, error) {
	out := make(map[string]string)
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	for _, pair := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" || v == "" {
			return nil, fmt.Errorf("malformed pair %q (want k=v, no empty key or value)", pair)
		}
		out[k] = v
	}
	return out, nil
}
