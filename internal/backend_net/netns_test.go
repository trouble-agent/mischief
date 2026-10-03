package backend_net

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/rails"
)

// fakeRunner records each allowlisted invocation and returns canned output.
type fakeRunner struct {
	calls [][]string
	out   map[string]string
	err   map[string]error
}

func (f *fakeRunner) run(argv []string, timeout time.Duration) (string, error) {
	key := strings.Join(argv, " ")
	f.calls = append(f.calls, argv)
	if err, ok := f.err[key]; ok {
		return "", err
	}
	return f.out[key], nil
}

// qdiscShowLine is the measured probe output shape (probe/RESULTS.md).
func qdiscShowLine(lossPct int) string {
	return fmt.Sprintf("qdisc netem 8001: root refcnt 2 limit 1000 loss %d%% delay 100ms seed 844288704255200251", lossPct)
}

func helperWith(runner *fakeRunner, lookErr error) *PrivilegedHelper {
	h := &PrivilegedHelper{NetnsName: "mc-test-ns", Timeout: 5 * time.Second, runner: runner.run}
	h.lookPath = func(string) (string, error) {
		if lookErr != nil {
			return "", lookErr
		}
		return "/usr/bin/sudo", nil
	}
	return h
}

// TestNetemCapabilityRefusalNamesHelper: with sudo absent the primitive
// REFUSES naming the missing helper — never a silent skip.
func TestNetemCapabilityRefusalNamesHelper(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	h := helperWith(runner, errors.New(`exec: "sudo": executable file not found in $PATH`))

	_, err := h.NetemApply(100, 100)
	var capErr *CapabilityError
	if !errors.As(err, &capErr) {
		t.Fatalf("want CapabilityError, got %T: %v", err, err)
	}
	if !strings.Contains(capErr.Error(), "capability_unavailable") {
		t.Errorf("refusal must say capability_unavailable: %q", capErr.Error())
	}
	if !strings.Contains(capErr.Missing, "sudo") {
		t.Errorf("refusal must NAME the missing helper (sudo): %q", capErr.Missing)
	}
	if len(runner.calls) != 0 {
		t.Errorf("no helper command may run without sudo, ran: %v", runner.calls)
	}
	// Revert takes the same refusal shape.
	if err := h.NetemRevert(); err == nil || !strings.Contains(err.Error(), "capability_unavailable") {
		t.Errorf("revert without sudo must refuse with capability_unavailable, got %v", err)
	}
}

// TestNetemApplyAllowlistAndLandedProof: the happy path runs exactly the
// allowlisted command shapes and reports the qdisc's OWN counters (AC-3).
func TestNetemApplyAllowlistAndLandedProof(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{out: map[string]string{
		"sudo ip netns exec mc-test-ns tc qdisc show dev lo": qdiscShowLine(100),
	}}
	h := helperWith(runner, nil)

	c, err := h.NetemApply(100, 100)
	if err != nil {
		t.Fatalf("NetemApply: %v", err)
	}
	if c.LossPct != 100 || c.DelayMS != 100 {
		t.Errorf("counters: got loss=%d delay=%d, want 100/100", c.LossPct, c.DelayMS)
	}

	// Allowlist contract: netns add, netem qdisc inside ns, qdisc show.
	want := [][]string{
		{"sudo", "ip", "netns", "add", "mc-test-ns"},
		{"sudo", "ip", "netns", "exec", "mc-test-ns", "tc", "qdisc", "add", "dev", "lo", "root", "netem", "loss", "100%", "delay", "100ms"},
		{"sudo", "ip", "netns", "exec", "mc-test-ns", "tc", "qdisc", "show", "dev", "lo"},
	}
	if len(runner.calls) != len(want) {
		t.Fatalf("helper ran %d commands, want %d: %v", len(runner.calls), len(want), runner.calls)
	}
	for i, w := range want {
		if strings.Join(runner.calls[i], " ") != strings.Join(w, " ") {
			t.Errorf("call %d: got %v, want %v (allowlist shape)", i, runner.calls[i], w)
		}
	}
}

