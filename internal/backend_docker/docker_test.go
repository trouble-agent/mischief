package backend_docker

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// fakeCLI builds a CLI over an in-memory container table: the fake runner
// mutates state exactly as the real docker verbs would (pause/unpause,
// kill/start, update, rm), so the landed-proof read-backs and the no-op
// arms are exercised through the SAME inspect path the live host uses.
//
// Windows are shortened for the whole suite: a fake host proves its point
// in milliseconds; a no_op arm must grade fast, not after a real 90s wait.
func init() {
	proofWindow = 2 * time.Second
	proofInterval = 10 * time.Millisecond
	oomKillWindow = 2 * time.Second
	startupWindow = 2 * time.Second
}

type fakeState struct {
	Name      string
	Running   bool
	Paused    bool
	OOMKilled bool
	ExitCode  int
	Memory    int64
	MemSwap   int64
	Restarts  int
}

type fakeHost struct {
	containers map[string]*fakeState
	commands   []string
}

func newFakeHost() *fakeHost { return &fakeHost{containers: map[string]*fakeState{}} }

func (h *fakeHost) add(name string, mem int64) {
	h.containers[name] = &fakeState{Name: "/" + name, Running: true, ExitCode: 0, Memory: mem, MemSwap: mem}
}

func (h *fakeHost) runner() Runner {
	return func(args ...string) (string, string, error) {
		h.commands = append(h.commands, strings.Join(args, " "))
		if len(args) == 0 {
			return "", "", errors.New("no args")
		}
		switch args[0] {
		case "inspect":
			st, ok := h.containers[args[1]]
			if !ok {
				return "", "Error: No such object", fmt.Errorf("exit status 1")
			}
			// the wire shape Inspect parses
			return fmt.Sprintf(`[{"Name":"%s","Image":"sha256:abc","State":{"Running":%t,"Paused":%t,"OOMKilled":%t,"ExitCode":%d,"RestartCount":%d,"StartedAt":"2026-10-05T00:00:00Z"},"HostConfig":{"Memory":%d,"MemorySwap":%d}}]`,
				st.Name, st.Running, st.Paused, st.OOMKilled, st.ExitCode, st.Restarts, st.Memory, st.MemSwap), "", nil
		case "pause":
			st, ok := h.containers[args[1]]
			if !ok {
				return "", "No such object", fmt.Errorf("exit status 1")
			}
			if !st.Running {
				return "", "is not running", fmt.Errorf("exit status 1")
			}
			st.Paused = true
			return args[1] + "\n", "", nil
		case "unpause":
			st, ok := h.containers[args[1]]
			if !ok {
				return "", "No such object", fmt.Errorf("exit status 1")
			}
			st.Paused = false
			return args[1] + "\n", "", nil
		case "kill":
			st, ok := h.containers[args[1]]
			if !ok {
				return "", "No such object", fmt.Errorf("exit status 1")
			}
			if !st.Running {
				return "", "is not running", fmt.Errorf("exit status 1")
			}
			st.Running, st.Paused, st.ExitCode = false, false, 137
			return args[1] + "\n", "", nil
		case "start":
			st, ok := h.containers[args[1]]
			if !ok {
				return "", "No such object", fmt.Errorf("exit status 1")
			}
			st.Running, st.ExitCode, st.OOMKilled, st.Restarts = true, 0, false, st.Restarts+1
			return args[1] + "\n", "", nil
		case "update":
			// update --memory N --memory-swap N name — on the real host the
			// squeezed workload then allocates past the cap and the kernel OOM
			// kill fires; the fake models that outcome (the OOM death is the
			// workload's doing, not the update's, which is why the sabotage
			// test overrides update to exit cleanly instead).
			st, ok := h.containers[args[5]]
			if !ok {
				return "", "No such object", fmt.Errorf("exit status 1")
			}
			fmt.Sscanf(args[2], "%d", &st.Memory)
			st.MemSwap = st.Memory
			st.Running, st.Paused, st.ExitCode, st.OOMKilled = false, false, 137, true
			return args[5] + "\n", "", nil
		case "image":
			// docker image inspect <name>: local-only existence probe
			if len(args) > 1 && args[1] == "inspect" {
				return "[{\"Id\":\"sha256:fake\"}]", "", nil
			}
			return "", "unknown image subcommand", fmt.Errorf("exit status 1")
		case "rm":
			delete(h.containers, args[2])
			return args[2] + "\n", "", nil
		case "version":
			return "29.1.3\n", "", nil
		}
		return "", "unknown command in fake: " + args[0], fmt.Errorf("exit status 1")
	}
}

