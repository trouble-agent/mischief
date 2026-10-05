package selftest

import (
	"strings"
	"testing"
	"time"
)

// ── corpus & state derivation ───────────────────────────────────────────────

// TestCoveredCorpusMatchesCatalog: every catalog id is covered (live
// landable or recorded skip) — the corpus is DERIVED, not hand-enumerated.
// A future catalog addition without a landable entry fails this test.
func TestCoveredCorpusMatchesCatalog(t *testing.T) {
	cat, err := loadCatalogForTest()
	if err != nil {
		t.Skipf("catalog unavailable: %v", err)
	}
	covered := map[string]bool{}
	for _, id := range CoveredIDs() {
		covered[id] = true
	}
	for _, id := range cat.IDs {
		if !covered[id] {
			t.Errorf("catalog primitive %s is not covered by the selftest corpus (add a landable or a named skip)", id)
		}
	}
}

// TestStateOfHonestyMapping: the recorded state derives from the RESULT —
// pass⇒green, skip⇒missing, fail⇒failing. A skip must never record green
// (MSF-030: no force-set green).
func TestStateOfHonestyMapping(t *testing.T) {
	cases := []struct{ state, want string }{
		{"pass", "green"},
		{"skip", "missing"},
		{"fail", "failing"},
		{"mystery", "failing"},
	}
	for _, c := range cases {
		got := StateOf(Result{ID: "X-001", State: c.state})
		if got != c.want {
			t.Errorf("StateOf(state=%s) = %s, want %s", c.state, got, c.want)
		}
	}
}

// TestSummarizeExitContract: exit 0 only when every result is
// pass-or-skip; any fail names the failing ids and exits 1 (the M1 exit
// criterion).
func TestSummarizeExitContract(t *testing.T) {
	summary, exit := Summarize([]Result{
		{ID: "P-001", State: "pass"},
		{ID: "N-001", State: "skip"},
	})
	if exit != 0 {
		t.Fatalf("green-or-skip run exited %d (%s), want 0", exit, summary)
	}
	summary, exit = Summarize([]Result{
		{ID: "P-001", State: "pass"},
		{ID: "S-001", State: "fail", Reason: "injected red"},
		{ID: "N-012", State: "fail", Reason: "injected red"},
	})
	if exit != 1 {
		t.Fatalf("failing run exited %d, want 1", exit)
	}
	if !strings.Contains(summary, "S-001") || !strings.Contains(summary, "N-012") {
		t.Fatalf("summary does not name the failing primitives: %s", summary)
	}
}

// ── the measured loop (RED proofs, no real backends) ────────────────────────

// stubLandable drives the runner through scripted outcomes.
type stubLandable struct {
	capMsg     string
	capMissing bool
	snap       []byte
	landOut    landOutcome
	invOut     landOutcome
	postSnap   func() []byte
	prepared   bool
	landed     bool
	cleanedUp  bool
}

func (s *stubLandable) capability() (string, bool) { return s.capMsg, s.capMissing }
func (s *stubLandable) prepare() ([]byte, error) {
	s.prepared = true
	return s.snap, nil
}
func (s *stubLandable) land() landOutcome    { s.landed = true; return s.landOut }
func (s *stubLandable) inverse() landOutcome { return s.invOut }
func (s *stubLandable) post() ([]byte, error) {
	if s.postSnap != nil {
		return s.postSnap(), nil
	}
	return s.snap, nil
}
func (s *stubLandable) cleanup() { s.cleanedUp = true }

// runnerWith builds a runner over one stub under the real loop.
func runnerWith(l *stubLandable) runner {
	return runner{
		corpus: func() []string { return []string{"Z-001"} },
		build: func(id, dir string) landable { return l },
		now:   time.Now,
	}
}

// TestLandDidNotProveIsFail: a landing whose proof never fires FAILS —
// it is never a pass (AC-3).
func TestLandDidNotProveIsFail(t *testing.T) {
	l := &stubLandable{snap: []byte("pre")}
	l.landOut = bad("counter file absent — the fault never fired")
	r := runnerWith(l)
	res := r.RunOne("Z-001")
	if res.State != "fail" || !strings.Contains(res.Reason, "AC-3") {
		t.Fatalf("unproven landing graded %q (%s), want fail naming AC-3", res.State, res.Reason)
	}
	if res.LandProof != "" {
		t.Fatalf("a failed landing must not carry a proof: %q", res.LandProof)
	}
}

