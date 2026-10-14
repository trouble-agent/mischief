package selftest

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/backend_docker"
)

// docker_test.go — INT-CI-6: the selftest land path must grade the known
// GitHub-runner docker:oom flake class (the cgroup-update refusal after the
// backend's own retry, and the exit=137/OOMKilled=false timing race) as an
// honest capability skip — never state=fail — while a genuine OOM landing
// or a land/revert correctness failure keeps its own grading.
//
// The classification itself lives in ONE shared classifier
// (backend_docker.OOMRunnerFlake); the go-test surface
// (internal/backend_docker/live_test.go) and this harness's land path
// consume the SAME function, so both surfaces behave identically on the
// flake class. These tests pin that wiring.

// ── the runner's mid-loop skip shape ────────────────────────────────────────

// TestMidLoopLandSkipGradesSkipNeverFail: a land() that reports the flake
// class records exactly like a capability()-gate skip — AC-11 shape, never
// a pass, never a failure.
func TestMidLoopLandSkipGradesSkipNeverFail(t *testing.T) {
	l := &stubLandable{snap: []byte("pre")}
	l.landOut = skipOut("runner cgroup timing: container mischief-l0-x exited code=137 but OOMKilled=false")
	r := runnerWith(l)
	res := r.RunOne("Z-001")
	if !res.IsSkip() {
		t.Fatalf("mid-loop flake graded %q (%s), want skip", res.State, res.Reason)
	}
	if !strings.HasPrefix(res.Reason, "capability_unavailable: ") ||
		!strings.Contains(res.Reason, "OOMKilled=false") {
		t.Fatalf("skip must carry the AC-11 prefix and name the observed class: %q", res.Reason)
	}
	if res.LandProof != "" {
		t.Fatalf("a skipped run must not carry a land proof: %q", res.LandProof)
	}
	if !l.cleanedUp {
		t.Fatal("cleanup must run on a mid-loop skip")
	}
}

// TestMidLoopInverseSkipGradesSkipNeverFail: the inverse path can hit the
// same class (the OOM restore leg runs `docker update` against the same
// cgroup driver) — graded skip, with the measured land proof kept (it did
// land) and the skip reason carried.
func TestMidLoopInverseSkipGradesSkipNeverFail(t *testing.T) {
	l := &stubLandable{snap: []byte("pre")}
	l.landOut = ok("land measured")
	l.invOut = skipOut("docker update --memory refused by this host's cgroup driver during the OOM restore (runc/cgroup v2): openat2: permission denied")
	r := runnerWith(l)
	res := r.RunOne("Z-001")
	if !res.IsSkip() {
		t.Fatalf("mid-loop inverse flake graded %q (%s), want skip", res.State, res.Reason)
	}
	if !strings.Contains(res.Reason, "capability_unavailable") ||
		!strings.Contains(res.Reason, "OOM restore") {
		t.Fatalf("inverse skip must name the class: %q", res.Reason)
	}
}

// TestMidLoopSkipReasonIsTheClassifierVerdict: the reason a mid-loop skip
// carries is the SHARED classifier's verdict — the same function the
// go-test surface consults. Feed the fabricated exit=137/OOMKilled=false
// observation to the classifier, hand its reason to the harness, and the
// recorded result must embed that identical text (criterion: both surfaces
// behave identically on the flake class).
func TestMidLoopSkipReasonIsTheClassifierVerdict(t *testing.T) {
	race := backend_docker.Outcome{Primitive: "docker:oom", NoOpReason: "container mischief-l0-x exited (code=137) but NOT by OOM (OOMKilled=false)"}
	st := &backend_docker.InspectState{Name: "mischief-l0-x", Running: false, ExitCode: 137, OOMKilled: false}
	reason, flake := backend_docker.OOMRunnerFlake(race, st)
	if !flake {
		t.Fatal("precondition: the fabricated observation must classify as the flake class")
	}
	l := &stubLandable{snap: []byte("pre")}
	l.landOut = skipOut("%s", reason)
	res := runnerWith(l).RunOne("Z-001")
	if !res.IsSkip() || !strings.Contains(res.Reason, reason) {
		t.Fatalf("recorded skip lost the classifier's verdict: state=%q reason=%q want %q", res.State, res.Reason, reason)
	}
}

// ── the docker landable's land/inverse wiring (fake docker runner) ──────────

// fakeDocker models the scratch-container verbs the dockerLandable's
// land/inverse path drives, with a scriptable `update` behavior. The inspect
// wire shape is the same JSON subset backend_docker.Inspect parses.
type fakeDocker struct {
	name      string
	running   bool
	paused    bool
	oom       bool
	exit      int
	memory    int64
	updateErr string // when set, `update` refuses with this stderr
	updates   int
	commands  []string
}