func fakeCLI() (*CLI, *fakeHost) {
	h := newFakeHost()
	return NewCLIWithRunner(h.runner()), h
}

// ── safety floor ────────────────────────────────────────────────────────────

func TestCheckScratchPrefix(t *testing.T) {
	if err := CheckScratch("mischief-l0-abc"); err != nil {
		t.Fatalf("scratch name refused: %v", err)
	}
	for _, name := range []string{"", "mischief-l0-", "web-prod", "/var/run/dockershim", "MISCHIEF-L0-x", "mischief-l1-x"} {
		if err := CheckScratch(name); err == nil {
			t.Fatalf("CheckScratch(%q) passed, want refusal", name)
		}
	}
}

func TestMutatingVerbsRefuseNonScratch(t *testing.T) {
	c, h := fakeCLI()
	for name, fn := range map[string]func(){
		"pause":  func() { _, _ = c.Pause("prod-web") },
		"kill":   func() { _, _ = c.Kill("prod-web") },
		"oom":    func() { _, _ = c.OOM("prod-web", 32<<20) },
		"remove": func() { _ = c.RemoveScratch("prod-web") },
		"run":    func() { _ = c.RunScratch("prod-web", "alpine", 0, []string{"true"}) },
	} {
		before := len(h.commands)
		fn()
		// the refusal happens BEFORE the runner: nothing new was invoked
		if len(h.commands) != before {
			t.Fatalf("%s: a non-scratch invocation reached docker: %v", name, h.commands[before:])
		}
	}
	// direct: Inspect refuses too (read-back on a non-scratch name is
	// equally outside the contract)
	if _, err := c.Inspect("prod-db"); err == nil {
		t.Fatal("Inspect(non-scratch) passed, want refusal")
	}
}

func TestMintScratchNameShape(t *testing.T) {
	n := MintScratchName()
	if !scratchNameRe.MatchString(n) {
		t.Fatalf("minted %q does not match the pinned shape", n)
	}
	if n == MintScratchName() {
		t.Fatal("two mints collided (nanos must differ)")
	}
}

// ── landed proofs (fake host: same inspect path, no daemon) ────────────────

func TestPauseLandsAndReverts(t *testing.T) {
	c, h := fakeCLI()
	h.add("mischief-l0-p1", 64<<20)
	out, f := c.Pause("mischief-l0-p1")
	if !out.OK() {
		t.Fatalf("pause did not land: %+v", out)
	}
	if !strings.Contains(out.LandedProof, "Paused=true") {
		t.Fatalf("landed proof lacks the measured state: %q", out.LandedProof)
	}
	if f == nil || f.Kind != KindPause {
		t.Fatalf("armed fault missing or wrong kind: %+v", f)
	}
	rev, err := f.Revert()
	if err != nil || !rev.OK() {
		t.Fatalf("revert: outcome=%+v err=%v", rev, err)
	}
	st, _ := c.Inspect("mischief-l0-p1")
	if st.Paused || !st.Running {
		t.Fatalf("post-revert state drifted: %+v", st)
	}
}

func TestPauseNoOpArms(t *testing.T) {
	c, h := fakeCLI()
	h.add("mischief-l0-p2", 64<<20)
	// already-paused target: the receipt would succeed but the landing is
	// meaningless — the backend grades no_op BEFORE arming
	_, _, _ = h.runner()("pause", "mischief-l0-p2")
	out, f := c.Pause("mischief-l0-p2")
	if out.OK() || f != nil {
		t.Fatalf("pause of an already-paused container graded landed: %+v", out)
	}
	if !strings.Contains(out.NoOpReason, "already paused") {
		t.Fatalf("no_op reason must name the shape: %q", out.NoOpReason)
	}
	// stopped target (must unpause first, else the already-paused gate
	// fires before the running gate)
	if _, _, err := h.runner()("unpause", "mischief-l0-p2"); err != nil {
		t.Fatal(err)
	}
	h.containers["mischief-l0-p2"].Running = false
	out, _ = c.Pause("mischief-l0-p2")
	if out.OK() || !strings.Contains(out.NoOpReason, "not running") {
		t.Fatalf("pause of a stopped container must no_op naming it: %+v", out)
	}
}

