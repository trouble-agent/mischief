// Systemd-run user scopes (R-001 backend=scope): the measured rootless
// resource-limit shape. The measured command is
//
//	systemd-run --user --scope -p MemoryMax=64M -p CPUQuota=10% -p TasksMax=32 true
//
// which returned rc 0 on this host (probe/RESULTS.md, re-measured while
// writing this package: MemoryMax=67108864, CPUQuotaPerSecUSec=100ms,
// TasksMax=32, and the unit GCs to not-found/inactive after stop).
//
// The primitive models a scoped HELD run (a `--unit` service, not `--scope`,
// because a held fault needs a stoppable unit): it starts the target command
// inside a transient service unit with the declared limits, proves the
// limits landed by reading them BACK from systemd, and stops the unit on
// Release. Landed-proof is a measured read-back (AC-3), never the start
// command's exit code (AC-13).
package backend_resource

import (
	"fmt"
	"strings"
)

// ScopeLimits is the declared resource limit set for a systemd-run scope.
type ScopeLimits struct {
	// MemoryMaxBytes is the cgroup memory.max the scope enforces (0 = unset).
	MemoryMaxBytes int64
	// CPUQuotaPercent is the CPU quota percentage (0 = unset; 10 →
	// CPUQuotaPerSecUSec=100ms on this host).
	CPUQuotaPercent int
	// TasksMax is the pids.max the scope enforces (0 = unset).
	TasksMax int
	// Cmd is the command the scope runs.
	Cmd []string
}

// SystemdScope is the exported alias of the scope primitive (the selftest
// harness consumes the named-unit variant through it).
type SystemdScope = systemdScope

// systemdScope is the primitive: probe, inject, release, selftest.
type systemdScope struct{}

// Name implements primitive. (The catalog tie is R-001, backend=scope;
// the registry key and the primitive name are the same publishable
// string.)
func (systemdScope) Name() string { return capNameScope }

// Probe implements primitive. Available requires the binary AND a working
// user bus: a systemd-run whose "Running as unit" line never arrives cannot
// prove a landed scope, so the probe measures the round trip, not the PATH.
// (The `true` command matches the measured probe shape exactly.)
func (systemdScope) Probe() (CapabilityResult, error) {
	return systemdScope{}.probeWith(Hosts())
}

// probeWith is the seam-injectable probe body (the tests' DI point).
func (systemdScope) probeWith(h host) (CapabilityResult, error) {
	if _, err := h.LookPath("systemd-run"); err != nil {
		return CapabilityResult{Name: capNameScope}, nil
	}
	if _, err := h.LookPath("systemctl"); err != nil {
		return CapabilityResult{Name: capNameScope}, nil
	}
	unit := "msf-probe-" + scratchToken()
	out, err := h.Run([]string{"systemd-run", "--user", "--unit=" + unit,
		"-p", "MemoryMax=64M", "-p", "CPUQuota=10%", "-p", "TasksMax=32", "true"})
	if err != nil {
		// Best-effort cleanup of the (possibly half-created) unit; the
		// probe's answer is the same either way.
		_, _ = h.Run([]string{"systemctl", "--user", "stop", unit})
		return CapabilityResult{
			Name:    capNameScope,
			Missing: "working systemd user manager (systemd-run --user failed: " + errText(err) + ")",
			Basis:   "cmd=systemd-run --user --unit=<unit> -p MemoryMax=64M -p CPUQuota=10% -p TasksMax=32 true",
		}, nil
	}
	if !strings.Contains(out, "Running as unit") {
		return CapabilityResult{
			Name:    capNameScope,
			Missing: "transient unit creation (no 'Running as unit' line in systemd-run output)",
			Basis:   "output=" + truncateLine(out),
		}, nil
	}
	return CapabilityResult{
		Available: true,
		Name:      capNameScope,
		Basis:     "measured: systemd-run --user scope round trip OK (" + truncateLine(out) + ")",
	}, nil
}

// Inject implements primitive. It starts <cmd> as transient user service
// unit with the declared limits, then proves the landed state by reading
// the limits back from systemd. Any read-back gap is an error (and the
// unit is stopped), never a pass.
func (systemdScope) Inject() (Proof, Release, error) {
	return systemdScope{}.InjectLimits(ScopeLimits{
		MemoryMaxBytes:  64 << 20,
		CPUQuotaPercent: 10,
		TasksMax:        32,
		Cmd:             []string{"/bin/sleep", "30"},
	})
}

// InjectLimits lands a scoped held run for the DECLARED limits: it starts
// <cmd> as a transient user service unit, then proves the landed state by
// reading the limits back from systemd. Any read-back gap is an error
// (and the unit is stopped), never a pass.
func (systemdScope) InjectLimits(s ScopeLimits) (Proof, Release, error) {
	return systemdScope{}.InjectLimitsNamed(s, "msf-run-"+scratchToken())
}

