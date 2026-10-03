// Package backend_net is SPEC-08: the network fault backends — the rootless
// userspace HTTP proxy with fault injection, and the netns/netem primitive
// behind the privileged helper (sudo allowlist).
//
// Design authority: docs/prd/mischief-v0.1.md (SPEC-08, AC-3, AC-8, AC-17)
// and probe/RESULTS.md — the live host audit that measured the split this
// package implements:
//
//   - netns + netem PROVEN only with sudo (qdisc showed `loss 100%`, ping
//     rc=2, netns del verified). Unprivileged user+net namespaces CANNOT
//     create a veth on this host (EPERM — Ubuntu AppArmor userns
//     restriction), so a rootless netns path does not exist here.
//   - Rootless path = a userspace proxy: HTTP 429 injection, response
//     truncation, stall/delay — all measured via proxy hit counters.
//
// AC-3 (anti-gaming) shapes both paths: never assert landed, MEASURE it.
// The proxy path proves landing with hit counters; the netns path reports
// the qdisc counters read back from the helper. When the privileged helper
// is absent the netns primitive refuses with a capability_unavailable
// error that NAMES the missing helper — it never skips silently and never
// claims a fault it did not land.
//
// AC-17: primitives targeting a cloud provider refuse by name when no
// cloud plane is armed (rails' L5 gate vocabulary).
//
// AC-8: the load gate is internal/rails' — this package CALLS
// LoadGate.Check and passes its refusal through unchanged rather than
// reimplementing the gate or the taxonomy.
//
// ch:trace row=MSF-009 spec=docs/prd/mischief-v0.1.md#SPEC-08 evidence=internal/backend_net/ witness=none:no-live-host-run-in-worktree
package backend_net

import (
	"fmt"
	"strings"
)

// CapabilityError is the refusal a primitive returns when the capability it
// needs is not available on this host (no sudo, no privileged helper). The
// text names the missing piece — a refusal that cannot say what is missing
// is how a silent skip disguises itself as a pass.
type CapabilityError struct {
	// Primitive is the primitive id that needs the capability.
	Primitive string
	// Missing names the missing capability/helper (e.g. "sudo", or the
	// helper path).
	Missing string
	// Detail is the human explanation with the measured basis (the probe
	// result that established the requirement).
	Detail string
}

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("backend_net: capability_unavailable: %s: missing %s: %s", e.Primitive, e.Missing, e.Detail)
}

// CloudPlaneError is the AC-17 refusal: the primitive targets a cloud
// provider but no cloud plane is armed. It names the provider.
type CloudPlaneError struct {
	// Primitive is the primitive id.
	Primitive string
	// Provider is the cloud provider named by the target.
	Provider string
	// Detail is the human explanation.
	Detail string
}

func (e *CloudPlaneError) Error() string {
	return fmt.Sprintf("backend_net: cloud plane not armed: %s targets provider %q: %s", e.Primitive, e.Provider, e.Detail)
}

// sanitizeNetemParams rejects netem parameters that could escape the
// allowlisted command shape (the helper builds a fixed argv; parameters
// must be plain numbers).
func sanitizeNetemParams(lossPct int, delayMS int) error {
	if lossPct < 0 || lossPct > 100 {
		return fmt.Errorf("backend_net: netem loss %% must be 0..100, got %d", lossPct)
	}
	if delayMS < 0 || delayMS > 60000 {
		return fmt.Errorf("backend_net: netem delay ms must be 0..60000, got %d", delayMS)
	}
	return nil
}

// qdiscCounters is the landed-proof read-back for the privileged path: the
// qdisc show output parsed into the counters AC-3 measures.
type qdiscCounters struct {
	LossPct int
	DelayMS int
	RawLine string
}

// parseQdiscShow pulls `loss N%` and `delay Nms` from a `tc qdisc show`
// line (the probe measured: "qdisc netem 8001: root refcnt 2 limit 1000
// loss 100% seed ..."). Empty output parses to zero counters — the caller
// treats that as not-landed, never as landed.
func parseQdiscShow(out string) qdiscCounters {
	c := qdiscCounters{RawLine: strings.TrimSpace(out)}
	for _, f := range strings.Fields(out) {
		if v, ok := strings.CutSuffix(f, "%"); ok && len(v) > 0 && isDigits(v) {
			if c.LossPct == 0 {
				fmt.Sscanf(v, "%d", &c.LossPct)
			}
		}
		if v, ok := strings.CutSuffix(f, "ms"); ok && len(v) > 0 && isDigits(v) {
			if c.DelayMS == 0 {
				fmt.Sscanf(v, "%d", &c.DelayMS)
			}
		}
	}
	return c
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
