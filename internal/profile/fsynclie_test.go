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

// fsynclie_test.go — the real S-010 actuator: generate the shim and the
// controlled commit-log database, land the fsync lie on a REAL process, and
// prove it. The witnesses are external (the commit child's own report plus
// the parent's post-exit read of the database file); the shim counter is
// corroboration only, and the probe tests pin that injector-only evidence
// cannot produce a pass.

func s010(t *testing.T) *catalog.Descriptor {
	t.Helper()
	cat, err := catalog.LoadDir(filepath.Join("..", "..", "catalog", "faults"))
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	d := cat.Get("S-010")
	if d == nil {
		t.Fatal("S-010 is not in the catalog")
	}
	return d
}

func fsyncLieRequest(t *testing.T, discipline string) (LandRequest, string, string) {
	t.Helper()
	dir := t.TempDir()
	dbFile := filepath.Join(dir, "mischief-durability.db")
	req := LandRequest{
		Step:  Step{ID: "commit-ack-durability", Fault: "S-010", Probe: ProbeCommitDurable, Params: paramMap{"commits": "3"}},
		Desc:  s010(t),
		Target: Target{Kind: "dbdurability", Selector: "dbdurability:" + discipline + ":" + dbFile, Path: dbFile, Discipline: discipline},
		Dir:   dir,
		RunID: "run-abc123",
		Seed:  11,
	}
	return req, dbFile, dir
}

// TestFsyncLieFsyncingTargetPassesAndDetaches: the fsync-on-commit target
// under the lie — the fault lands (acked records vanish from stable storage)
// and the probe holds (the target synced before every ack and its recovery
// report names the loss instead of claiming it).
func TestFsyncLieFsyncingTargetPassesAndDetaches(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no C compiler (cc) on PATH: the shim cannot be built here")
	}
	req, dbFile, dir := fsyncLieRequest(t, "fsync")
	res := (&FsyncLie{}).Apply(context.Background(), req)
	if !res.Landed {
		t.Fatalf("fault did not land: %s", res.Reason)
	}
	if res.Witness == nil || res.Witness.Kind != "external" || res.Witness.Evidence == "" {
		t.Fatalf("landed without an external witness: %+v", res.Witness)
	}
	// The measured observation, not the actuator's claim: the child acked
	// every commit with successful syncs, and stable storage holds none of
	// them after the child exited.
	if res.Obs.Acks != 3 || res.Obs.Syncs != 3 || res.Obs.SyncFails != 0 {
		t.Fatalf("child report = %+v", res.Obs)
	}
	if res.Obs.StableRecords != 0 {
		t.Fatalf("stable storage holds %d acked records after the lie (want 0)", res.Obs.StableRecords)
	}
	if res.Obs.ClaimedDurable != 0 || res.Obs.Lost != 3 {
		t.Fatalf("recovery report = durable=%d lost=%d (the fsync discipline must verify and name the loss)", res.Obs.ClaimedDurable, res.Obs.Lost)
	}
	if pr := probeCommitDurable(res.Obs); !pr.Passed {
		t.Fatalf("probe did not hold: %+v", pr)
	}
	// The injector's counter is corroboration: it must exist and agree.
	hits, stash, err := readFsyncLieCounter(filepath.Join(dir, "shim", "fsynclie.counter"))
	if err != nil {
		t.Fatalf("shim counter: %v", err)
	}
	if hits < 1 || stash < 1 {
		t.Fatalf("shim counter hits=%d stash_events=%d", hits, stash)
	}
	// The generated sources are the injection log a reviewer reads.
	for _, f := range []string{"libfsynclie.c", "dbchild.c", "libfsynclie.so", "dbchild"} {
		if _, err := os.Stat(filepath.Join(dir, "shim", f)); err != nil {
			t.Fatalf("generated artifact %s missing: %v", f, err)
		}
	}
	// The ack ledger (the target's durable ack log, fsynced for real) still
	// says committed — the pairing the landed_proof names.
	acks, err := countLedgerAcks(dbFile + durabilityAcklogSuffix)
	if err != nil || acks != 3 {
		t.Fatalf("ack log holds %d acks (err %v), want 3", acks, err)
	}

	det := (&FsyncLie{}).Detach(context.Background(), req)
	if det.Witness == nil {
		t.Fatalf("inverse not proven: %s", det.Reason)
	}
	if !strings.Contains(det.Witness.Evidence, "durable=3") {
		t.Fatalf("detach evidence does not show the fault gone: %s", det.Witness.Evidence)
	}
}

