package selftest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/backend_docker"
	"github.com/trouble-agent/mischief/internal/backend_sim"
)

// docker.go — the SPEC-09 landables: the docker node faults and the sim
// primitives, driven through the SAME measured loop as the M1 primitives
// (land, measure the landed-proof, inverse, measure the revert,
// byte-compare the pre/post state — the runner owns the loop; a landable
// only answers the five questions).
//
// Ids: the docker faults carry backend-scoped ids ("docker:pause" etc.);
// the sim carries the catalog's I-layer ids (I-003/004/006/010/011/012/
// 013/014 — the rows whose landed_proof column reads "provider-sim request
// log", which is exactly what the sim landable reads back from disk).
//
// Skip discipline (AC-11): docker primitives SKIP with the measured
// missing piece (no binary / dead daemon / no local image — never pulled);
// the sim primitives are unconditionally rootless (loopback listener +
// fsynced log inside the scratch dir) and never skip for capability.

// ── sim landables ───────────────────────────────────────────────────────────

// simShapes maps the covered sim primitive ids onto their armed shape,
// probe path (the fault each catalog row names, at its minimal selftest
// shape) and optional seed (state the shape needs in the sim's store —
// I-006's "404 on a key that EXISTS" needs the key to exist).
var simShapes = map[string]struct {
	shape backend_sim.Shape
	path  string
	seed  func(*backend_sim.Simulator)
}{
	"I-003": {backend_sim.Shape{Kind: backend_sim.ShapeRateLimit}, "/v1/selftest/rate", nil},
	"I-004": {backend_sim.Shape{Kind: backend_sim.ShapeAPI5xx, Status: 503, Burst: 1}, "/v1/selftest/5xx", nil},
	"I-006": {backend_sim.Shape{Kind: backend_sim.ShapeObjectStore, MissingKey: true}, "/obj/selftest/key",
		func(s *backend_sim.Simulator) { s.Put("selftest/key", []byte("the-key-exists\n")) }},
	"I-010": {backend_sim.Shape{Kind: backend_sim.ShapeAuthExpiry, Burst: 1}, "/v1/selftest/auth", nil},
	"I-011": {backend_sim.Shape{Kind: backend_sim.ShapeMetadata, MetadataMode: "status500"}, "/meta/selftest", nil},
	"I-012": {backend_sim.Shape{Kind: backend_sim.ShapeQuota, QuotaResource: "instances"}, "/v1/selftest/quota", nil},
	"I-013": {backend_sim.Shape{Kind: backend_sim.ShapeSnapshotStale, StaleSeconds: 600}, "/v1/selftest/restore", nil},
	// the DNS shape's two-resolver disagreement needs the STALE leg (the
	// resolver-b query); the log records URL.Path, so the proof still keys
	// on /dns/selftest
	"I-014": {backend_sim.Shape{Kind: backend_sim.ShapeDNSPropagation, StaleSeconds: 30},
		"/dns/selftest?resolver=resolver-b", nil},
}