// TestLandedWithoutProofTextIsFail: a landed=true outcome with an EMPTY
// proof text is a contract violation and fails (a landing without a
// measurement is a no_op — AC-3's whole point).
func TestLandedWithoutProofTextIsFail(t *testing.T) {
	l := &stubLandable{snap: []byte("pre")}
	l.landOut = ok("") // landed, but measured nothing
	r := runnerWith(l)
	res := r.RunOne("Z-001")
	if res.State != "fail" || !strings.Contains(res.Reason, "empty proof text") {
		t.Fatalf("empty-proof landing graded %q (%s), want fail", res.State, res.Reason)
	}
}

// TestRevertWithoutProofTextIsFail: a proven landing whose inverse
// reports landed with an empty proof FAILS (AC-4: revert measured, not
// asserted).
func TestRevertWithoutProofTextIsFail(t *testing.T) {
	l := &stubLandable{snap: []byte("pre")}
	l.landOut = ok("land measured")
	l.invOut = ok("")
	r := runnerWith(l)
	res := r.RunOne("Z-001")
	if res.State != "fail" || !strings.Contains(res.Reason, "AC-4") {
		t.Fatalf("empty-proof revert graded %q (%s), want fail naming AC-4", res.State, res.Reason)
	}
}

// TestPreStateDriftIsFail: a proven land + proven inverse whose post-
// state DRIFTS from the pre-state FAILS (the byte-identical compare is
// the AC-4 revert measurement's final half).
func TestPreStateDriftIsFail(t *testing.T) {
	l := &stubLandable{snap: []byte("pre-state-bytes")}
	l.landOut = ok("land measured")
	l.invOut = ok("revert measured")
	l.postSnap = func() []byte { return []byte("pre-state-BYTES") }
	r := runnerWith(l)
	res := r.RunOne("Z-001")
	if res.State != "fail" || !strings.Contains(res.Reason, "drifted") {
		t.Fatalf("drifted post-state graded %q (%s), want fail", res.State, res.Reason)
	}
}

// TestEmptyPreStateIsFail: an empty snapshot makes the compare vacuous —
// refused (a vacuous compare is how a revert measurement lies).
func TestEmptyPreStateIsFail(t *testing.T) {
	l := &stubLandable{snap: nil}
	l.landOut = ok("land measured")
	l.invOut = ok("revert measured")
	r := runnerWith(l)
	res := r.RunOne("Z-001")
	if res.State != "fail" || !strings.Contains(res.Reason, "vacuous") {
		t.Fatalf("empty pre-state graded %q (%s), want fail", res.State, res.Reason)
	}
}

// TestHappyPathPassesCarriesProofs: land proves + inverse proves + post
// byte-identical ⇒ pass, with both proofs carried on the result.
func TestHappyPathPassesCarriesProofs(t *testing.T) {
	l := &stubLandable{snap: []byte("same")}
	l.landOut = ok("land proof text")
	l.invOut = ok("revert proof text")
	r := runnerWith(l)
	res := r.RunOne("Z-001")
	if res.State != "pass" {
		t.Fatalf("measured loop graded %q (%s), want pass", res.State, res.Reason)
	}
	if res.LandProof != "land proof text" || res.RevertProof != "revert proof text" {
		t.Fatalf("pass lost its proofs: land=%q revert=%q", res.LandProof, res.RevertProof)
	}
	if res.Duration <= 0 {
		t.Fatalf("pass must carry a measured duration, got %v", res.Duration)
	}
}

// TestCapabilityMissingIsSkipNeverPass: the capability gate SKIPs with
// the named missing piece — never a pass, never a silent fall-through
// (AC-11 shape).
func TestCapabilityMissingIsSkipNeverPass(t *testing.T) {
	l := &stubLandable{capMsg: "no such device: the helper", capMissing: true}
	r := runnerWith(l)
	res := r.RunOne("Z-001")
	if !res.IsSkip() {
		t.Fatalf("capability gap graded %q, want skip", res.State)
	}
	if !strings.Contains(res.Reason, "capability_unavailable") || !strings.Contains(res.Reason, "the helper") {
		t.Fatalf("skip must name the missing piece: %q", res.Reason)
	}
	if l.prepared || l.landed {
		t.Fatal("a skipped primitive must never reach prepare/land")
	}
	if !l.cleanedUp {
		t.Fatal("cleanup must run even on a skip")
	}
}