// TestFsyncLieNonFsyncingTargetFails: the no-fsync target under the lie —
// the fault lands and the probe breaks (no sync ever covered an ack, and the
// recovery report claims durable records stable storage does not hold).
func TestFsyncLieNonFsyncingTargetFails(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no C compiler (cc) on PATH: the shim cannot be built here")
	}
	req, _, _ := fsyncLieRequest(t, "none")
	res := (&FsyncLie{}).Apply(context.Background(), req)
	if !res.Landed {
		t.Fatalf("fault did not land: %s", res.Reason)
	}
	if res.Obs.Syncs != 0 {
		t.Fatalf("a no-fsync target reported %d syncs", res.Obs.Syncs)
	}
	if res.Obs.StableRecords >= res.Obs.Acks {
		t.Fatalf("stable storage holds %d of %d acked records — the loss did not land", res.Obs.StableRecords, res.Obs.Acks)
	}
	if res.Obs.ClaimedDurable != res.Obs.Acks {
		t.Fatalf("the none discipline must trust its ack (durable=%d acks=%d)", res.Obs.ClaimedDurable, res.Obs.Acks)
	}
	pr := probeCommitDurable(res.Obs)
	if pr.Passed {
		t.Fatalf("probe held for a no-fsync target: %+v", pr)
	}
}

// TestFsyncLieRefusesNonDurabilityTarget pins the attachable-kind contract.
func TestFsyncLieRefusesNonDurabilityTarget(t *testing.T) {
	req := LandRequest{
		Step:   Step{ID: "s1", Fault: "S-010", Probe: ProbeCommitDurable},
		Desc:   s010(t),
		Target: Target{Kind: "scratch", Selector: "scratch:/tmp/x", Path: "/tmp/x"},
		Dir:    t.TempDir(),
	}
	res := (&FsyncLie{}).Apply(context.Background(), req)
	if res.Landed {
		t.Fatal("landed on a scratch selector")
	}
	if !strings.Contains(res.Reason, "cannot be faulted") {
		t.Fatalf("reason = %q", res.Reason)
	}
}

// TestFsyncLieRefusesBadDisciplineAndCommits pins the named refusals: a
// discipline this build does not ship and an out-of-range commits count are
// NOT-LANDED, never silent.
func TestFsyncLieRefusesBadDisciplineAndCommits(t *testing.T) {
	dir := t.TempDir()
	dbFile := filepath.Join(dir, "mischief-durability.db")
	base := LandRequest{
		Step:  Step{ID: "s1", Fault: "S-010", Probe: ProbeCommitDurable},
		Desc:  s010(t),
		Dir:   dir,
		RunID: "run-x",
	}
	req := base
	req.Target = Target{Kind: "dbdurability", Path: dbFile, Discipline: "wal"}
	if res := (&FsyncLie{}).Apply(context.Background(), req); res.Landed || !strings.Contains(res.Reason, "fsync|none") {
		t.Fatalf("bad discipline: landed=%v reason=%q", res.Landed, res.Reason)
	}
	req = base
	req.Target = Target{Kind: "dbdurability", Path: dbFile, Discipline: "fsync"}
	req.Step.Params = paramMap{"commits": "0"}
	if res := (&FsyncLie{}).Apply(context.Background(), req); res.Landed || !strings.Contains(res.Reason, "commits") {
		t.Fatalf("zero commits: landed=%v reason=%q", res.Landed, res.Reason)
	}
	req.Step.Params = paramMap{"commits": "999999"}
	if res := (&FsyncLie{}).Apply(context.Background(), req); res.Landed || !strings.Contains(res.Reason, "bound") {
		t.Fatalf("unbounded commits: landed=%v reason=%q", res.Landed, res.Reason)
	}
}

