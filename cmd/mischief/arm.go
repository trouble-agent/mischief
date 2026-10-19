package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

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
func cmdArm(args []string) int {
	fs := flag.NewFlagSet("arm", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", defaultRunDir(), "run dir (journal home)")
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

	inv := reverter.Decl{Kind: *invKind}
	var err error
	if inv.Params, err = parseDeclParams(*invParams); err != nil {
		fmt.Fprintf(os.Stderr, "arm: --inverse-params: %v\n", err)
		return 2
	}
	chk := reverter.Decl{Kind: *chkKind}
	if chk.Params, err = parseDeclParams(*chkParams); err != nil {
		fmt.Fprintf(os.Stderr, "arm: --check-params: %v\n", err)
		return 2
	}

	// SPEC-13 sanction gate (same shape as plan/selftest/battery): fail
	// closed, exit 2, NOTHING written to the run dir on refusal.
	if err := sanction.Check(sanction.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	hold := reverter.Hold{
		FaultID: *fault,
		Target:  *target,
		Inverse: inv,
		Check:   chk,
		TTL:     *ttl,
	}
	store, err := reverter.Open(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "arm:", err)
		return 1
	}
	defer store.Close()
	id, err := reverter.New(store).Arm(hold)
	if err != nil {
		fmt.Fprintln(os.Stderr, "arm:", err)
		return 1
	}
	fmt.Printf("armed %s fault=%s target=%s ttl=%s dir=%s\n", id, *fault, *target, *ttl, *dir)
	fmt.Printf("write-ahead: inverse + check durable in the journal (SPEC-04); serve reverts at TTL\n")
	return 0
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
