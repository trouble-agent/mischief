package backend_docker

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// live_test.go — the L0 scratch LIVE selftests (AC-19 discipline): each new
// docker primitive must prove land + revert on an L0 scratch container THIS
// TEST created, or record the honest skip with the missing piece named.
// The daemon here is real (docker 29.x, cgroup v2 on this host); the
// container is mischief-l0- prefixed, created and removed by the test.
//
// The internal/selftest harness consumes these primitives through
// buildLandable; these live tests exercise the SAME land+revert pairs at
// the backend layer (the harness adds the pre/post byte-compare).

// liveCapable skips with the named missing piece when this host cannot run
// docker scratch faults (no binary, dead daemon, no local image).
func liveCapable(t *testing.T) (*CLI, string) {
	t.Helper()
	c, err := NewCLI()
	if err != nil {
		t.Skipf("capability_unavailable: %v", err)
	}
	if _, err := c.Probe(); err != nil {
		t.Skipf("capability_unavailable: %v", err)
	}
	return c, dockerSelftestImage
}

// dockerSelftestImage is the LOCAL image the scratch containers run (never
// pulled; ImagePresent gates). Alpine is the smallest common denominator.
const dockerSelftestImage = "alpine:latest"

// oomAllocCmd is the scratch workload for the OOM selftest: allocates 32
// MiB — OVER the 16 MiB squeeze cap (so the kernel OOM-kills it) but UNDER
// the 64 MiB restored cap (so the reverted container runs stably and the
// post-revert snapshot is deterministic, not a second OOM race).
var oomAllocCmd = []string{"sh", "-c", "dd if=/dev/zero of=/dev/null bs=32M"}

// runScratchForTest creates a unique scratch container running sleep (or
// cmd) and registers its cleanup. Memory bytes 0 = daemon default.
func runScratchForTest(t *testing.T, c *CLI, memoryBytes int64, cmd []string) string {
	t.Helper()
	name := MintScratchName()
	if err := c.RunScratch(name, dockerSelftestImage, memoryBytes, cmd); err != nil {
		t.Fatalf("scratch container: %v", err)
	}
	t.Cleanup(func() { _ = c.RemoveScratch(name) })
	// wait for observable running state (the exec race the signal backend
	// also covers)
	if !waitFor(startupWindow, proofInterval, func() bool {
		st, err := c.Inspect(name)
		return err == nil && st.Running
	}) {
		t.Fatalf("scratch container %s never became running", name)
	}
	return name
}

func TestLivePauseLandAndRevert(t *testing.T) {
	c, image := liveCapable(t)
	if !c.ImagePresent(image) {
		t.Skipf("capability_unavailable: no local image %s (the selftest never pulls)", image)
	}
	name := runScratchForTest(t, c, 0, []string{"sleep", "60"})
	out, f := c.Pause(name)
	if !out.OK() {
		t.Fatalf("pause did not land: %+v", out)
	}
	st, _ := c.Inspect(name)
	if !st.Paused || !st.Running {
		t.Fatalf("landed state wrong: paused=%t running=%t", st.Paused, st.Running)
	}
	rev, err := f.Revert()
	if err != nil || !rev.OK() {
		t.Fatalf("revert: outcome=%+v err=%v", rev, err)
	}
	st, _ = c.Inspect(name)
	if st.Paused || !st.Running {
		t.Fatalf("post-revert state drifted: %+v", st)
	}
}

func TestLiveKillLandAndRevert(t *testing.T) {
	c, image := liveCapable(t)
	if !c.ImagePresent(image) {
		t.Skipf("capability_unavailable: no local image %s (the selftest never pulls)", image)
	}
	name := runScratchForTest(t, c, 0, []string{"sleep", "60"})
	out, f := c.Kill(name)
	if !out.OK() {
		t.Fatalf("kill did not land: %+v", out)
	}
	st, _ := c.Inspect(name)
	if st.Running || st.ExitCode != 137 || st.OOMKilled {
		t.Fatalf("landed state wrong: running=%t exit=%d oom=%t", st.Running, st.ExitCode, st.OOMKilled)
	}
	rev, err := f.Revert()
	if err != nil || !rev.OK() {
		t.Fatalf("revert: outcome=%+v err=%v", rev, err)
	}
	st, _ = c.Inspect(name)
	if !st.Running {
		t.Fatalf("post-revert state drifted: %+v", st)
	}
}