// TestNetemNotLandedRefusesAndReverts: when the qdisc does not SHOW what
// was armed the primitive refuses (never claims a landed fault) and the
// namespace is reverted.
func TestNetemNotLandedRefusesAndReverts(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{out: map[string]string{
		"sudo ip netns exec mc-test-ns tc qdisc show dev lo": qdiscShowLine(0), // shows 0%, armed 100%
	}}
	h := helperWith(runner, nil)

	_, err := h.NetemApply(100, 100)
	if err == nil || !strings.Contains(err.Error(), "not landed") {
		t.Fatalf("want not-landed refusal, got %v", err)
	}
	last := runner.calls[len(runner.calls)-1]
	if strings.Join(last, " ") != "sudo ip netns del mc-test-ns" {
		t.Errorf("namespace must be reverted after not-landed, last call: %v", last)
	}
}

// TestNetemRevertAllowlistShape.
func TestNetemRevertAllowlistShape(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	h := helperWith(runner, nil)
	if err := h.NetemRevert(); err != nil {
		t.Fatalf("NetemRevert: %v", err)
	}
	if len(runner.calls) != 1 || strings.Join(runner.calls[0], " ") != "sudo ip netns del mc-test-ns" {
		t.Errorf("revert = %v, want exactly the allowlisted netns del", runner.calls)
	}
}

// TestNetemParamSanitization: out-of-range parameters refuse before any
// command runs (the allowlist admits plain numbers only).
func TestNetemParamSanitization(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	h := helperWith(runner, nil)
	for _, tc := range []struct {
		Loss, Delay int
		wantErr     bool
	}{
		{-1, 0, true},
		{-1, 0, true},
		{101, 0, true},
		{50, -1, true},
		{50, 60001, true},
	} {
		_, err := h.NetemApply(tc.Loss, tc.Delay)
		if (err != nil) != tc.wantErr {
			t.Errorf("loss=%d delay=%d: err=%v, wantErr=%v", tc.Loss, tc.Delay, err, tc.wantErr)
		}
	}
	if len(runner.calls) != 0 {
		t.Errorf("invalid params must refuse before any command: %v", runner.calls)
	}
}

// TestCloudGuardRefusal (AC-17): a cloud-targeting primitive refuses by
// name when no cloud plane is armed, and passes when the plane is armed.
func TestCloudGuardRefusal(t *testing.T) {
	t.Cleanup(func() { CloudPlaneArmed = false })

	CloudPlaneArmed = false
	err := CloudGuard("P-NET-01", "aws")
	var cloudErr *CloudPlaneError
	if !errors.As(err, &cloudErr) {
		t.Fatalf("want CloudPlaneError, got %T: %v", err, err)
	}
	if !strings.Contains(cloudErr.Error(), "aws") {
		t.Errorf("refusal must name the provider: %q", cloudErr.Error())
	}
	if !strings.Contains(cloudErr.Error(), "not armed") {
		t.Errorf("refusal must say the plane is not armed: %q", cloudErr.Error())
	}

	CloudPlaneArmed = true
	if err := CloudGuard("P-NET-01", "aws"); err != nil {
		t.Errorf("armed cloud plane must pass the guard: %v", err)
	}
}

// TestGateCheckRefusalPassThrough (AC-8): the backend calls rails' load
// gate and passes its refusal through UNCHANGED — same reason value, same
// measured numbers, no reimplementation.
func TestGateCheckRefusalPassThrough(t *testing.T) {
	gate, err := rails.NewLoadGate(rails.LoadGateConfig{LoadThreshold: 2.0, PSIThreshold: 10})
	if err != nil {
		t.Fatal(err)
	}
	reading := rails.LoadReading{Load1: 12.34, Load5: 9.87, PSIIoSomeAvg10: 44.5}
	err = GateCheckReading(gate, reading)
	if err == nil {
		t.Fatal("saturated reading must be refused")
	}
	// The refusal must be rails' OWN type carrying rails' measured text.
	var ref *rails.Refusal
	if !errors.As(err, &ref) {
		t.Fatalf("want a *rails.Refusal passed through, got %T: %v", err, err)
	}
	if ref.Reason != rails.ReasonLoad {
		t.Errorf("reason = %q, want rails.ReasonLoad (taxonomy owned by rails)", ref.Reason)
	}
	for _, want := range []string{"12.34", "44.50", "2.00", "10.00"} {
		if !strings.Contains(ref.Detail, want) {
			t.Errorf("refusal detail must carry the measured numbers (%q missing from %q)", want, ref.Detail)
		}
	}

	// Under the gate the same call passes.
	okReading := rails.LoadReading{Load1: 0.5, Load5: 0.4, PSIIoSomeAvg10: 1.0}
	if err := GateCheckReading(gate, okReading); err != nil {
		t.Errorf("reading under the gate must pass: %v", err)
	}
}
