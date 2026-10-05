package ops

import (
	"fmt"
	"strings"
)

// This file owns the DOCTOR posture bridge (SPEC-11 → AC-11/AC-6): per
// privileged primitive, AVAILABLE or ABSENT plus THE MISSING PIECE (not a
// bare false): the sudoers drop-in not installed, the helper manifest
// missing, the granted command shape absent from the installed drop-in, a
// drifted drop-in, or a setuid binary in the prefix.
//
// The bridge maps capability KINDS to the table verbs that serve them, so
// doctor's per-primitive rows can name the exact missing artefact instead
// of "unavailable".

// CapabilityPosture is one capability kind's privileged posture.
type CapabilityPosture struct {
	// Kind is the capability token ("NET_ADMIN", "docker", ...).
	Kind string
	// Available: every table verb the kind needs is granted in an
	// UNDRIFTED installed drop-in.
	Available bool
	// Missing names THE missing piece, one line ("" when available).
	Missing string
	// ServingVerbs lists the table verb ids that serve this kind.
	ServingVerbs []string
	// Findings carries audit observations that colour the verdict
	// (drift, absent artefacts, setuid flags).
	Findings []string
}

// capabilityVerbKinds maps a capability kind to the verb table ids that
// must be granted for a primitive requiring it to be runnable through the
// privileged surface. Kinds absent from the map have no privileged-surface
// dependency ("cgroup2 + user scope" is rootless-delegated by the table;
// "docker API" rides the docker socket, not sudo).
func capabilityVerbKinds(kind string) []string {
	switch {
	case kind == "NET_ADMIN":
		return []string{"netns_add", "netns_del", "tc_netem"}
	case strings.HasPrefix(kind, "cgroup2"):
		return nil // rootless delegation: no sudo grant required (table row ExecRootlessDelegated)
	default:
		return nil
	}
}

// Posture computes per-capability privileged posture from an audit. The
// caller (doctor) runs RunAudit once and feeds it here per kind.
func Posture(a *Audit, kind string) CapabilityPosture {
	cp := CapabilityPosture{Kind: kind}
	need := capabilityVerbKinds(kind)
	if len(need) == 0 {
		// No privileged-surface dependency: posture follows the audit's
		// structural findings only (never blocks on sudo).
		cp.Available = true
		if a != nil {
			cp.Findings = a.Notes
		}
		return cp
	}
	cp.ServingVerbs = need
	if a == nil {
		cp.Available = false
		cp.Missing = "privileged surface not measured (no audit)"
		return cp
	}
	// The verdict pieces, each naming THE missing artefact:
	if !a.DropInInstalled {
		cp.Available = false
		cp.Missing = fmt.Sprintf("sudoers drop-in not installed (expected %s) — run `mischief install`", a.DropInPath)
		return cp
	}
	if !a.DropInMatches {
		cp.Available = false
		cp.Missing = fmt.Sprintf("sudoers drop-in drifted from the canonical verb table (hand-edited or stale) — run `mischief install` to regenerate; diff follows in the audit")
		cp.Findings = append(cp.Findings, "drop-in drift: "+firstLine(a.DropInDiff))
		return cp
	}
	// Drift-free drop-in: are THIS kind's verbs granted?
	var absent []string
	for _, id := range need {
		found := false
		for _, av := range a.Verbs {
			if av.ID == id && av.GrantedInDropIn {
				found = true
				break
			}
		}
		if v, ok := Lookup(id); ok && v.Exec == ExecRootlessDelegated {
			continue // never granted via sudo by design
		}
		if !found {
			absent = append(absent, id)
		}
	}
	if len(absent) > 0 {
		cp.Available = false
		cp.Missing = fmt.Sprintf("sudoers drop-in installed but missing grant(s) for: %s (regenerate with `mischief install`)", strings.Join(absent, ", "))
		return cp
	}
	if !a.HelperManifestInstalled {
		// Grants are live but the manifest is missing: the helper
		// milestone's contract is incomplete — report, do not fail the
		// sudo-side posture.
		cp.Findings = append(cp.Findings, "helper manifest missing at "+a.HelperManifestPath)
	}
	cp.Available = true
	cp.Findings = append(cp.Findings, a.Notes...)
	return cp
}

// firstLine is the audit diff's one-line preview.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// RenderPosture renders the posture as one doctor-grade line.
func (cp CapabilityPosture) RenderPosture() string {
	state := "available"
	if !cp.Available {
		state = "absent"
	}
	line := fmt.Sprintf("privileged posture %s: %s", cp.Kind, state)
	if cp.Missing != "" {
		line += " — " + cp.Missing
	}
	return line
}