func TestKillLands137AndReverts(t *testing.T) {
	c, h := fakeCLI()
	h.add("mischief-l0-k1", 64<<20)
	out, f := c.Kill("mischief-l0-k1")
	if !out.OK() {
		t.Fatalf("kill did not land: %+v", out)
	}
	if !strings.Contains(out.LandedProof, "137") {
		t.Fatalf("kill proof must carry exit 137 (P-008): %q", out.LandedProof)
	}
	rev, err := f.Revert()
	if err != nil || !rev.OK() {
		t.Fatalf("revert: outcome=%+v err=%v", rev, err)
	}
	st, _ := c.Inspect("mischief-l0-k1")
	if !st.Running || st.ExitCode != 0 {
		t.Fatalf("post-revert state drifted: %+v", st)
	}
}

func TestKillRefusesOOMMisattribution(t *testing.T) {
	_, h := fakeCLI()
	h.add("mischief-l0-k2", 64<<20)
	// a container that dies by OOM during the kill window: the observed
	// exit is NOT this fault's landing
	h.containers["mischief-l0-k2"].OOMKilled = true
	// simulate the race: kill receipt ok but OOM flag observed — poison the
	// fake so kill sets OOM instead of plain 137
	h.commands = nil
	orig := h.runner()
	c2 := NewCLIWithRunner(func(args ...string) (string, string, error) {
		if len(args) > 0 && args[0] == "kill" {
			st := h.containers[args[1]]
			st.Running, st.ExitCode, st.OOMKilled = false, 137, true
			return args[1], "", nil
		}
		return orig(args...)
	})
	out, f := c2.Kill("mischief-l0-k2")
	if out.OK() || f != nil {
		t.Fatalf("OOM-during-kill graded landed: %+v", out)
	}
	if !strings.Contains(out.NoOpReason, "OOM") {
		t.Fatalf("no_op must name the misattribution: %q", out.NoOpReason)
	}
}

func TestKillNoOpOnStoppedContainer(t *testing.T) {
	c, h := fakeCLI()
	h.add("mischief-l0-k3", 64<<20)
	h.containers["mischief-l0-k3"].Running = false
	out, f := c.Kill("mischief-l0-k3")
	if out.OK() || f != nil || !strings.Contains(out.NoOpReason, "not running") {
		t.Fatalf("kill of a stopped container must no_op: %+v", out)
	}
}

func TestOOMLandsAndReverts(t *testing.T) {
	c, h := fakeCLI()
	h.add("mischief-l0-o1", 64<<20)
	out, f := c.OOM("mischief-l0-o1", 16<<20)
	if !out.OK() {
		t.Fatalf("oom did not land: %+v", out)
	}
	if !strings.Contains(out.LandedProof, "OOMKilled=true") {
		t.Fatalf("oom proof must carry the OOM observation: %q", out.LandedProof)
	}
	rev, err := f.Revert()
	if err != nil || !rev.OK() {
		t.Fatalf("revert: outcome=%+v err=%v", rev, err)
	}
	st, _ := c.Inspect("mischief-l0-o1")
	if !st.Running || st.MemoryBytes != 64<<20 {
		t.Fatalf("post-revert limit not restored: %+v", st)
	}
}

func TestOOMRefusesSubFloorCapAndTightCap(t *testing.T) {
	c, h := fakeCLI()
	h.add("mischief-l0-o2", 64<<20)
	out, f := c.OOM("mischief-l0-o2", 1<<20)
	if out.OK() || f != nil {
		t.Fatalf("sub-floor cap graded landed: %+v", out)
	}
	// cap at/above the current limit: the squeeze would land nothing
	out, f = c.OOM("mischief-l0-o2", 64<<20)
	if out.OK() || f != nil || !strings.Contains(out.NoOpReason, "already") {
		t.Fatalf("no-op squeeze must refuse: %+v", out)
	}
}

func TestOOMNoOpWhenContainerExitsButNotByOOM(t *testing.T) {
	h := newFakeHost()
	h.add("mischief-l0-o3", 64<<20)
	// workload exits cleanly after the squeeze instead of OOMing: the
	// observed exit is not this fault's landing (the half-2 anti-gaming arm)
	orig := h.runner()
	c2 := NewCLIWithRunner(func(args ...string) (string, string, error) {
		out, errOut, err := orig(args...)
		if len(args) > 0 && args[0] == "update" {
			st := h.containers[args[5]]
			st.Memory, st.MemSwap = 16<<20, 16<<20
			st.Running, st.ExitCode, st.OOMKilled = false, 0, false
		}
		return out, errOut, err
	})
	out, f := c2.OOM("mischief-l0-o3", 16<<20)
	if out.OK() || f != nil {
		t.Fatalf("clean exit under squeeze graded landed: %+v", out)
	}
	if !strings.Contains(out.NoOpReason, "NOT by OOM") {
		t.Fatalf("no_op must name the misattribution: %q", out.NoOpReason)
	}
}