// InjectLimitsNamed is InjectLimits with a caller-chosen unit name: the
// selftest harness mints the name BEFORE landing so its pre-state capture
// can query the exact object the fault will create (an object-scoped
// revert measurement, immune to concurrent units on the shared namespace).
// The measured shape is identical to InjectLimits.
func (systemdScope) InjectLimitsNamed(s ScopeLimits, unit string) (Proof, Release, error) {
	h := Hosts()
	if len(s.Cmd) == 0 {
		return Proof{}, nil, fmt.Errorf("scope inject: no command declared")
	}
	if strings.TrimSpace(unit) == "" {
		return Proof{}, nil, fmt.Errorf("scope inject: empty unit name")
	}
	cap, err := systemdScope{}.Probe()
	if err != nil {
		return Proof{}, nil, err
	}
	if !cap.Available {
		return Proof{}, nil, NewCapabilityError(cap)
	}
	argv := []string{"systemd-run", "--user", "--unit=" + unit}
	argv = appendScopeProps(argv, s)
	argv = append(argv, s.Cmd...)
	out, err := h.Run(argv)
	if err != nil {
		return Proof{}, nil, fmt.Errorf("scope inject: %w", err)
	}
	rel := func() (string, error) { return stopUnit(h, unit) }
	props, err := readUnitProps(h, unit)
	if err != nil {
		_, _ = rel()
		return Proof{}, nil, fmt.Errorf("scope inject: landed-proof read-back failed: %w", err)
	}
	p := Proof{
		Primitive:  systemdScope{}.Name(),
		Landed:     true,
		Verdict:    VerdictLanded,
		Evidence:   appendUnitEvidence("systemd-run", truncateLine(out), props),
		Property:   "scope properties read back from systemd (" + unit + ")",
		ReadbackOk: true,
	}
	return p, rel, nil
}

// appendScopeProps renders the -p properties from the declared limits
// (MemoryMax bytes, CPUQuota percent, TasksMax).
func appendScopeProps(argv []string, s ScopeLimits) []string {
	if s.MemoryMaxBytes > 0 {
		argv = append(argv, "-p", fmt.Sprintf("MemoryMax=%d", s.MemoryMaxBytes))
	}
	if s.CPUQuotaPercent > 0 {
		argv = append(argv, "-p", fmt.Sprintf("CPUQuota=%d%%", s.CPUQuotaPercent))
	}
	if s.TasksMax > 0 {
		argv = append(argv, "-p", fmt.Sprintf("TasksMax=%d", s.TasksMax))
	}
	return argv
}

// readUnitProps reads the scope-relevant unit properties back from
// systemd: the exact read-back the landed-proof cites (AC-3).
func readUnitProps(h host, unit string) ([]string, error) {
	out, err := h.Run([]string{"systemctl", "--user", "show", unit,
		"-p", "MemoryMax", "-p", "TasksMax", "-p", "CPUQuotaPerSecUSec", "-p", "ActiveState"})
	if err != nil {
		return nil, newHostErr([]string{"systemctl", "--user", "show", unit}, err)
	}
	var props []string
	sawActive := false
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "ActiveState=") {
			sawActive = true
		}
		props = append(props, line)
	}
	if !sawActive {
		return nil, fmt.Errorf("systemctl show returned no ActiveState (output: %s)", truncateLine(out))
	}
	return props, nil
}

// stopUnit stops a user unit and verifies it is gone (LoadState not-found
// or ActiveState inactive) — the measured release shape (a stopped unit
// GCs to not-found/inactive on this host). A stop whose exit code is zero
// but whose unit is still active is an ERROR.
func stopUnit(h host, unit string) (string, error) {
	if _, err := h.Run([]string{"systemctl", "--user", "stop", unit}); err != nil {
		return "", newHostErr([]string{"systemctl", "--user", "stop", unit}, err)
	}
	out, err := h.Run([]string{"systemctl", "--user", "show", unit, "-p", "LoadState", "-p", "ActiveState"})
	if err != nil {
		return "", newHostErr([]string{"systemctl", "--user", "show", unit}, err)
	}
	if strings.Contains(out, "LoadState=not-found") || strings.Contains(out, "ActiveState=inactive") {
		return "unit " + unit + " stopped (verified: " + strings.Join(strings.Fields(strings.TrimSpace(out)), " ") + ")", nil
	}
	return "", fmt.Errorf("unit %s still loaded after stop (%s)", unit, truncateLine(out))
}

// Selftest implements primitive. Green on this host requires the real
// measured command to land AND its read-back to match the declared limits
// (the measured renderings: MemoryMax bytes as decimal, CPUQuotaPerSecUSec
// for the quota, TasksMax decimal). Unavailable → clean skip (recorded,
// not failed). Available but unprovable → FAIL (AC-3: a primitive that
// cannot land must not grade a fake pass).
func (p systemdScope) Selftest() SelftestReport {
	rep := SelftestReport{Primitive: p.Name()}
	cap, err := p.Probe()
	if err != nil {
		rep.Fail = fmt.Sprintf("probe error: %v", err)
		return rep
	}
	if !cap.Available {
		rep.Skipped = cap.Missing
		return rep
	}
	proof, rel, err := p.Inject()
	if err != nil {
		rep.Fail = "selftest unit failed: " + errText(err)
		return rep
	}
	relMsg, relErr := rel()
	if relErr != nil {
		rep.Fail = "selftest: revert proof failed: " + errText(relErr)
		return rep
	}
	if !proof.Landed || !proof.ReadbackOk {
		rep.Fail = "selftest: landed-proof did not fire"
		return rep
	}
	joined := strings.Join(proof.Evidence, "\n")
	for _, want := range []string{"MemoryMax=67108864", "TasksMax=32", "CPUQuotaPerSecUSec=100ms"} {
		if !strings.Contains(joined, want) {
			rep.Fail = fmt.Sprintf("selftest: read-back lacks %s (evidence: %s)", want, truncateLine(joined))
			return rep
		}
	}
	rep.Green = true
	rep.Detail = "measured scope landed + read-back green; revert verified (" + truncateLine(relMsg) + ")"
	return rep
}
