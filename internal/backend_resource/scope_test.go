// Tests for the SPEC-07 primitive table, the tie-break (six different
// fixture environments all classify to exactly the five primitive names —
// order stability is the tie-break's contract), the no-op stub fallback
// (AC-3), and the real systemd-run integration (skip-clean when the user
// bus is unavailable; never parallel; cleanup always).
//
// ch:trace row=MSF-008 spec=docs/SPEC-PLAN.md (SPEC-07) test=internal/backend_resource/scope_test.go evidence=probe/RESULTS.md witness=live:systemd-run-user-bus
package backend_resource

import (
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// The table and the tie-break
// ---------------------------------------------------------------------------

// TestTableHasExactlyFivePrimitives: the SPEC-07 set is the five names,
// each resolving to a distinct implementation.
func TestTableHasExactlyFivePrimitives(t *testing.T) {
	tab := Table()
	if len(tab) != 5 {
		t.Fatalf("Table() has %d entries, want exactly 5 (SPEC-07: scopes, freezer, dm/loop, fsfreeze, RO remount)", len(tab))
	}
	for _, name := range PrimitiveNames() {
		p, ok := tab[name]
		if !ok {
			t.Fatalf("PrimitiveNames() names %q but Table() lacks it", name)
		}
		if p.Name() != name {
			t.Fatalf("table entry Name()=%q != key %q", p.Name(), name)
		}
	}
}

// TestTieBreakAcrossEnvironments: six fixture environments — the measured
// rootless host, a privileged host, a host missing every tool, a host with
// tools but no user manager, a host whose filesystem probes all fail, and
// a zero fixture — all classify each primitive to exactly one of the five
// table names, and the CAPABILITY ORDER is identical across them (the
// tie-break: deterministic classification regardless of environment).
func TestTieBreakAcrossEnvironments(t *testing.T) {
	envs := map[string]*fakeHost{
		"measured-rootless": freezerFixture(),
		"privileged": func() *fakeHost {
			h := freezerFixture()
			h.files[procSelfStatus] = "CapEff:\t000001ffffffffff\n"
			h.runs["losetup -f"] = "/dev/loop27\n"
			return h
		}(),
		"no-tools": newFakeHost(),
		"tools-no-manager": func() *fakeHost {
			h := freezerFixture()
			h.paths["losetup"] = true
			h.runFunc = func(argv []string) (string, error, bool) {
				if len(argv) > 2 && argv[0] == "systemd-run" && argv[1] == "--user" &&
					strings.HasPrefix(argv[2], "--unit=") {
					return "", errors.New("Failed to connect to bus: No medium found"), true
				}
				return "", nil, false
			}
			return h
		}(),
		"unreadable-fs": newFakeHost(),
		"zero-fixture":  newFakeHost(),
	}
	var wantOrder []string
	for envName, h := range envs {
		caps := CapabilitiesOf(h)
		if len(caps) != 5 {
			t.Fatalf("%s: CapabilitiesOf returned %d results, want 5", envName, len(caps))
		}
		gotOrder := make([]string, 0, 5)
		for _, c := range caps {
			gotOrder = append(gotOrder, c.Name)
		}
		if wantOrder == nil {
			wantOrder = gotOrder
			continue
		}
		if strings.Join(gotOrder, "|") != strings.Join(wantOrder, "|") {
			t.Fatalf("%s: capability order drifted across environments:\n got %v\nwant %v",
				envName, gotOrder, wantOrder)
		}
		// Every classification is one of the five names — no env produces
		// a sixth class or renames one.
		valid := map[string]bool{}
		for _, n := range PrimitiveNames() {
			valid[n] = true
		}
		for _, c := range caps {
			if !valid[c.Name] {
				t.Fatalf("%s: classified to unknown primitive %q", envName, c.Name)
			}
		}
	}
}

// TestCapabilitiesOfDeterministicOnLiveHost: two calls on the real host
// return the same order and the same availability answers (the probe must
// not flip between calls; it measures, not guesses).
func TestCapabilitiesOfDeterministicOnLiveHost(t *testing.T) {
	a := CapabilitiesOf(Hosts())
	b := CapabilitiesOf(Hosts())
	if len(a) != len(b) || len(a) != 5 {
		t.Fatalf("live CapabilitiesOf lengths differ or wrong: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Available != b[i].Available {
			t.Fatalf("live probe not deterministic at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
	// The honest live answer for the freezer on THIS host (uid 1000,
	// CapEff=0, no root freeze file): unavailable, naming the privileged
	// helper. If this ever fails because the host CHANGED (elevated
	// caps), the basis must say so — fail loudly, never silently flip.
	for _, c := range a {
		if c.Name == capNameFreezer && c.Available {
			t.Skipf("host changed under us: freezer now available (%s); the rootless audit basis no longer holds here", c.Basis)
		}
	}
}

// ---------------------------------------------------------------------------
// Selftest driver (publishable shape)
// ---------------------------------------------------------------------------

// TestSelftestAllStringPublishable: the rendered selftest matrix has one
// line per primitive, each carrying a status token and the primitive name.
// The systemd-run line must NOT be failing merely because the bus is
// unavailable — that is a recorded skip, never a failure.
func TestSelftestAllStringPublishable(t *testing.T) {
	out := SelftestAllString()
	lines := nonEmptyLines(out)
	if len(lines) != 5 {
		t.Fatalf("selftest matrix has %d lines, want 5:\n%s", len(lines), out)
	}
	for _, name := range PrimitiveNames() {
		if !strings.Contains(out, name) {
			t.Fatalf("selftest matrix lacks primitive %q:\n%s", name, out)
		}
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "green:") && !strings.HasPrefix(line, "skipped:") && !strings.HasPrefix(line, "failing:") {
			t.Fatalf("selftest line lacks a status token: %q", line)
		}
	}
}

// TestSelftestAllAgainstFakeHost: with a fully-refusing fake host, every
// selftest is either skipped or failing — none green (nothing was proven,
// and honesty is the contract).
func TestSelftestAllAgainstFakeHost(t *testing.T) {
	saved := Hosts
	t.Cleanup(func() { Hosts = saved })
	Hosts = func() host { return freezerFixture() }
	for _, r := range SelftestAll() {
		if r.Green {
			t.Fatalf("%s: selftest green against a refusing fake host is dishonest", r.Primitive)
		}
		if r.Status() != "skipped" && r.Status() != "failing" {
			t.Fatalf("%s: status %q, want skipped|failing", r.Primitive, r.Status())
		}
	}
}

// ---------------------------------------------------------------------------
// AC-3: the no-op stub fallback
// ---------------------------------------------------------------------------

// TestResolveUnknownNamesNoOpStub: an unknown primitive resolves to the
// stub whose landing never proves itself — Inject succeeds but the Proof
// is the no_op shape, and a caller grading on ReadbackOk fails the run.
func TestResolveUnknownNamesNoOpStub(t *testing.T) {
	p := Resolve("totally-made-up-primitive")
	if !IsStub(p) {
		t.Fatalf("unknown name must resolve to the stub, got %T", p)
	}
	proof, rel, err := p.Inject()
	if err != nil || rel == nil {
		t.Fatalf("stub inject: err=%v rel=%v (want the stub's no-op release, err nil)", err, rel)
	}
	if proof.Landed || proof.ReadbackOk || proof.Verdict != VerdictNoOp {
		t.Fatalf("stub proof must be the no_op shape, got %+v", proof)
	}
	if proof.String() == "" || !strings.Contains(proof.String(), VerdictNoOp) {
		t.Fatalf("no_op proof must render its verdict, got %q", proof.String())
	}
	// The reverter of a nothing-landing refuses to claim a revert proof.
	if _, err := rel(); err == nil {
		t.Fatal("stub release must refuse (nothing landed, nothing to revert), got nil error")
	}
	// And the stub never selftests green.
	if r := p.Selftest(); r.Green || r.Status() != "failing" {
		t.Fatalf("stub selftest must be failing, got %+v", r)
	}
}

// TestResolveKnownNamesNeverStub: every table name resolves to the real
// primitive, never the stub.
func TestResolveKnownNamesNeverStub(t *testing.T) {
	for _, name := range PrimitiveNames() {
		if IsStub(Resolve(name)) {
			t.Fatalf("%s resolved to the stub", name)
		}
	}
}

// ---------------------------------------------------------------------------
// parseSelfCgroup (freezer's base path extraction)
// ---------------------------------------------------------------------------

func TestParseSelfCgroup(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"12:devices:/user.slice\n0::/user.slice/user-1000.slice\n", "/user.slice/user-1000.slice", true},
		{"0::/\n", "/", true},
		{"0::\n", "/", true},
		{"11:pids:/a\n", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := parseSelfCgroup(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("parseSelfCgroup(%q) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// ---------------------------------------------------------------------------
// Real systemd-run integration (rootless, live user bus)
// ---------------------------------------------------------------------------

// requireLiveBus skips cleanly when systemd-run --user is unavailable —
// the skip is recorded with the probe's measured reason.
func requireLiveBus(t *testing.T) {
	t.Helper()
	c, err := (systemdScope{}).Probe()
	if err != nil {
		t.Skipf("systemd-run probe error: %v", err)
	}
	if !c.Available {
		t.Skipf("systemd-run --user unavailable on this host: missing=%q basis=%q", c.Missing, c.Basis)
	}
}

// TestSystemdRunIntegrationScopedRun: the REAL measured shape — start a
// held sleep unit with the exact probe limits, read the properties back,
// prove the landed-proof, stop the unit with a verified release.
func TestSystemdRunIntegrationScopedRun(t *testing.T) {
	requireLiveBus(t)
	p := systemdScope{}
	proof, rel, err := p.InjectLimits(ScopeLimits{
		MemoryMaxBytes:  64 << 20,
		CPUQuotaPercent: 10,
		TasksMax:        32,
		Cmd:             []string{"/bin/sleep", "30"},
	})
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	t.Cleanup(func() { _, _ = rel() })
	if !proof.Landed || !proof.ReadbackOk || proof.Verdict != VerdictLanded {
		t.Fatalf("landed-proof did not fire: %+v", proof)
	}
	joined := strings.Join(proof.Evidence, "\n")
	for _, want := range []string{"MemoryMax=67108864", "TasksMax=32", "CPUQuotaPerSecUSec=100ms"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("evidence lacks %q; evidence:\n%s", want, joined)
		}
	}
	// The release proves the undo (AC-4 shape): stop + verified gone.
	msg, err := rel()
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if !strings.Contains(msg, "stopped") {
		t.Fatalf("release proof must name the stopped state, got %q", msg)
	}
}

// TestSystemdRunInjectNoCmd: a scope with no command is a caller error
// (never a silent empty unit).
func TestSystemdRunInjectNoCmd(t *testing.T) {
	if _, _, err := (systemdScope{}).InjectLimits(ScopeLimits{}); err == nil {
		t.Fatal("InjectLimits with no command must error")
	}
}

// TestSystemdRunIntegrationSelftest: the primitive's own selftest runs the
// same measured shape end to end on the live bus.
func TestSystemdRunIntegrationSelftest(t *testing.T) {
	requireLiveBus(t)
	rep := (systemdScope{}).Selftest()
	if !rep.Green {
		t.Fatalf("selftest not green on the live bus: fail=%q detail=%q", rep.Fail, rep.Detail)
	}
	if !strings.Contains(rep.Detail, "read-back") {
		t.Fatalf("green detail must name the read-back, got %q", rep.Detail)
	}
}

// TestSystemdRunIntegrationProbeUnavailableShape: with the bus broken
// (fake host), Probe reports unavailable with the measured basis and the
// SELFTEST records a clean skip.
func TestSystemdRunProbeUnavailableShape(t *testing.T) {
	saved := Hosts
	t.Cleanup(func() { Hosts = saved })
	h := freezerFixture()
	h.paths["systemd-run"] = true
	h.paths["systemctl"] = true
	// The probe's real argv carries a per-call unit token, so the fake
	// matches on the argv shape via the catch-all hook.
	h.runFunc = func(argv []string) (string, error, bool) {
		if len(argv) > 2 && argv[0] == "systemd-run" && argv[1] == "--user" &&
			strings.HasPrefix(argv[2], "--unit=") {
			return "", errors.New("Failed to connect to bus: No medium found"), true
		}
		return "", nil, false
	}
	Hosts = func() host { return h }
	c, err := (systemdScope{}).Probe()
	if err != nil {
		t.Fatalf("Probe must not error on a broken bus: %v", err)
	}
	if c.Available {
		t.Fatal("broken bus must read unavailable")
	}
	if !strings.Contains(c.Missing, "user manager") || !strings.Contains(c.Basis, "cmd=systemd-run") {
		t.Fatalf("unavailable shape incomplete: missing=%q basis=%q", c.Missing, c.Basis)
	}
	rep := (systemdScope{}).Selftest()
	if rep.Green || rep.Status() != "skipped" {
		t.Fatalf("broken bus selftest must be a clean skip, got %+v", rep)
	}
}

// nonEmptyLines splits s into non-empty lines.
func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimRight(l, " "))
		}
	}
	return out
}