func TestProbeAndImagePresent(t *testing.T) {
	c, _ := fakeCLI()
	v, err := c.Probe()
	if err != nil || v != "29.1.3" {
		t.Fatalf("probe: %q %v", v, err)
	}
	if !c.ImagePresent("alpine:latest") {
		t.Fatal("fake image inspect should succeed for any name")
	}
	// a dead runner fails closed
	dead := NewCLIWithRunner(func(args ...string) (string, string, error) {
		return "", "cannot connect", errors.New("exit status 1")
	})
	if _, err := dead.Probe(); err == nil {
		t.Fatal("probe on a dead daemon must refuse")
	}
}

func TestSnapshotExcludesVolatileFields(t *testing.T) {
	c, h := fakeCLI()
	h.add("mischief-l0-s1", 64<<20)
	pre, err := c.Snapshot("mischief-l0-s1")
	if err != nil {
		t.Fatal(err)
	}
	h.containers["mischief-l0-s1"].Restarts = 7
	post, _ := c.Snapshot("mischief-l0-s1")
	if string(pre) != string(post) {
		t.Fatalf("snapshot is not stable across restart-count changes:\npre=%s\npost=%s", pre, post)
	}
}

// ── write-ahead journal ─────────────────────────────────────────────────────

func TestWriteAheadArmAndLand(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir + "/revo.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := DeclOf(KindPause, "mischief-l0-j1", 0)
	chk := CheckDecl{Kind: "docker-unpaused", Params: map[string]string{"container": "mischief-l0-j1"}}
	if err := j.Arm("docker:pause", "mischief-l0-j1", inv, chk); err != nil {
		t.Fatal(err)
	}
	ok, err := j.WriteAheadOK("docker:pause", "mischief-l0-j1")
	if err != nil || !ok {
		t.Fatalf("write-ahead not durable after arm: ok=%t err=%v", ok, err)
	}
	if err := j.Land("docker:pause", "mischief-l0-j1"); err != nil {
		t.Fatalf("land after arm must succeed: %v", err)
	}
	// a DIFFERENT hold has no write-ahead line
	if err := j.Land("docker:kill", "mischief-l0-j1"); err == nil {
		t.Fatal("land without a durable arm must refuse")
	}
	if ok, _ := j.WriteAheadOK("docker:kill", "mischief-l0-j1"); ok {
		t.Fatal("docker:kill has no arm line but WriteAheadOK says true")
	}
}

func TestDeclOfVocabulary(t *testing.T) {
	inv, err := DeclOf(KindPause, "mischief-l0-d1", 0)
	if err != nil || inv.Kind != "docker-unpause" {
		t.Fatalf("pause inverse: %+v %v", inv, err)
	}
	inv, _ = DeclOf(KindKill, "mischief-l0-d1", 0)
	if inv.Kind != "docker-start" {
		t.Fatalf("kill inverse: %+v", inv)
	}
	inv, _ = DeclOf(KindOOM, "mischief-l0-d1", 64<<20)
	if inv.Kind != "docker-memory-restore" || inv.Params["memory"] != "67108864" {
		t.Fatalf("oom inverse: %+v", inv)
	}
	if _, err := DeclOf(Kind("bogus"), "mischief-l0-d1", 0); err == nil {
		t.Fatal("unknown kind must refuse to derive an inverse")
	}
}

func TestExecuteInverseUnknownKindFailsClosed(t *testing.T) {
	c, _ := fakeCLI()
	if _, err := c.ExecuteInverse(InverseDecl{Kind: "rm-rf-everything",
		Params: map[string]string{"container": "mischief-l0-x1"}}); err == nil {
		t.Fatal("unknown inverse kind must fail closed")
	}
	if _, _, err := c.EvalCheck(CheckDecl{Kind: "unknown-check",
		Params: map[string]string{"container": "mischief-l0-x1"}}); err == nil {
		t.Fatal("unknown check kind must fail closed")
	}
}

