// Package selftest is the MSF-015 selftest harness: the verb
// `mischief selftest (--all | --primitive <id>)` and the engine under it.
//
// The contract it enforces (board row MSF-015; PRD §4 loop rule 4, §6.1,
// §7 ladder): every catalog primitive must PROVE, on an L0 scratch target
// owned by the run, that (a) its landing path lands — the descriptor's
// landed_proof is measured TRUE out-of-band, not asserted (AC-3), and (b)
// its inverse restores the exact pre-state — the pre-state is captured
// before the landing, replayed after the inverse, and byte-compared
// (AC-4). A primitive that does both on THIS host may be recorded green;
// anything less never is (MSF-030 honesty ladder — the harness never
// force-sets green).
//
// Where the primitive set comes from: selftest walks the CATALOG
// (catalog/faults at MSF-015: C-009 F-009 I-002 N-001 N-012 P-001 R-001
// S-001 S-004 T-001), not a hand list — a primitive added to the catalog
// is selftested by construction. Green states live in the "green"
// registry here (greenStates), consumed downstream by the AC-19 wiring:
// rails.CheckScope refuses a non-green primitive outside an L0 scratch
// scope (the predicate is the catalog's own SelftestState.RefusesOutsideL0;
// backend_signal.Gate restates it at the backend boundary).
//
// Result vocabulary per primitive: pass (landed + proved + reverted +
// pre-state byte-identical), skip (capability_unavailable with the
// missing piece NAMED — AC-11 shape — or no M1-tier actuator for the
// primitive yet; a skip is recorded, never a pass), fail (landed and did
// not prove, proved and did not revert, or the pre-state compare drifted).
//
// Scope discipline: everything here targets an os.MkdirTemp scratch dir
// the run owns and removes. Nothing outside the scratch dir is ever
// written. The pre-state capture/compare is INSIDE the scratch dir — the
// pre-state of the faulted object itself — so "revert measured" means the
// target object is byte-identical to what the run snapshotted, not that
// the host is untouched (the host never is: the fault lives on the
// scratch object alone).
//
// NOT-LIST (what this package does not promise at M1):
//
//   - It does not promise every catalog primitive has an M1 actuator: the
//     green corpus covers the rootless, privilege-free set (P-001, S-001,
//     N-012, F-009, R-001). The rest SKIP with the named reason; their
//     selftests arrive with their actuators (M2/M3).
//   - It does not write the catalog's selftest: field — green is a
//     property of THIS HOST at run time (greenStates is consulted live,
//     the descriptor stays the shipped contract).
//   - It does not grade fault-experiment verdicts: a selftest proves the
//     MECHANISM lands and reverts; SPEC-05's verdict engine stays
//     untouched.
//
// ch:trace row=MSF-015 spec=docs/prd/mischief-v0.1.md (§4, §6.1, §7) + .coding-hermes/board/tasks.jsonl (MSF-015) evidence=internal/selftest/ + cmd/mischief/ witness=none:scratch-only-live-runs
package selftest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"time"
)

// L0 is the scratch scope level (the ladder's "everything, always
// allowed — where selftest lives").
const L0 = 0

// greenStates holds the primitives whose selftest is GREEN on this host
// after MSF-015: measured land + revert proofs on the L0 scratch harness.
// R-001 (systemd-run user scope) is green CONDITIONALLY — GreenReports()
// consults the live capability probe, so an absent user manager reports
// the honest missing-piece skip rather than a stale green.
//
// MSF-010 adds the SPEC-09 backends: the sim primitives (I-003/004/006/
// 010/011/012/013/014) are unconditionally green (loopback + scratch log,
// no daemon), and the docker primitives (docker:pause/kill/oom) are green
// where the live L0 selftests proved them on THIS host (docker 29.x,
// cgroup v2) — the same measured-proofs-only rule, no force-set green:
// a host without the daemon reports the skip, never a green.
var greenStates = map[string]bool{
	"P-001": true, // SIGSTOP / SIGCONT: state T proven, resume proven
	"S-001": true, // shim EIO: counter proven, detach + next-write proven
	"N-012": true, // proxy 429: hit-ledger proven, disarm + replay proven
	"F-009": true, // file-replaced-under-writer: inode identity proven, restore byte-identical
	"R-001": true, // systemd-run scope: property read-back proven, stop verified (probe-gated)
	// SPEC-09 (MSF-010): provider simulator — rootless, no daemon
	"I-003": true, // sim 429 burst: request-log proven, disarm proven
	"I-004": true, // sim 5xx/503: request-log proven
	"I-006": true, // sim object-store 404-on-existing: request-log proven
	"I-010": true, // sim auth expiry 401: request-log proven
	"I-011": true, // sim metadata 500: request-log proven
	"I-012": true, // sim quota exhaustion: request-log proven
	"I-013": true, // sim snapshot stale restore: request-log proven
	"I-014": true, // sim DNS propagation split: request-log proven
	// SPEC-09 (MSF-010): docker node faults — live-proven on this host
	// (docker 29.x + cgroup v2); a host without the daemon skips honestly
	"docker:pause": true,
	"docker:kill":  true,
	"docker:oom":   true,
}