// TestUnknownIdFailsNotSkips: an id outside the corpus FAILS with the
// unknown-primitive reason (a skip would inflate the exit criterion).
func TestUnknownIdFailsNotSkips(t *testing.T) {
	r := runnerWith(&stubLandable{snap: []byte("x")})
	res := r.RunOne("NOPE-99")
	if res.State != "fail" || !strings.Contains(res.Reason, "unknown primitive") {
		t.Fatalf("unknown id graded %q (%s), want fail", res.State, res.Reason)
	}
}

// TestCleanupRunsOnFailure: cleanup runs on every path (children, fds).
func TestCleanupRunsOnFailure(t *testing.T) {
	l := &stubLandable{snap: []byte("pre")}
	l.landOut = bad("land exploded")
	r := runnerWith(l)
	_ = r.RunOne("Z-001")
	if !l.cleanedUp {
		t.Fatal("cleanup did not run on a failed loop")
	}
}

// ── firstDiff / journal records ─────────────────────────────────────────────

func TestFirstDiffNamesFirstByte(t *testing.T) {
	if d := firstDiff([]byte("abc"), []byte("abc")); d != "" {
		t.Fatalf("identical snapshots drifted: %q", d)
	}
	d := firstDiff([]byte("abc"), []byte("axc"))
	if !strings.Contains(d, "byte 1") {
		t.Fatalf("firstDiff = %q, want byte 1", d)
	}
	d = firstDiff([]byte("abc"), []byte("abcd"))
	if !strings.Contains(d, "length") {
		t.Fatalf("firstDiff = %q, want length", d)
	}
}

// TestJournalRecordsVocabulary: the record set uses only the journal
// package's lifecycle types, carries both proofs on a pass, and never
// grades recovered on a fail (the honesty ladder, end to end).
func TestJournalRecordsVocabulary(t *testing.T) {
	res := []Result{
		{ID: "P-001", State: "pass", LandProof: "lp", RevertProof: "rp"},
		{ID: "S-004", State: "skip", Reason: "cap missing"},
		{ID: "T-001", State: "fail", Reason: "injected red"},
	}
	recs := JournalRecords("testrun0000000001", res)
	for _, r := range recs {
		if !r.Type.Valid() {
			t.Fatalf("record type %q is not in the journal vocabulary", r.Type)
		}
		if r.RunID != "testrun0000000001" {
			t.Fatalf("record run id %q, want testrun0000000001", r.RunID)
		}
	}
	var landed, reverted int
	var verdict string
	for _, r := range recs {
		switch r.Type {
		case "fault_landed":
			landed++
			if r.Fields["primitive"] == "T-001" && r.Fields["outcome"] != "no_op" {
				t.Fatalf("a failed primitive's landed record must grade no_op: %v", r.Fields)
			}
		case "fault_reverted":
			reverted++
			if r.Fields["proof"] != "rp" {
				t.Fatalf("the revert record lost the measured proof: %v", r.Fields)
			}
		case "verdict":
			verdict = string(r.Verdict)
		}
	}
	if landed != 2 || reverted != 1 {
		t.Fatalf("record counts: landed=%d reverted=%d, want 2/1", landed, reverted)
	}
	if verdict != "no_op" {
		t.Fatalf("a run with a fail graded %q, want no_op (never recovered)", verdict)
	}
}

// TestJournalRunIDStableAndSensitive: same results ⇒ same id; any proof
// change ⇒ different id (the content-derived id the journal line names).
func TestJournalRunIDStableAndSensitive(t *testing.T) {
	a := []Result{{ID: "P-001", State: "pass", LandProof: "lp", RevertProof: "rp"}}
	b := []Result{{ID: "P-001", State: "pass", LandProof: "lp", RevertProof: "rp"}}
	c := []Result{{ID: "P-001", State: "pass", LandProof: "CHANGED", RevertProof: "rp"}}
	if JournalRunID(a) != JournalRunID(b) {
		t.Fatal("identical results derived different run ids")
	}
	if JournalRunID(a) == JournalRunID(c) {
		t.Fatal("changed proofs derived the same run id")
	}
	if len(JournalRunID(a)) != 16 {
		t.Fatalf("run id length %d, want 16 hex chars", len(JournalRunID(a)))
	}
}