func TestExecInverseFromJournalRevertsLandedHold(t *testing.T) {
	c, h := fakeCLI()
	h.add("mischief-l0-jr1", 64<<20)
	j, err := OpenJournal(t.TempDir() + "/j.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := DeclOf(KindKill, "mischief-l0-jr1", 0)
	chk := CheckDecl{Kind: "docker-running", Params: map[string]string{"container": "mischief-l0-jr1"}}
	if err := j.Arm("docker:kill", "mischief-l0-jr1", inv, chk); err != nil {
		t.Fatal(err)
	}
	if err := j.Land("docker:kill", "mischief-l0-jr1"); err != nil {
		t.Fatal(err)
	}
	out, _ := c.Kill("mischief-l0-jr1")
	if !out.OK() {
		t.Fatalf("kill: %+v", out)
	}
	proofs, err := ExecInverseFromJournal(c, j.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(proofs) != 1 || !strings.Contains(proofs[0], "reverted") {
		t.Fatalf("want one reverted proof, got %v", proofs)
	}
	recs, _ := j.Records()
	last := recs[len(recs)-1]
	if last.Type != RecordRevert || last.Outcome != "reverted" {
		t.Fatalf("journal tail: %+v", last)
	}
	// a second fold is idempotent: the hold is terminal after its revert
	// proof
	recs2, _ := j.Records()
	revertCount := 0
	for _, r := range recs2 {
		if r.Type == RecordRevert {
			revertCount++
		}
	}
	proofs2, err := ExecInverseFromJournal(c, j.Path())
	if err != nil || len(proofs2) != 0 {
		t.Fatalf("second fold must be empty: %v %v", proofs2, err)
	}
	recs3, _ := j.Records()
	count3 := 0
	for _, r := range recs3 {
		if r.Type == RecordRevert {
			count3++
		}
	}
	if count3 != revertCount {
		t.Fatalf("second fold appended new revert records (%d -> %d)", revertCount, count3)
	}
}

func TestExecInverseFromJournalRecordsFailedRevert(t *testing.T) {
	c, h := fakeCLI()
	h.add("mischief-l0-jr2", 64<<20)
	j, err := OpenJournal(t.TempDir() + "/j2.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := DeclOf(KindPause, "mischief-l0-jr2", 0)
	chk := CheckDecl{Kind: "docker-unpaused", Params: map[string]string{"container": "mischief-l0-jr2"}}
	if err := j.Arm("docker:pause", "mischief-l0-jr2", inv, chk); err != nil {
		t.Fatal(err)
	}
	if err := j.Land("docker:pause", "mischief-l0-jr2"); err != nil {
		t.Fatal(err)
	}
	_, _ = c.Pause("mischief-l0-jr2")
	// sabotage the inverse: unpause becomes a no-op that leaves Paused=true
	sab := NewCLIWithRunner(func(args ...string) (string, string, error) {
		if len(args) > 0 && args[0] == "unpause" {
			return args[1], "", nil // receipt ok, state unchanged
		}
		return h.runner()(args...)
	})
	proofs, err := ExecInverseFromJournal(sab, j.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(proofs) != 1 || !strings.Contains(proofs[0], "revert_failed") {
		t.Fatalf("sabotaged inverse must grade revert_failed: %v", proofs)
	}
	recs, _ := j.Records()
	if recs[len(recs)-1].Outcome != "revert_failed" {
		t.Fatalf("journal must carry revert_failed, not reverted: %+v", recs[len(recs)-1])
	}
}

func TestRecordVocabularyClosed(t *testing.T) {
	j, err := OpenJournal(t.TempDir() + "/j3.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	err = j.append(record{Type: RecordType("mystery")})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("unknown record type must refuse: %v", err)
	}
	err = j.RecordRevert("docker:pause", "mischief-l0-j4", "sort-of-reverted", "")
	if err == nil || !strings.Contains(err.Error(), "vocabulary") {
		t.Fatalf("undeclared outcome must refuse: %v", err)
	}
}

func TestDeclOfParamsValidated(t *testing.T) {
	// an OOM decl for an EMPTY container would be a "restore" that touches
	// nothing: refuse at derivation
	if _, err := DeclOf(KindOOM, "", 64<<20); err == nil {
		t.Fatal("empty container must refuse")
	}
	// and the derived decls must all pass InverseDecl.Validate (empty
	// values are the other touch-nothing shape)
	for _, tc := range []struct {
		kind Kind
		mem  int64
	}{{KindPause, 0}, {KindKill, 0}, {KindOOM, 64 << 20}} {
		inv, err := DeclOf(tc.kind, "mischief-l0-dv1", tc.mem)
		if err != nil {
			t.Fatalf("%s: %v", tc.kind, err)
		}
		if err := inv.Validate(); err != nil {
			t.Fatalf("%s: derived decl invalid: %v", tc.kind, err)
		}
	}
}

var _ = regexp.MustCompile // keep the import list stable for the shape test