// TestFsyncLieGeneratedSourcesAreSeedDeterministic: the same profile bytes +
// seed generate byte-identical child and shim sources (the injection log a
// same-seed re-run must reproduce); a different seed generates different
// payload bytes, so a seeded difference is REAL.
func TestFsyncLieGeneratedSourcesAreSeedDeterministic(t *testing.T) {
	dirA, dirB, dirC := t.TempDir(), t.TempDir(), t.TempDir()
	payloadA := scenarioPayload("run-r1", 11, durabilityPayloadBytes)
	payloadB := scenarioPayload("run-r1", 11, durabilityPayloadBytes)
	payloadC := scenarioPayload("run-r1", 12, durabilityPayloadBytes)
	if string(payloadA) != string(payloadB) {
		t.Fatal("same run id + seed derived different payloads")
	}
	if string(payloadA) == string(payloadC) {
		t.Fatal("a different seed derived the same payload — a seeded difference would not be real")
	}
	if err := os.WriteFile(filepath.Join(dirA, "dbchild.c"), []byte(durabilityChildC(payloadA)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "dbchild.c"), []byte(durabilityChildC(payloadB)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirC, "dbchild.c"), []byte(durabilityChildC(payloadC)), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"dbchild.c"} {
		a, err := os.ReadFile(filepath.Join(dirA, f))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dirB, f))
		if err != nil {
			t.Fatal(err)
		}
		c, err := os.ReadFile(filepath.Join(dirC, f))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Fatalf("%s differs between two same-seed runs", f)
		}
		if string(a) == string(c) {
			t.Fatalf("%s is identical across different seeds", f)
		}
	}
}

// TestProbeCommitDurableGrades pins the probe's grading table, including the
// anti-gaming arms: injector-only evidence cannot produce a pass, and a
// zero-ack observation is a permanently-failed premise.
func TestProbeCommitDurableGrades(t *testing.T) {
	cases := []struct {
		name string
		obs  Observation
		want bool
		// wantObserved names the measured condition that must appear in the
		// probe's observed line (the grade is the measurement, not prose).
		wantObserved string
	}{
		{
			name: "fsync target under the lie: synced before every ack, recovery names the loss",
			obs:  Observation{Acks: 3, Syncs: 3, SyncFails: 0, StableRecords: 0, ClaimedDurable: 0, Lost: 3, LieHits: 3},
			want: true, wantObserved: "acks=3",
		},
		{
			name: "no-fsync target: zero syncs covered the acks",
			obs:  Observation{Acks: 3, Syncs: 0, SyncFails: 0, StableRecords: 0, ClaimedDurable: 3, Lost: 0, LieHits: 0},
			want: false, wantObserved: "syncs=0",
		},
		{
			name: "no-fsync target with partial page-cache survival: still claims the absent records",
			obs:  Observation{Acks: 3, Syncs: 0, SyncFails: 0, StableRecords: 1, ClaimedDurable: 3, Lost: 0},
			want: false, wantObserved: "durable=3",
		},
		{
			name: "INJECTOR-ONLY EVIDENCE CANNOT PASS: the shim counted lies, nothing else observed",
			obs:  Observation{Acks: 0, Syncs: 0, SyncFails: 0, StableRecords: 0, ClaimedDurable: 0, Lost: 0, LieHits: 7},
			want: false, wantObserved: "acks=0",
		},
		{
			name: "trusting recovery under a full loss: claims absent records durable",
			obs:  Observation{Acks: 3, Syncs: 3, SyncFails: 0, StableRecords: 0, ClaimedDurable: 3, Lost: 0, LieHits: 3},
			want: false, wantObserved: "durable=3",
		},
		{
			name: "sync failures are not successful coverage",
			obs:  Observation{Acks: 3, Syncs: 3, SyncFails: 2, StableRecords: 3, ClaimedDurable: 3, Lost: 0},
			want: false, wantObserved: "syncfails=2",
		},
		{
			name: "vacuous zero-ack premise",
			obs:  Observation{Acks: 0, Syncs: 0, SyncFails: 0, StableRecords: 0, ClaimedDurable: 0, Lost: 0},
			want: false, wantObserved: "acks=0",
		},
	}
	for _, tc := range cases {
		pr := probeCommitDurable(tc.obs)
		if pr.Passed != tc.want {
			t.Fatalf("%s: passed=%v want=%v (observed %q)", tc.name, pr.Passed, tc.want, pr.Observed)
		}
		if !strings.Contains(pr.Observed, tc.wantObserved) {
			t.Fatalf("%s: the observed line does not carry the measured condition %q: %q", tc.name, tc.wantObserved, pr.Observed)
		}
	}
	// The probe's name is the vocabulary token the profile references.
	if ProbeCommitDurable != "commit-durable" {
		t.Fatalf("probe token = %q", ProbeCommitDurable)
	}
}