// simCoveredIDs lists the sim primitives this build selftests, sorted.
func simCoveredIDs() []string {
	out := make([]string, 0, len(simShapes))
	for id := range simShapes {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// simLandable is one sim primitive's landable: control request (healthy),
// arm, one real request, prove via the REQUEST LOG read back from disk
// (the catalog's landed-proof channel), then Disarm and prove the healthy
// surface resumed.
type simLandable struct {
	id    string
	shape backend_sim.Shape
	path  string
	seed  func(*backend_sim.Simulator)
	sim   *backend_sim.Simulator
	log   string
	dir   string
	// lastServed is the object-scoped pre/post snapshot: the status + body
	// hash the sim's surface last served (the proxyLandable pattern — the
	// request LOG is append-only by design, so it is the PROOF channel,
	// never the byte-compare basis; the served surface is what the inverse
	// must restore).
	lastServed string
	// controlServed is the CONTROL request's serve record (prepare); the
	// inverse compares its post-disarm serve against THIS, not against the
	// faulted serve land() left behind.
	controlServed string
}

func newSimLandable(id, dir string) *simLandable {
	spec := simShapes[id]
	return &simLandable{id: id, shape: spec.shape, path: spec.path, seed: spec.seed, dir: dir}
}

func (l *simLandable) capability() (string, bool) {
	return "", false // loopback listener + a scratch log file: rootless, always
}

// serveAndRecord issues one request and records the object-scoped
// last-served snapshot ("served:<status>:<body-hash>").
func (l *simLandable) serveAndRecord() error {
	st, body, err := simGetBody(l.sim.URL() + l.path)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	l.lastServed = fmt.Sprintf("served:%d:%s", st, hex.EncodeToString(sum[:8]))
	return nil
}

func (l *simLandable) prepare() ([]byte, error) {
	l.log = joinPath(l.dir, "sim-requests-"+strings.ReplaceAll(strings.ToLower(l.id), "-", "")+".log")
	l.sim = backend_sim.NewSimulator(l.log)
	if err := l.sim.Start(); err != nil {
		return nil, fmt.Errorf("sim start: %w", err)
	}
	if l.seed != nil {
		l.seed(l.sim)
	}
	// the CONTROL request first: healthy surface, logged NOT faulted (the
	// negative arm the landed-proof reads against)
	if err := l.serveAndRecord(); err != nil {
		return nil, fmt.Errorf("control request: %w", err)
	}
	l.controlServed = l.lastServed
	b, err := os.ReadFile(l.log)
	if err != nil {
		return nil, fmt.Errorf("request log after control: %w", err)
	}
	if !strings.Contains(string(b), " healthy ") {
		return nil, fmt.Errorf("control entry not visible in the request log")
	}
	// the pre-state IS the object-scoped served-surface record
	return []byte(l.lastServed), nil
}

func (l *simLandable) land() landOutcome {
	if err := l.sim.Arm(l.shape); err != nil {
		return bad("arm: %v", err)
	}
	pre := l.lastServed
	if err := l.serveAndRecord(); err != nil {
		return bad("faulted request: %v", err)
	}
	if l.lastServed == pre {
		return bad("faulted request served the same (status, body-hash) as the control — the shape did not reach the client")
	}
	// THE LANDED-PROOF: the request log, read back FROM DISK (AC-3 — the
	// catalog's landed_proof column names the request log, not the client).
	entries, err := backend_sim.ReadRequestLog(l.log)
	if err != nil {
		return bad("request log read: %v", err)
	}
	faulted, healthy := 0, 0
	for _, e := range entries {
		if e.Path != l.pathOnly() {
			continue
		}
		if e.Faulted && e.Shape == l.shape.Kind {
			faulted++
		} else if !e.Faulted {
			healthy++
		}
	}
	if faulted < 1 {
		return bad("request log shows no faulted %s entry for %s — the landed-proof did not fire", l.shape.Kind, l.path)
	}
	if healthy < 1 {
		return bad("request log lost the control (healthy) entry — the negative arm is missing")
	}
	return ok(fmt.Sprintf("sim request log: %d faulted %s hit(s) + %d control hit(s) for %s (%s)",
		faulted, l.shape.Kind, healthy, l.pathOnly(), l.log))
}

func (l *simLandable) inverse() landOutcome {
	l.sim.Disarm()
	controlServe := l.controlServed // the healthy serve prepare() recorded
	if err := l.serveAndRecord(); err != nil {
		return bad("post-disarm request: %v", err)
	}
	// the served surface must be BACK to what the CONTROL served (the
	// disarm restored the passthrough — the byte-identity the runner
	// re-verifies through post())
	if l.lastServed != controlServe {
		return bad("post-disarm surface is %s, want the control's healthy serve back (%s)", l.lastServed, controlServe)
	}
	entries, err := backend_sim.ReadRequestLog(l.log)
	if err != nil {
		return bad("post-disarm log read: %v", err)
	}
	// a healthy entry for the path must exist AFTER the faulted one (the
	// disarm proven by the log, not by the client's view)
	lastFaultedIdx, lastHealthyIdx := -1, -1
	for i, e := range entries {
		if e.Path != l.pathOnly() {
			continue
		}
		if e.Faulted {
			lastFaultedIdx = i
		} else {
			lastHealthyIdx = i
		}
	}
	if lastHealthyIdx <= lastFaultedIdx {
		return bad("request log does not show the healthy surface resuming AFTER the fault (faulted@%d healthy@%d)", lastFaultedIdx, lastHealthyIdx)
	}
	return ok(fmt.Sprintf("sim disarm: post-disarm request served the control's healthy body again; the request log records the passthrough resuming after the fault"))
}

// pathOnly is the log's path key (URL path without the query — the log
// records r.URL.Path).
func (l *simLandable) pathOnly() string {
	if i := strings.IndexByte(l.path, '?'); i >= 0 {
		return l.path[:i]
	}
	return l.path
}

func (l *simLandable) post() ([]byte, error) {
	// the byte-compare basis: the served surface (object-scoped), captured
	// by inverse()'s last serveAndRecord
	return []byte(l.lastServed), nil
}

func (l *simLandable) cleanup() {
	if l.sim != nil {
		l.sim.Close()
	}
}

// simGet issues one real GET against the sim (a real socket, a real
// client — not a handler call).
func simGet(url string) (int, error) {
	st, _, err := simGetBody(url)
	return st, err
}

// simGetBody issues one real GET and returns (status, body).
func simGetBody(url string) (int, []byte, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// ── docker landables ────────────────────────────────────────────────────────

// dockerImageName is the local image the docker selftests run (never
// pulled; the capability gate proves it exists first). dockerImageName
// resolves at run time to the FIRST present local image: alpine:latest
// where it exists, else any locally-built/pulled base the host already
// has (the selftest never pulls — the no-network contract stands; a host
// with NEITHER reports the honest skip naming both candidates).
var dockerImageCandidates = []string{"alpine:latest", "busybox:latest", "golang:1.26-alpine", "node:22-alpine", "python:3.11-alpine"}

func resolveDockerImage(cli *backend_docker.CLI) string {
	for _, img := range dockerImageCandidates {
		if cli.ImagePresent(img) {
			return img
		}
	}
	return ""
}

// dockerSelftestIDs are the docker primitives' selftest ids, sorted.
var dockerSelftestIDs = []string{"docker:kill", "docker:oom", "docker:pause"}

// dockerLandable is one docker node fault's landable: create the L0
// scratch container (mischief-l0- prefixed, owned and removed here), arm
// the fault, prove via `docker inspect`, revert, re-prove.
type dockerLandable struct {
	id        string
	cli       *backend_docker.CLI
	image     string
	container string
	fault     *backend_docker.Fault
	preMem    int64
}

func (d *dockerLandable) capability() (string, bool) {
	cli, err := backend_docker.NewCLI()
	if err != nil {
		return err.Error(), true
	}
	if _, err := cli.Probe(); err != nil {
		return err.Error(), true
	}
	img := resolveDockerImage(cli)
	if img == "" {
		return fmt.Sprintf("no local image any of %v (the selftest never pulls; `docker pull %s` or provide one of them)", dockerImageCandidates, dockerImageCandidates[0]), true
	}
	d.image = img
	d.cli = cli
	if d.id == "docker:oom" {
		// The OOM primitive's land path runs `docker update --memory`,
		// which some hosts' cgroup drivers refuse outright (runc cannot
		// open the container's cgroup.scope — GitHub-hosted runners with
		// cgroup namespaces, among others). Probe the update path ONCE
		// with a throwaway scratch so the primitive reports an honest
		// capability skip instead of a red fail on such hosts. The probe
		// scratch is removed here; the real selftest creates its own.
		probe := backend_docker.MintScratchName()
		if err := cli.RunScratch(probe, img, 0, []string{"true"}); err != nil {
			return fmt.Sprintf("docker update probe: scratch container: %v", err), true
		}
		_ = cli.RemoveScratch(probe)
		probe2 := backend_docker.MintScratchName()
		if err := cli.RunScratch(probe2, img, 0, []string{"sleep", "5"}); err != nil {
			return fmt.Sprintf("docker update probe: scratch container: %v", err), true
		}
		defer func() { _ = cli.RemoveScratch(probe2) }()
		if err := cli.UpdateScratchMemory(probe2, 32<<20); err != nil {
			if backend_docker.CgroupUpdateUnavailable(err) {
				return fmt.Sprintf("docker update --memory refused by this host's cgroup driver (runc/cgroup v2): %v", err), true
			}
			return fmt.Sprintf("docker update probe: %v", err), true
		}
	}
	return "", false
}

func (d *dockerLandable) prepare() ([]byte, error) {
	d.container = backend_docker.MintScratchName()
	mem := int64(0)
	cmd := []string{"sleep", "90"}
	if d.id == "docker:oom" {
		// the OOM scratch starts under a 64 MiB cap with a workload that
		// allocates 32 MiB (over the 16 MiB squeeze, under the 64 MiB
		// restore — the post-revert state must be stable, not a second OOM)
		mem = 64 << 20
		cmd = []string{"sh", "-c", "dd if=/dev/zero of=/dev/null bs=32M"}
		d.preMem = mem
	}
	if err := d.cli.RunScratch(d.container, d.image, mem, cmd); err != nil {
		return nil, fmt.Errorf("scratch container: %w", err)
	}
	// observable-running wait (the exec race every spawn covers)
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, err := d.cli.Inspect(d.container)
		if err == nil && st.Running {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("scratch container %s never became running", d.container)
		}
		time.Sleep(100 * time.Millisecond)
	}
	pre, err := d.cli.Snapshot(d.container)
	if err != nil {
		return nil, err
	}
	if len(pre) == 0 {
		return nil, fmt.Errorf("empty pre-state snapshot")
	}
	return pre, nil
}

func (d *dockerLandable) land() landOutcome {
	var out backend_docker.Outcome
	var fault *backend_docker.Fault
	switch d.id {
	case "docker:pause":
		out, fault = d.cli.Pause(d.container)
	case "docker:kill":
		out, fault = d.cli.Kill(d.container)
	case "docker:oom":
		out, fault = d.cli.OOM(d.container, 16<<20)
	default:
		return bad("land: unknown docker primitive %q", d.id)
	}
	d.fault = fault
	if !out.OK() {
		reason := out.NoOpReason
		if out.Err != nil {
			reason = out.Err.Error()
		}
		return bad("docker land: %s", reason)
	}
	return ok(out.LandedProof)
}

func (d *dockerLandable) inverse() landOutcome {
	if d.fault == nil {
		return bad("inverse: no armed docker fault (land never proved)")
	}
	out, err := d.fault.Revert()
	if err != nil {
		return bad("docker revert: %v", err)
	}
	if !out.OK() {
		return bad("docker revert: %s", out.NoOpReason)
	}
	// the independent half: re-read the state the inverse claims restored
	st, err := d.cli.Inspect(d.container)
	if err != nil {
		return bad("post-revert inspect: %v", err)
	}
	switch d.id {
	case "docker:pause":
		if st.Paused || !st.Running {
			return bad("revert proven by the fault but inspect disagrees: %s", st.SnapshotText())
		}
	case "docker:kill":
		if !st.Running {
			return bad("revert proven by the fault but inspect disagrees: %s", st.SnapshotText())
		}
	case "docker:oom":
		if !st.Running || st.MemoryBytes != d.preMem {
			return bad("revert proven by the fault but the memory did not return to %d: %s", d.preMem, st.SnapshotText())
		}
	}
	return ok(out.LandedProof + " [independent inspect: " + st.SnapshotText() + "]")
}

func (d *dockerLandable) post() ([]byte, error) {
	return d.cli.Snapshot(d.container)
}

func (d *dockerLandable) cleanup() {
	if d.cli != nil && d.container != "" {
		_ = d.cli.RemoveScratch(d.container)
	}
}

// joinPath is filepath.Join kept local (the harness's scratch dir + name).
func joinPath(dir, name string) string { return dir + "/" + name }

// ── I-002: dependency cold return (docker actuator) ─────────────────────────
//
// The TRBL-031 shape: a dependency dies and comes back as an EMPTY instance
// on the SAME address. The docker actuator: kill the L0 scratch container
// (the port-refuses half), `docker start` it (the same address returns),
// and prove the cold return — the container restarted with RestartCount
// still 0-through-its-own-lifecycle (a fresh exec, not a daemon-side
// restart) and a FRESH boot identity (StartedAt moved).
//
// The measured loop:
//
//	prepare — scratch container (sleep workload, 64 MiB cap), snapshot =
//	          the inspect line (the state the fault must restore).
//	land    — docker kill: Running=false ExitCode=137 (the P-008 proof,
//	          reused), then docker start: Running=true AND StartedAt newer
//	          than the pre-kill value (the SAME ADDRESS returned — the name
//	          and IP are docker-stable) — the cold return's own proof.
//	inverse — none needed beyond what land already restored (kill→start IS
//	          the recovery action the catalog's inverse column names for
//		  P-008: "docker start"); the landable's inverse step re-proves
//	          the running state as the measured revert (the runner requires
//	          a non-empty inverse proof).
//	post    — the inspect line again (byte-compare vs prepare).
//
// Capability: the docker daemon + local image, else the named skip (the
// same gate the docker landables use).
type coldReturnLandable struct {
	cli        *backend_docker.CLI
	image      string
	container  string
	startedPre string
	killFault  *backend_docker.Fault
}

func (c *coldReturnLandable) capability() (string, bool) {
	cli, err := backend_docker.NewCLI()
	if err != nil {
		return err.Error(), true
	}
	if _, err := cli.Probe(); err != nil {
		return err.Error(), true
	}
	img := resolveDockerImage(cli)
	if img == "" {
		return fmt.Sprintf("no local image any of %v (the selftest never pulls)", dockerImageCandidates), true
	}
	c.image = img
	c.cli = cli
	return "", false
}

func (c *coldReturnLandable) prepare() ([]byte, error) {
	c.container = backend_docker.MintScratchName()
	if err := c.cli.RunScratch(c.container, c.image, 0, []string{"sleep", "90"}); err != nil {
		return nil, fmt.Errorf("scratch container: %w", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, err := c.cli.Inspect(c.container)
		if err == nil && st.Running {
			c.startedPre = st.StartedAt
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("scratch container %s never became running", c.container)
		}
		time.Sleep(100 * time.Millisecond)
	}
	pre, err := c.cli.Snapshot(c.container)
	if err != nil {
		return nil, err
	}
	if len(pre) == 0 {
		return nil, fmt.Errorf("empty pre-state snapshot")
	}
	return pre, nil
}

func (c *coldReturnLandable) land() landOutcome {
	// half 1: the dependency dies (docker kill — exit 137 proof)
	out, fault := c.cli.Kill(c.container)
	if !out.OK() {
		reason := out.NoOpReason
		if out.Err != nil {
			reason = out.Err.Error()
		}
		return bad("cold-return kill half: %s", reason)
	}
	c.killFault = fault
	// half 2: it returns on the SAME address as an EMPTY instance
	rev, err := c.killFault.Revert() // docker start
	if err != nil {
		return bad("cold-return restart half: %v", err)
	}
	if !rev.OK() {
		return bad("cold-return restart half: %s", rev.NoOpReason)
	}
	st, err := c.cli.Inspect(c.container)
	if err != nil {
		return bad("post-restart inspect: %v", err)
	}
	if !st.Running {
		return bad("dependency did not return running: %s", st.SnapshotText())
	}
	if st.StartedAt == c.startedPre {
		return bad("StartedAt unchanged after restart (%q) — this was not a cold return", st.StartedAt)
	}
	return ok(fmt.Sprintf("cold-return: %s died (exit 137) and returned on the same address with a fresh boot identity (StartedAt %q -> %q)",
		c.container, c.startedPre, st.StartedAt))
}

func (c *coldReturnLandable) inverse() landOutcome {
	// the recovery action (docker start) already ran in land(); the inverse
	// step MEASURES the recovered state stands (the runner requires the
	// measured proof — a revert of a fault that ended recovered is the
	// standing-state proof, not another kill)
	if c.cli == nil || c.container == "" {
		return bad("inverse: no scratch container (prepare never ran)")
	}
	st, err := c.cli.Inspect(c.container)
	if err != nil {
		return bad("inverse inspect: %v", err)
	}
	if !st.Running {
		return bad("inverse: container not running after the cold return: %s", st.SnapshotText())
	}
	return ok(fmt.Sprintf("cold-return steady state: %s Running=true (recovered; the recovery action was the fault's own restart verb)", c.container))
}

func (c *coldReturnLandable) post() ([]byte, error) {
	return c.cli.Snapshot(c.container)
}

func (c *coldReturnLandable) cleanup() {
	if c.cli != nil && c.container != "" {
		_ = c.cli.RemoveScratch(c.container)
	}
}