func TestLiveOOMLandAndRevert(t *testing.T) {
	c, image := liveCapable(t)
	if !c.ImagePresent(image) {
		t.Skipf("capability_unavailable: no local image %s (the selftest never pulls)", image)
	}
	// 64 MiB initial cap, squeeze to 16 MiB, allocating workload dies
	name := runScratchForTest(t, c, 64<<20, oomAllocCmd)
	out, f := c.OOM(name, 16<<20)
	if !out.OK() {
		t.Fatalf("oom did not land: %+v", out)
	}
	st, _ := c.Inspect(name)
	if st.Running || !st.OOMKilled {
		t.Fatalf("landed state wrong: running=%t oom=%t", st.Running, st.OOMKilled)
	}
	rev, err := f.Revert()
	if err != nil || !rev.OK() {
		t.Fatalf("revert: outcome=%+v err=%v", rev, err)
	}
	st, _ = c.Inspect(name)
	if !st.Running || st.MemoryBytes != 64<<20 {
		t.Fatalf("post-revert state drifted: running=%t memory=%d", st.Running, st.MemoryBytes)
	}
}

// TestLiveJournalExecutedInverseFromDetachedShape: the journal-path inverse
// (ExecInverseFromJournal) reverts a REAL landed fault with only journal
// bytes and a fresh CLI — the AC-5 detached-executor shape.
func TestLiveJournalExecutedInverseFromDetachedShape(t *testing.T) {
	c, image := liveCapable(t)
	if !c.ImagePresent(image) {
		t.Skipf("capability_unavailable: no local image %s (the selftest never pulls)", image)
	}
	name := runScratchForTest(t, c, 0, []string{"sleep", "60"})
	j, err := OpenJournal(t.TempDir() + "/live.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	inv, err := DeclOf(KindPause, name, 0)
	if err != nil {
		t.Fatal(err)
	}
	chk := CheckDecl{Kind: "docker-unpaused", Params: map[string]string{"container": name}}
	if err := j.Arm("docker:pause", name, inv, chk); err != nil {
		t.Fatal(err)
	}
	if err := j.Land("docker:pause", name); err != nil {
		t.Fatal(err)
	}
	out, _ := c.Pause(name)
	if !out.OK() {
		t.Fatalf("pause: %+v", out)
	}
	// a FRESH CLI over the same host: only the journal bytes connect them
	fresh, err := NewCLI()
	if err != nil {
		t.Skipf("capability_unavailable: %v", err)
	}
	proofs, err := ExecInverseFromJournal(fresh, j.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(proofs) != 1 || !strings.Contains(proofs[0], "reverted") {
		t.Fatalf("detached inverse: %v", proofs)
	}
	st, _ := c.Inspect(name)
	if st.Paused || !st.Running {
		t.Fatalf("post-revert state drifted: %+v", st)
	}
}

// TestLivePrefixedNamespacesClean: the suite leaks no scratch containers —
// the cleanup contract the L0 discipline demands (best-effort check, runs
// after all cleanups via the t.Cleanup order).
func TestLiveNoScratchLeak(t *testing.T) {
	c, err := NewCLI()
	if err != nil {
		t.Skipf("capability_unavailable: %v", err)
	}
	out, errOut, err := c.run("ps", "-a", "--format", "{{.Names}}")
	if err != nil {
		t.Skipf("docker ps unavailable: %v: %s", err, out+errOut)
	}
	leaked := []string{}
	for _, n := range strings.Fields(out) {
		if strings.HasPrefix(n, ScratchNamePrefix) {
			leaked = append(leaked, n)
		}
	}
	if len(leaked) > 0 {
		t.Errorf("scratch containers leaked past cleanup: %v", leaked)
	}
}

// ensure os/exec stay referenced even if the live gates change (the CLI
// seam is the only process surface)
var (
	_ = os.Getenv
	_ = exec.Command
	_ = time.Now
)