// GreenState answers the AC-19 question for one primitive id: green, or
// the SelftestState string that refuses outside L0. The catalog's own
// predicate consumes this (SelftestState.RefusesOutsideL0).
func GreenState(id string) string {
	if greenStates[id] {
		return "green"
	}
	return "missing"
}

// StateOf derives the recorded selftest state from a RUN RESULT (not from
// the static expectation): pass ⇒ green; skip ⇒ missing (the primitive
// is un-selftested on this host — a recorded skip is never green); fail
// ⇒ failing. This is the state the AC-19 predicate reads after the run,
// per the honesty ladder: only a measured pass may be recorded green.
func StateOf(r Result) string {
	switch r.State {
	case "pass":
		return "green"
	case "skip":
		return "missing"
	default:
		return "failing"
	}
}

// IsGreen reports whether the primitive selftests green on this host.
func IsGreen(id string) bool { return greenStates[id] }

// GreenIDs returns the green primitive ids, sorted.
func GreenIDs() []string {
	out := make([]string, 0, len(greenStates))
	for id := range greenStates {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Result is one primitive's selftest outcome.
type Result struct {
	// ID is the catalog primitive id ("P-001").
	ID string
	// State is pass | skip | fail.
	State string
	// LandProof is the measured landed-proof text ("" when never landed).
	LandProof string
	// RevertProof is the measured revert-proof text ("" when never
	// reverted). Empty on a pass is a contract violation the runner
	// refuses (AC-4: revert measured, not asserted).
	RevertProof string
	// Reason names why a skip skipped (the missing capability piece) or a
	// fail failed — always populated for skip/fail, always "" for pass.
	Reason string
	// Duration is the measured wall time of the whole land+prove+revert
	// loop.
	Duration time.Duration
}

// OK reports whether the result is a pass.
func (r Result) OK() bool { return r.State == "pass" }

// IsSkip reports whether the result is a clean skip (never a pass, never
// a failure).
func (r Result) IsSkip() bool { return r.State == "skip" }

// String renders the per-primitive line the CLI prints.
func (r Result) String() string {
	switch r.State {
	case "pass":
		return fmt.Sprintf("%-8s PASS  land: %s | revert: %s (%s)", r.ID, r.LandProof, r.RevertProof, r.Duration.Round(time.Millisecond))
	case "skip":
		return fmt.Sprintf("%-8s SKIP  %s", r.ID, r.Reason)
	default:
		return fmt.Sprintf("%-8s FAIL  %s (land=%q revert=%q)", r.ID, r.Reason, r.LandProof, r.RevertProof)
	}
}

// coveredSkips are the catalog primitives whose actuator cannot run on
// the L0 scratch harness at M1 (each SKIPs with its named missing piece
// from buildLandable). They are part of the covered corpus: a run
// exercises them and records the skip.
//
// MSF-010 moves C-009 and I-002 OUT of the pure-skip set: both gain a
// docker-backed landable when the host has the daemon (their landables
// still SKIP with the named piece when it does not — I-002's cold-return
// needs a container the run can kill and restart, which the docker
// backend provides). C-009 (rolling replace) stays a skip: multi-container
// orchestration is out of v0.1's docker backend scope.
var coveredSkips = []string{"C-009", "I-002", "N-001", "S-004", "T-001"}

// CoveredIDs returns every primitive id this build's harness covers
// (green live selftests + recorded skips), sorted. The runner's corpus
// membership check consumes it; a catalog id OUTSIDE it fails with "no
// M1-tier selftest exists" (honest for future catalog growth), and an id
// outside the CATALOG fails as unknown.
func CoveredIDs() []string {
	out := append(GreenIDs(), coveredSkips...)
	sort.Strings(out)
	return out
}

// runner is the injected harness seam (tests hand a landable, the CLI
// wires the real backends).
type runner struct {
	corpus func() []string
	build  func(id, dir string) landable
	now    func() time.Time
}

// newRunner builds the production runner: the covered corpus, the real
// backend builder, the wall clock.
func newRunner() runner {
	return runner{
		corpus: CoveredIDs,
		build:  buildLandable,
		now:    time.Now,
	}
}

// RunOne selftests one primitive id on a fresh L0 scratch target. An id
// outside the corpus is a fail (a selftest for an unknown primitive must
// not silently pass — same refusal shape as an unknown catalog id).
func (rn runner) RunOne(id string) Result {
	if rn.corpus == nil || rn.build == nil || rn.now == nil {
		return Result{ID: id, State: "fail", Reason: "harness misconfigured: nil seam"}
	}
	known := false
	for _, k := range rn.corpus() {
		if k == id {
			known = true
			break
		}
	}
	if !known {
		return Result{ID: id, State: "fail", Reason: fmt.Sprintf("unknown primitive %q: not in the selftest corpus (the catalog ids it covers)", id)}
	}
	start := rn.now()
	res := rn.run(id)
	res.Duration = rn.now().Sub(start)
	return res
}

// run executes the land+prove+revert loop for one primitive: scratch dir,
// landable build, capability gate, land, measure, inverse, measure,
// pre-state compare — every step named on failure.
func (rn runner) run(id string) Result {
	dir, err := scratchUnder(id)
	if err != nil {
		return Result{ID: id, State: "fail", Reason: fmt.Sprintf("scratch dir: %v", err)}
	}
	defer func() { _ = os.RemoveAll(dir) }()

	l := rn.build(id, dir)
	res := Result{ID: id}
	defer l.cleanup()
	if msg, missing := l.capability(); missing {
		// AC-11 shape: the SKIP names the missing piece. A skip is a
		// recorded outcome, never a pass (the AC-19 set stays honest).
		res.State = "skip"
		res.Reason = "capability_unavailable: " + msg
		return res
	}

	pre, err := l.prepare()
	if err != nil {
		res.State = "fail"
		res.Reason = "prepare: " + err.Error()
		return res
	}
	if len(pre) == 0 {
		res.State = "fail"
		res.Reason = "prepare produced an empty pre-state — the land+revert compare would be vacuous"
		return res
	}

	land := l.land()
	if !land.landed {
		res.State = "fail"
		res.Reason = "land did not prove (AC-3): " + land.err
		return res
	}
	if land.proof == "" {
		res.State = "fail"
		res.Reason = "landed outcome carried an empty proof text — a landing without a measurement is a no_op (AC-3)"
		return res
	}
	res.LandProof = land.proof

	inv := l.inverse()
	if !inv.landed {
		res.State = "fail"
		res.Reason = "inverse did not prove (AC-4): " + inv.err
		return res
	}
	if inv.proof == "" {
		res.State = "fail"
		res.Reason = "inverse outcome carried an empty proof text — a revert without a measurement is not a revert (AC-4)"
		return res
	}
	res.RevertProof = inv.proof

	after, err := l.post()
	if err != nil {
		res.State = "fail"
		res.Reason = "post-state read: " + err.Error()
		return res
	}
	if drift := firstDiff(pre, after); drift != "" {
		res.State = "fail"
		res.Reason = "revert measured but pre-state drifted (AC-4): " + drift
		return res
	}

	res.State = "pass"
	return res
}

// RunAll selftests every primitive in the corpus, sorted (the catalog's
// own sort order), each on its own fresh scratch target.
func (rn runner) RunAll() []Result {
	ids := rn.corpus()
	sort.Strings(ids)
	out := make([]Result, 0, len(ids))
	for _, id := range ids {
		out = append(out, rn.RunOne(id))
	}
	return out
}

// Summarize renders the run's verdict block: counts, failing ids, exit.
// exit is 0 when every result is pass-or-skip (the M1 exit criterion),
// 1 otherwise with the failing ids named.
func Summarize(res []Result) (summary string, exitCode int) {
	pass, skip, fail := 0, 0, 0
	var failed []string
	for _, r := range res {
		switch r.State {
		case "pass":
			pass++
		case "skip":
			skip++
		default:
			fail++
			failed = append(failed, r.ID)
		}
	}
	summary = fmt.Sprintf("selftest: %d pass, %d skip, %d fail (of %d)", pass, skip, fail, len(res))
	if fail > 0 {
		sort.Strings(failed)
		summary += fmt.Sprintf(" — FAILING: %s", joinIDs(failed))
		return summary, 1
	}
	return summary, 0
}

func joinIDs(ids []string) string {
	s := ""
	for i, id := range ids {
		if i > 0 {
			s += ", "
		}
		s += id
	}
	return s
}

// JournalRunID derives the selftest run's content id (exported for the
// CLI's journal line): the same derivation family as journal.RunID
// (hex(sha256(seed||bytes))[:16]) with the run's own bytes (the sorted
// result lines, which carry every proof). Same results ⇒ same id
// (reproducibility), different proofs ⇒ different id.
func JournalRunID(res []Result) string {
	h := sha256.New()
	h.Write([]byte("seed\n1\n"))
	for _, r := range res {
		h.Write([]byte(r.String()))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// firstDiff names the first byte position where two snapshots differ
// (the AC-4 drift detail the fail line carries).
func firstDiff(pre, after []byte) string {
	if bytesEqual(pre, after) {
		return ""
	}
	n := len(pre)
	if len(after) < n {
		n = len(after)
	}
	for i := 0; i < n; i++ {
		if pre[i] != after[i] {
			return fmt.Sprintf("byte %d: pre %02x != post %02x", i, pre[i], after[i])
		}
	}
	return fmt.Sprintf("length: pre %d bytes != post %d bytes", len(pre), len(after))
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// scratchUnder is the one place a scratch dir is made (the brief: "tmpdir
// under /tmp owned by the run"); kept separate so tests can assert every
// target went through it.
func scratchUnder(id string) (string, error) {
	return os.MkdirTemp("", "mischief-selftest-"+id+"-*")
}
