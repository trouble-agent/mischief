// The SPEC-07 primitive registry: the five backends, the deterministic
// tie-break, the primitive Table(), and the selftest driver. It also
// publishes the anti-gaming fallback (AC-3): an unknown primitive name
// resolves to the deliberately no-op stub, whose landing never proves
// itself — Inject returns a no_op Proof and a release that refuses — so a
// caller that grades on Proof.ReadbackOk FAILS the run instead of passing
// a green-but-did-nothing backend.
package backend_resource

import (
	"fmt"
	"strings"
)

// PrimitiveNames returns the five SPEC-07 primitive names in table order.
func PrimitiveNames() []string {
	return []string{
		"systemd-run user scope",
		"cgroup v2 freezer",
		"dm/loop I/O error backend",
		"fsfreeze (FIFREEZE)",
		"read-only remount",
	}
}

// Table returns the SPEC-07 primitive set (name → primitive), order
// deterministic via PrimitiveNames.
func Table() map[string]primitive {
	return map[string]primitive{
		"systemd-run user scope":    systemdScope{},
		"cgroup v2 freezer":         Freezer{},
		"dm/loop I/O error backend": DMLoop{},
		"fsfreeze (FIFREEZE)":       Fsfreeze{},
		"read-only remount":         RemountRO{},
	}
}

// CapabilitiesOf answers the live capability question for every primitive,
// deterministically ordered, refusal text rendered for the unavailable
// ones (the AC-11 refusal: names what is absent, never a bare error).
func CapabilitiesOf(h host) []CapabilityResult {
	out := make([]CapabilityResult, 0, 5)
	for _, name := range PrimitiveNames() {
		p, ok := Table()[name]
		if !ok {
			continue
		}
		var c CapabilityResult
		var err error
		switch name {
		case "systemd-run user scope":
			c, err = p.(systemdScope).probeWith(h)
		case "cgroup v2 freezer":
			c = probeFreezer(h)
		case "dm/loop I/O error backend":
			c = probeDMLoop(h)
		case "fsfreeze (FIFREEZE)":
			c = probeFsfreeze(h)
		default:
			c = probeRemountRO(h)
		}
		if err != nil {
			c = CapabilityResult{Name: name, Missing: "probe error: " + errText(err),
				Basis: "probe failed (fail-closed: unreadable never reads as capable)"}
		}
		out = append(out, c)
	}
	return out
}

// SelftestAll runs every primitive's selftest in table order.
func SelftestAll() []SelftestReport {
	var out []SelftestReport
	for _, name := range PrimitiveNames() {
		p := Table()[name]
		out = append(out, p.Selftest())
	}
	return out
}

// SelftestAllString renders the selftest matrix as one line per primitive
// (the AC-19 publishable shape: primitive → green|skipped|failing + why).
func SelftestAllString() string {
	var b strings.Builder
	for _, r := range SelftestAll() {
		status := r.Status()
		detail := r.Detail
		if r.Skipped != "" {
			detail = "skipped: " + r.Skipped
		}
		if r.Fail != "" {
			detail = "fail: " + r.Fail
		}
		fmt.Fprintf(&b, "%-28s %s\n", status+":", r.Primitive+" — "+detail)
	}
	return b.String()
}

// noOpStub is the deliberately no-op primitive the AC-3 anti-gaming rule
// requires: it "runs", lands nothing provable, and grades no_op. A caller
// that trusts exit codes reads a green run; a caller that checks
// Proof.ReadbackOk fails it.
type noOpStub struct{ name string }

// Name implements primitive.
func (s noOpStub) Name() string { return s.name }

// Probe implements primitive: claims available (that is what makes the
// stub dangerous and the rule necessary).
func (s noOpStub) Probe() (CapabilityResult, error) {
	return CapabilityResult{Available: true, Name: s.name,
		Basis: "stub backend: claims availability without evidence (AC-3 fixture)"}, nil
}

// Inject implements primitive: lands nothing, returns the no_op proof and
// a reverter that refuses to claim a revert (nothing landed, nothing to
// revert — a reverter that "succeeded" here would be manufacturing proof).
func (s noOpStub) Inject() (Proof, Release, error) {
	return Proof{Primitive: s.name, Landed: false, Verdict: VerdictNoOp,
			Property:   "stub backend: nothing landed (AC-3 no-op fixture)",
			ReadbackOk: false,
		}, func() (string, error) {
			return "", fmt.Errorf("nothing landed: the no-op stub has nothing to revert (AC-3)")
		}, nil
}

// Selftest implements primitive: the stub NEVER selftests green (a stub
// that could selftest green would defeat the AC-3 contract it exists to
// exercise).
func (s noOpStub) Selftest() SelftestReport {
	return SelftestReport{Primitive: s.name, Fail: "stub backend never selftests green (AC-3 fixture)"}
}

// Resolve returns the named primitive; an unknown name resolves to the
// no-op stub (AC-3: "inject a deliberately no-op primitive" is the
// contract test for every caller). Name it with the requested name so the
// run's output names what failed to land.
func Resolve(name string) primitive {
	if p, ok := Table()[name]; ok {
		return p
	}
	return noOpStub{name: name}
}

// ResolveScope returns the systemd-scope primitive typed (the selftest
// harness consumes the named-unit variant); the second return is false
// when the name does not resolve to the scope primitive.
func ResolveScope(name string) (systemdScope, bool) {
	if p, ok := Table()[name]; ok {
		if s, ok := p.(systemdScope); ok {
			return s, true
		}
	}
	return systemdScope{}, false
}

// IsStub reports whether p is the no-op stub.
func IsStub(p primitive) bool { return IsNoOpStub(p) }

// IsNoOpStub is the type check behind IsStub.
func IsNoOpStub(p primitive) bool {
	_, ok := p.(noOpStub)
	return ok
}