// TestProbeCommitDurableInjectorOnlyCannotProduceLandingGrade is the AC-3
// negative arm at the actuator boundary: a shim counter that says the lie
// fired is CORROBORATION, and the landing contract requires the external
// pairing (child report vs the parent's post-exit read). These assertions
// pin that the witness the actuator emits is external, and that the probe's
// pass condition cannot be satisfied by the counter alone.
func TestProbeCommitDurableInjectorOnlyCannotProduceLandingGrade(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no C compiler (cc) on PATH: the shim cannot be built here")
	}
	req, _, dir := fsyncLieRequest(t, "fsync")
	res := (&FsyncLie{}).Apply(context.Background(), req)
	if !res.Landed {
		t.Fatalf("fault did not land: %s", res.Reason)
	}
	// The witness must be EXTERNAL — the counter alone is not proof.
	if res.Witness.Kind != "external" {
		t.Fatalf("landing witness kind = %q (want external)", res.Witness.Kind)
	}
	if !strings.Contains(res.Witness.Source, "parent's post-exit read") {
		t.Fatalf("landing witness source %q does not name the outside observer", res.Witness.Source)
	}
	// And the grade is decoupled from the counter: zero out the observed
	// external legs (as an injector-only reading would) and the probe
	// refuses to pass even with a high lie count.
	injectorOnly := res.Obs
	injectorOnly.Acks, injectorOnly.Syncs = 0, 0
	injectorOnly.StableRecords = 0
	injectorOnly.ClaimedDurable = 0
	injectorOnly.LieHits = 99
	if pr := probeCommitDurable(injectorOnly); pr.Passed {
		t.Fatalf("injector-only evidence produced a pass: %+v", pr)
	}
	_ = dir
}

// TestParseCommits pins the commits param grammar.
func TestParseCommits(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    int
		wantErr string
	}{
		{in: "3", want: 3},
		{in: " 2 ", want: 2},
		{in: "0", wantErr: "fewer than one commit"},
		{in: "-1", wantErr: "fewer than one commit"},
		{in: "abc", wantErr: "not an integer"},
		{in: "999999", wantErr: "bound"},
	} {
		n, err := parseCommits(tc.in)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("parseCommits(%q) = %d, %v (want error naming %q)", tc.in, n, err, tc.wantErr)
			}
			continue
		}
		if err != nil || n != tc.want {
			t.Fatalf("parseCommits(%q) = %d, %v (want %d)", tc.in, n, err, tc.want)
		}
	}
}