func newFakeDocker(name string, mem int64) *fakeDocker {
	return &fakeDocker{name: name, running: true, exit: 0, memory: mem}
}

func (f *fakeDocker) runner() backend_docker.Runner {
	return func(args ...string) (string, string, error) {
		f.commands = append(f.commands, strings.Join(args, " "))
		if len(args) == 0 {
			return "", "", errors.New("no args")
		}
		switch args[0] {
		case "inspect":
			if f.name == "" {
				return "", "Error: No such object", errors.New("exit status 1")
			}
			return fmt.Sprintf(`[{"Name":"/%s","Image":"sha256:fake","State":{"Running":%t,"Paused":%t,"OOMKilled":%t,"ExitCode":%d,"RestartCount":0,"StartedAt":"2026-10-14T00:00:00Z"},"HostConfig":{"Memory":%d,"MemorySwap":%d}}]`,
				f.name, f.running, f.paused, f.oom, f.exit, f.memory, f.memory), "", nil
		case "update":
			f.updates++
			if f.updateErr != "" {
				return "", f.updateErr, errors.New("exit status 1")
			}
			fmt.Sscanf(args[2], "%d", &f.memory)
			// the squeezed workload allocates past the cap and the kernel
			// OOM-kills it (the genuine shape; the timing-race variant is
			// covered at the classifier level — the anti-gaming poll owns
			// the production 90s window)
			f.running, f.paused, f.exit, f.oom = false, false, 137, true
			return f.name + "\n", "", nil
		case "start":
			f.running, f.exit, f.oom = true, 0, false
			return f.name + "\n", "", nil
		case "rm":
			f.name = ""
			return args[2] + "\n", "", nil
		}
		return "", "unknown command in fake: " + args[0], errors.New("exit status 1")
	}
}

func (f *fakeDocker) cli() *backend_docker.CLI {
	return backend_docker.NewCLIWithRunner(f.runner())
}

// oomLandable builds the docker:oom landable over the fake, past the
// capability gate (the gate's live probes are the live tests' business).
func oomLandable(f *fakeDocker) *dockerLandable {
	return &dockerLandable{
		id:        "docker:oom",
		cli:       f.cli(),
		image:     "alpine:latest",
		container: f.name,
		preMem:    f.memory,
	}
}

// TestDockerLandSkipOnCgroupRefusal: the land path's update refused with
// the runc openat2 cgroup.controllers error (post-retry) — the landable
// must return the mid-loop SKIP carrying the classifier's verdict, not a
// failure.
func TestDockerLandSkipOnCgroupRefusal(t *testing.T) {
	f := newFakeDocker("mischief-l0-ci6a", 64<<20)
	f.updateErr = "OCI runtime create failed: runc did not terminate successfully: exit status 1: openat2 /sys/fs/cgroup/mischief-l0-ci6a/cgroup.controllers: no such file or directory: unknown"
	out := oomLandable(f).land()
	if !out.skipped || out.landed {
		t.Fatalf("cgroup refusal graded landed=%t skipped=%t (%s), want the mid-loop skip", out.landed, out.skipped, out.err)
	}
	if !strings.Contains(out.err, "cgroup driver") || !strings.Contains(out.err, "cgroup.controllers") {
		t.Fatalf("skip reason must be the classifier's verdict naming the observed error: %q", out.err)
	}
	if f.updates != 2 {
		t.Fatalf("the backend's own single retry must have fired first: %d update attempts", f.updates)
	}
}

// TestDockerLandFailsOnRealRefusal: a refusal OUTSIDE the flake class (the
// memory-cgroup guard) keeps its failure grading — no over-skip.
func TestDockerLandFailsOnRealRefusal(t *testing.T) {
	f := newFakeDocker("mischief-l0-ci6b", 64<<20)
	f.updateErr = "Cannot update container mischief-l0-ci6b: memory cgroup is not enabled"
	out := oomLandable(f).land()
	if out.skipped || out.landed {
		t.Fatalf("a real refusal graded skipped=%t — over-skip", out.skipped)
	}
	if !strings.Contains(out.err, "docker land:") || !strings.Contains(out.err, "memory cgroup is not enabled") {
		t.Fatalf("failure must keep its own grading and name the refusal: %q", out.err)
	}
	if f.updates != 1 {
		t.Fatalf("real refusals fail fast: %d update attempts, want 1", f.updates)
	}
}

