package backend_net

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/trouble-agent/mischief/internal/rails"
)

// PrivilegedHelper is the SPEC-08 netns/netem actuator (SPEC-PLAN: "netns +
// netem via the privileged helper, sudo allowlist contract"). The measured
// basis (probe/RESULTS.md): netns+netem works ONLY with sudo — the qdisc
// showed `loss 100%`, the ping inside the namespace failed rc=2, and the
// netns delete reverted cleanly. Unprivileged user+net namespaces cannot
// create a veth on this host (EPERM — Ubuntu AppArmor userns restriction),
// so there is no rootless netns path: the primitive either goes through the
// allowlisted helper or refuses naming it.
type PrivilegedHelper struct {
	// NetnsName is the namespace the helper manages (probe shape:
	// mc-probe-ns; a run uses one dedicated name).
	NetnsName string
	// Timeout bounds each helper invocation.
	Timeout time.Duration

	// lookPath is the seam tests use to simulate an absent sudo.
	lookPath func(string) (string, error)
	// runner executes one allowlisted `sudo ip ...` invocation; tests
	// inject a fake.
	runner func(argv []string, timeout time.Duration) (string, error)
}

// NewPrivilegedHelper builds a helper for netnsName with production sudo
// lookup and os/exec execution.
func NewPrivilegedHelper(netnsName string, timeout time.Duration) *PrivilegedHelper {
	return &PrivilegedHelper{
		NetnsName: netnsName,
		Timeout:   timeout,
		lookPath:  exec.LookPath,
		runner:    runSudo,
	}
}

// runSudo is the production runner: `sudo <argv...>` under a timeout. The
// only argv shapes ever passed here are the allowlisted ones: `ip netns
// add/del <name>` and `ip netns exec <name> tc qdisc ...` — exactly the
// command shapes the probe measured.
func runSudo(argv []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", argv...)
	out, err := cmd.CombinedOutput()
	if cerr := ctx.Err(); cerr != nil {
		return string(out), fmt.Errorf("helper timed out after %s: %w", timeout, cerr)
	}
	return string(out), err
}

// NetemApply arms netem (loss %, delay ms) on lo inside the managed
// namespace and returns the measured qdisc counters — the landed-proof
// read-back (AC-3: the primitive reports what the qdisc SHOWS, not what it
// was told). When sudo is absent it refuses with a CapabilityError naming
// the missing helper; a mismatch between armed and shown counters is a
// not-landed error and the namespace is reverted.
func (h *PrivilegedHelper) NetemApply(lossPct, delayMS int) (qdiscCounters, error) {
	if err := h.checkSudo(); err != nil {
		return qdiscCounters{}, err
	}
	if err := sanitizeNetemParams(lossPct, delayMS); err != nil {
		return qdiscCounters{}, err
	}
	timeout := h.timeoutOr(30 * time.Second)
	ns := h.NetnsName

	// 1. netns add (probe: `sudo ip netns add mc-probe-ns`).
	if _, err := h.runner(sudoArgs("ip", "netns", "add", ns), timeout); err != nil {
		return qdiscCounters{}, fmt.Errorf("netns add: %w", err)
	}
	// 2. netem qdisc on lo inside the namespace (allowlisted shape).
	argv := sudoArgs("ip", "netns", "exec", ns, "tc", "qdisc", "add", "dev", "lo", "root", "netem",
		"loss", fmt.Sprintf("%d%%", lossPct), "delay", fmt.Sprintf("%dms", delayMS))
	if _, err := h.runner(argv, timeout); err != nil {
		_, _ = h.runner(sudoArgs("ip", "netns", "del", ns), timeout)
		return qdiscCounters{}, fmt.Errorf("netem apply: %w", err)
	}
	// 3. Counter read-back (probe: `tc qdisc show dev lo` inside the ns).
	showArgv := sudoArgs("ip", "netns", "exec", ns, "tc", "qdisc", "show", "dev", "lo")
	out, err := h.runner(showArgv, timeout)
	if err != nil {
		_, _ = h.runner(sudoArgs("ip", "netns", "del", ns), timeout)
		return qdiscCounters{}, fmt.Errorf("qdisc show: %w", err)
	}
	c := parseQdiscShow(out)
	if c.LossPct != lossPct {
		_, _ = h.runner(sudoArgs("ip", "netns", "del", ns), timeout)
		return c, fmt.Errorf("netem not landed: armed loss %d%%, qdisc shows %d%% (%q)", lossPct, c.LossPct, c.RawLine)
	}
	return c, nil
}

