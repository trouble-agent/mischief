package profile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// shortwrite_test.go — the real S-004 actuator: generate the shim, build it,
// land a short write, and prove it. Everything here is measured, not asserted
// from the actuator's own bookkeeping (the witness is the child's write(2)
// return count plus the parent's read of the target).

func s004(t *testing.T) *catalog.Descriptor {
	t.Helper()
	cat, err := catalog.LoadDir(filepath.Join("..", "..", "catalog", "faults"))
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	d := cat.Get("S-004")
	if d == nil {
		t.Fatal("S-004 is not in the catalog")
	}
	return d
}

func TestShortWriteLandsAndDetaches(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no C compiler (cc) on PATH: the shim cannot be built here")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "mischief-profile-target")
	req := LandRequest{
		Step:   Step{ID: "s1", Fault: "S-004", Probe: ProbePayloadComplete, Params: paramMap{"fraction": "0.5", "budget": "1"}},
		Desc:   s004(t),
		Target: Target{Kind: "scratch", Selector: "scratch:" + target, Path: target},
		Dir:    dir,
		RunID:  "run-abc123",
		Seed:   7,
	}
	act := &ShortWrite{}
	res := act.Apply(context.Background(), req)
	if !res.Landed {
		t.Fatalf("fault did not land: %s", res.Reason)
	}
	if res.Witness == nil || res.Witness.Evidence == "" {
		t.Fatalf("landed without a witness: %+v", res.Witness)
	}
	// The measured observation, not the actuator's claim: one shortened write,
	// the payload still complete because the reference writer retries.
	if res.Obs.ShortWrites < 1 {
		t.Fatalf("observed %d short writes (want >= 1)", res.Obs.ShortWrites)
	}
	if res.Obs.OnDisk != int64(res.Obs.PayloadLen) {
		t.Fatalf("on disk %d of payload %d", res.Obs.OnDisk, res.Obs.PayloadLen)
	}
	if res.Obs.Attempts < 2 {
		t.Fatalf("attempts = %d (a short write must force a second write)", res.Obs.Attempts)
	}
	if pr := probePayloadComplete(res.Obs); !pr.Passed {
		t.Fatalf("probe did not hold: %+v", pr)
	}
	// The injector's own counter is corroboration: it must exist and agree.
	hits, err := readCounterHits(filepath.Join(dir, "shim", "shortwrite.counter"))
	if err != nil {
		t.Fatalf("shim counter: %v", err)
	}
	if hits < 1 {
		t.Fatalf("shim counter hits = %d", hits)
	}
	// The generated sources are the injection log a reviewer reads.
	for _, f := range []string{"libshortwrite.c", "writer.c"} {
		if _, err := os.Stat(filepath.Join(dir, "shim", f)); err != nil {
			t.Fatalf("generated source %s missing: %v", f, err)
		}
	}

	det := act.Detach(context.Background(), req)
	if det.Witness == nil {
		t.Fatalf("inverse not proven: %s", det.Reason)
	}
	if !strings.Contains(det.Witness.Evidence, "shorts=0") {
		t.Fatalf("detach evidence does not show the fault gone: %s", det.Witness.Evidence)
	}
}

// TestShortWriteRefusesUnmatchableTarget pins that a rule which can never
// match is NOT-LANDED with the reason, not a silent pass.
func TestShortWriteRefusesUnmatchableTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "other-name")
	req := LandRequest{
		Step:   Step{ID: "s1", Fault: "S-004", Probe: ProbePayloadComplete, Params: paramMap{"path_substr": "mischief-profile-target"}},
		Desc:   s004(t),
		Target: Target{Kind: "scratch", Path: target},
		Dir:    dir,
		RunID:  "run-x",
	}
	res := (&ShortWrite{}).Apply(context.Background(), req)
	if res.Landed {
		t.Fatal("landed against a path the rule cannot match")
	}
	if !strings.Contains(res.Reason, "path_substr") {
		t.Fatalf("reason %q does not name the mismatch", res.Reason)
	}
}

// TestShortWriteRefusesNonScratchTarget pins the attachable-kind contract.
func TestShortWriteRefusesNonScratchTarget(t *testing.T) {
	req := LandRequest{
		Step:   Step{ID: "s1", Fault: "S-004", Probe: ProbePayloadComplete},
		Desc:   s004(t),
		Target: Target{Kind: "process", Selector: "process:1234"},
		Dir:    t.TempDir(),
	}
	res := (&ShortWrite{}).Apply(context.Background(), req)
	if res.Landed {
		t.Fatal("landed on a process selector")
	}
	if !strings.Contains(res.Reason, "cannot be faulted") {
		t.Fatalf("reason = %q", res.Reason)
	}
}

// TestShortWriteRefusesBadFraction pins that an out-of-range fraction is a
// named refusal (a "short" write that lands everything is not a fault).
func TestShortWriteRefusesBadFraction(t *testing.T) {
	dir := t.TempDir()
	req := LandRequest{
		Step:   Step{ID: "s1", Fault: "S-004", Probe: ProbePayloadComplete, Params: paramMap{"fraction": "1.5"}},
		Desc:   s004(t),
		Target: Target{Kind: "scratch", Path: filepath.Join(dir, "mischief-profile-target")},
		Dir:    dir,
		RunID:  "run-x",
	}
	res := (&ShortWrite{}).Apply(context.Background(), req)
	if res.Landed {
		t.Fatal("landed with an impossible fraction")
	}
	if !strings.Contains(res.Reason, "fraction") {
		t.Fatalf("reason = %q", res.Reason)
	}
}

func TestParseFraction(t *testing.T) {
	ok := []struct {
		in       string
		num, den int
	}{
		{"0.5", 50, 100},
		{"1/2", 1, 2},
		{"3/4", 3, 4},
		{"0.25", 25, 100},
	}
	for _, c := range ok {
		num, den, err := parseFraction(c.in)
		if err != nil || num != c.num || den != c.den {
			t.Fatalf("parseFraction(%q) = %d/%d, %v", c.in, num, den, err)
		}
	}
	for _, bad := range []string{"", "0", "1", "1/1", "2/2", "1.5", "-0.5", "1/0", "abc", "0/0"} {
		if _, _, err := parseFraction(bad); err == nil {
			t.Fatalf("parseFraction(%q) accepted an impossible fraction", bad)
		}
	}
}

func TestParseBudget(t *testing.T) {
	if n, err := parseBudget("3"); err != nil || n != 3 {
		t.Fatalf("parseBudget = %d, %v", n, err)
	}
	if n, err := parseBudget("0"); err != nil || n != 0 {
		t.Fatalf("parseBudget(0) = %d, %v", n, err)
	}
	if _, err := parseBudget("lots"); err == nil {
		t.Fatal("parseBudget accepted a non-integer")
	}
}

// TestScenarioPayloadIsDeterministic pins AC-10's replay precondition: the same
// run id and seed write the same bytes, a different seed does not.
func TestScenarioPayloadIsDeterministic(t *testing.T) {
	a := scenarioPayload("run-1", 7, 32)
	b := scenarioPayload("run-1", 7, 32)
	if string(a) != string(b) {
		t.Fatal("scenario payload is not deterministic")
	}
	if string(a) == string(scenarioPayload("run-1", 8, 32)) {
		t.Fatal("a different seed must not write the same payload")
	}
	if len(a) != 32 {
		t.Fatalf("payload length = %d", len(a))
	}
}