// TestDockerGenuineOOMStillLandsAndReverts: a genuine OOM (exit 137 WITH
// OOMKilled=true) lands, reverts, and grades ok at every step — the
// classification must never swallow the real landing (no over-skip).
func TestDockerGenuineOOMStillLandsAndReverts(t *testing.T) {
	f := newFakeDocker("mischief-l0-ci6c", 64<<20)
	l := oomLandable(f)
	land := l.land()
	if !land.landed || land.skipped {
		t.Fatalf("genuine OOM did not land: landed=%t skipped=%t (%s)", land.landed, land.skipped, land.err)
	}
	if !strings.Contains(land.proof, "OOMKilled=true") {
		t.Fatalf("land proof lost the OOM read-back: %q", land.proof)
	}
	inv := l.inverse()
	if !inv.landed || inv.skipped {
		t.Fatalf("genuine OOM revert graded landed=%t skipped=%t (%s)", inv.landed, inv.skipped, inv.err)
	}
	if !strings.Contains(inv.proof, fmt.Sprintf("Memory=%d", 64<<20)) {
		t.Fatalf("revert proof lost the memory restore: %q", inv.proof)
	}
}

// TestDockerInverseSkipOnRestoreRefusal: the OOM inverse's restore leg runs
// `docker update` against the same cgroup driver — a refusal there is the
// same flake class and must grade the mid-loop skip, not a revert failure.
func TestDockerInverseSkipOnRestoreRefusal(t *testing.T) {
	f := newFakeDocker("mischief-l0-ci6d", 64<<20)
	l := oomLandable(f)
	if land := l.land(); !land.landed {
		t.Fatalf("precondition: land must succeed: %s", land.err)
	}
	// the restore-leg refusal (the land path's update succeeded, so this is
	// the SECOND update call hitting the runc refusal)
	f.updates = 0
	f.updateErr = "OCI runtime create failed: runc did not terminate successfully: exit status 1: openat2 /sys/fs/cgroup/mischief-l0-ci6d/cgroup.controllers: no such file or directory: unknown"
	inv := l.inverse()
	if !inv.skipped || inv.landed {
		t.Fatalf("restore refusal graded landed=%t skipped=%t (%s), want the mid-loop skip", inv.landed, inv.skipped, inv.err)
	}
	if !strings.Contains(inv.err, "OOM restore") || !strings.Contains(inv.err, "cgroup.controllers") {
		t.Fatalf("inverse skip must name the class and the observed error: %q", inv.err)
	}
}

// TestDockerInverseFailsOnRealRestoreRefusal: a restore-leg refusal
// OUTSIDE the flake class keeps its revert-failure grading — the OOM-scoped
// classification must never swallow a real daemon refusal (no over-skip).
func TestDockerInverseFailsOnRealRestoreRefusal(t *testing.T) {
	f := newFakeDocker("mischief-l0-ci6e", 64<<20)
	l := oomLandable(f)
	if land := l.land(); !land.landed {
		t.Fatalf("precondition: land must succeed: %s", land.err)
	}
	// the restore leg hits a refusal OUTSIDE the flake class: real refusals
	// ("Cannot update container …") return immediately from Revert (no poll
	// — the production proof windows run in this package's binary) and must
	// keep the revert-failure grading, never a skip
	f.updateErr = "Cannot update container " + f.name + ": memory cgroup is not enabled"
	inv := l.inverse()
	if inv.skipped || inv.landed {
		t.Fatalf("a real restore refusal graded skipped=%t — over-skip", inv.skipped)
	}
	if !strings.Contains(inv.err, "docker revert") || !strings.Contains(inv.err, "memory cgroup is not enabled") {
		t.Fatalf("failure must keep its own grading: %q", inv.err)
	}
}

// TestDockerPauseKillUnaffected: the pause/kill arms keep their plain
// failure grading (the classifier is oom-scoped — a pause refusal must not
// consult the oom classifier at all).
func TestDockerPauseKillUnaffected(t *testing.T) {
	f := newFakeDocker("mischief-l0-ci6f", 0)
	l := &dockerLandable{id: "docker:pause", cli: f.cli(), image: "alpine:latest", container: f.name}
	// a dead container: pause's pre-check refuses (not running)
	f.running = false
	f.exit = 137
	out := l.land()
	if out.skipped || out.landed {
		t.Fatalf("pause graded skipped=%t — the classifier must be oom-scoped", out.skipped)
	}
	if !strings.Contains(out.err, "docker land:") {
		t.Fatalf("pause failure lost its own grading: %q", out.err)
	}
}