// NetemRevert deletes the managed namespace (the probe's verified revert:
// `sudo ip netns del mc-probe-ns` → netns_del=OK).
func (h *PrivilegedHelper) NetemRevert() error {
	if err := h.checkSudo(); err != nil {
		return err
	}
	if _, err := h.runner(sudoArgs("ip", "netns", "del", h.NetnsName), h.timeoutOr(30*time.Second)); err != nil {
		return fmt.Errorf("netns del: %w", err)
	}
	return nil
}

// checkSudo refuses with a CapabilityError when sudo is not on PATH — the
// refusal NAMES the missing helper (never a silent skip).
func (h *PrivilegedHelper) checkSudo() error {
	look := h.lookPath
	if look == nil {
		look = exec.LookPath
	}
	if _, err := look("sudo"); err != nil {
		return &CapabilityError{
			Primitive: "netns_netem",
			Missing:   "sudo (privileged helper)",
			Detail:    fmt.Sprintf("netns/netem requires the privileged helper: unprivileged user+net namespaces cannot create a veth on this host class (EPERM, Ubuntu AppArmor userns restriction); netem works only via `sudo ip netns exec <ns> tc qdisc ...` (probe/RESULTS.md): %v", err),
		}
	}
	return nil
}

func (h *PrivilegedHelper) timeoutOr(d time.Duration) time.Duration {
	if h.Timeout > 0 {
		return h.Timeout
	}
	return d
}

// sudoArgs prefixes argv with sudo (skipping a sudo already present from
// injected fakes).
func sudoArgs(argv ...string) []string {
	if len(argv) > 0 && argv[0] == "sudo" {
		return argv
	}
	return append([]string{"sudo"}, argv...)
}

// GateCheck is AC-8 pass-through: the backend refuses to land ANY network
// fault on a saturated host by calling internal/rails' LoadGate with a
// fresh reading — the gate and its refusal taxonomy are owned by rails;
// this package does not reimplement them.
func GateCheck(gate *rails.LoadGate) error {
	reading, err := rails.ReadLoad()
	if err != nil {
		return fmt.Errorf("load gate reading: %w", err)
	}
	return gate.Check(reading)
}

// GateCheckReading is the same pass-through with an injected reading (the
// test seam; production uses GateCheck).
func GateCheckReading(gate *rails.LoadGate, r rails.LoadReading) error {
	return gate.Check(r)
}

// CloudPlaneArmed is the seam the run surface sets when the operator armed
// the cloud plane (rails L5 opt-in: --allow-real + allowlist + spend cap).
// The default false is the safe state.
var CloudPlaneArmed bool

// CloudGuard refuses cloud-targeting primitives when no cloud plane is
// armed (AC-17): the refusal NAMES the provider and the plane. When the
// plane is armed the L5 rails remain the authority for the real-provider
// gate; this check is only the not-armed refusal this package owns.
func CloudGuard(primitive, provider string) error {
	if CloudPlaneArmed {
		return nil
	}
	return &CloudPlaneError{
		Primitive: primitive,
		Provider:  provider,
		Detail:    fmt.Sprintf("no cloud plane armed: primitive %q targets real provider %q; arm the cloud plane (rails L5: --allow-real + allowlist + spend cap) or run against the simulator", primitive, provider),
	}
}

// ch:trace row=MSF-009 spec=docs/prd/mischief-v0.1.md#SPEC-08 evidence=internal/backend_net/netns.go witness=none:no-live-host-run-in-worktree
