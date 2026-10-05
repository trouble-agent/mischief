package observe

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/journal"
)

// observe_test.go — SPEC-12 / AC-14 against a LOCAL fake ledger only (the
// package never contacts a real trouble instance; the only server any
// test creates is the httptest fake below).
//
// ch:trace row=MSF-013 spec=docs/SPEC-PLAN.md#SPEC-12 test=internal/observe/observe_test.go evidence=internal/observe/observe_test.go witness=none:no-live-trouble-instance-in-worktree

// decl is the ex-002 declaration shape, built in memory.
func decl(record string) *journal.TroubleObserve {
	return &journal.TroubleObserve{
		Namespace:    "trouble",
		ExpectRecord: record,
		Within:       "60s",
	}
}

// censusServer is the fake trouble ledger: it serves a fixed record set
// AND keeps a census of every request it saw (method, path, body) — the
// read-only proof reads this census. zero makes the census assertions
// readable in one place.
type censusServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	method []string
	paths  []string
	bodies []string
	// lines is the ledger content the server answers with.
	lines []Line
	// failStatus, when non-zero, is answered for every read (the
	// error-arm server).
	failStatus int
	// rawBody, when non-empty, is served verbatim (the bad-shape arm).
	rawBody string
}

func newCensus(t *testing.T, lines []Line) *censusServer {
	t.Helper()
	c := &censusServer{lines: lines}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.method = append(c.method, r.Method)
		c.paths = append(c.paths, r.URL.Path)
		body := []byte{}
		if r.Body != nil {
			b, _ := readAll(r.Body)
			body = b
		}
		c.bodies = append(c.bodies, string(body))
		c.mu.Unlock()
		switch {
		case c.failStatus != 0:
			w.WriteHeader(c.failStatus)
		case c.rawBody != "":
			_, _ = w.Write([]byte(c.rawBody))
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(c.lines)
		}
	}))
	t.Cleanup(c.srv.Close)
	return c
}

// census returns a snapshot of every request the server saw.
func (c *censusServer) census() (methods, paths, bodies []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.method...), append([]string{}, c.paths...), append([]string{}, c.bodies...)
}

// client wires a Ledger pinned to the fake server with an instant clock:
// the polls do not actually sleep (Sleep is a no-op) and time does not
// advance (Now is frozen) — the loop therefore spins until the deadline
// check runs out. The returned crank advances the frozen clock (the miss
// tests jump it past the deadline).
func (c *censusServer) client(poll time.Duration) (*Ledger, *time.Time) {
	now := time.Now()
	ledger := &Ledger{
		BaseURL:   c.srv.URL,
		Namespace: "trouble",
		HTTP:      c.srv.Client(),
		Poll:      poll,
		Now:       func() time.Time { return now },
		Sleep:     func(time.Duration) {},
	}
	return ledger, &now
}

// TestObserveMatchNamesTheLedgerLine (AC-14, hit arm): the expected
// record, in-window, matches — Matched=true, the RESULT carries the
// ledger line's id and ts, time-to-detect is line.ts − land, and the
// journal record names the matched line.
func TestObserveMatchNamesTheLedgerLine(t *testing.T) {
	land := time.Unix(1000, 0)
	lines := []Line{
		{ID: "TROUBLE-HUB-003", TS: 900},
		{ID: "TROUBLE-HUB-004", TS: 1030}, // the expected record, 30 s after land
		{ID: "TROUBLE-HUB-005", TS: 2000},
	}
	c := newCensus(t, lines)
	l, _ := c.client(250 * time.Millisecond)

	res, err := l.Assert("I-002", decl("TROUBLE-HUB-004"), land)
	if err != nil {
		t.Fatalf("assert: %v", err)
	}
	if !res.Matched {
		t.Fatalf("matched=false, want true (record TROUBLE-HUB-004 at ts 1030, land 1000, window 60s): %+v", res)
	}
	if res.Line.ID != "TROUBLE-HUB-004" || res.Line.TS != 1030 {
		t.Errorf("matched line = %+v, want TROUBLE-HUB-004 @1030 (AC-14: the result names the ledger line it matched)", res.Line)
	}
	if got, want := res.TimeToDetect, 30*time.Second; got != want {
		t.Errorf("t_detect = %s, want %s (line.ts − land)", got, want)
	}
	if res.Reason == "" || !strings.Contains(res.Reason, "TROUBLE-HUB-004") {
		t.Errorf("reason %q must name the matched line", res.Reason)
	}
}

// TestObserveMatchJournaledRecordNamesLine: the observe_result record the
// hit renders carries ledger_line=<the matched id> — the journal's AC-14
// proof field.
func TestObserveMatchJournaledRecordNamesLine(t *testing.T) {
	land := time.Unix(1000, 0)
	c := newCensus(t, []Line{{ID: "TROUBLE-HUB-004", TS: 1005}})
	l, _ := c.client(250 * time.Millisecond)

	res, err := l.Assert("I-002", decl("TROUBLE-HUB-004"), land)
	if err != nil {
		t.Fatal(err)
	}
	rec := res.Record("run0000000000000001", 7, land.Add(35*time.Second).Unix())
	if rec.Type != journal.RecordObserveResult {
		t.Fatalf("record type %q, want observe_result", rec.Type)
	}
	// The journal's write-path validation runs inside MarshalJSON (the
	// scrub pass included) — marshalling IS the validation here, and the
	// bytes prove the record survives the journal's own wire pass.
	b, err := rec.MarshalJSON()
	if err != nil {
		t.Fatalf("observe_result record refused by the journal's write path: %v", err)
	}
	if !strings.Contains(string(b), `"ledger_line":"TROUBLE-HUB-004"`) {
		t.Errorf("wire bytes must name the matched line: %s", b)
	}
	if got := rec.Fields["ledger_line"]; got != "TROUBLE-HUB-004" {
		t.Errorf("ledger_line = %q, want the matched line id", got)
	}
	if got := rec.Fields["matched"]; got != "true" {
		t.Errorf("matched = %q, want true", got)
	}
	if got := rec.Fields["t_detect"]; got != "5s" {
		t.Errorf("t_detect = %q, want 5s", got)
	}
}

// TestObserveMissFilesDetectionGapRow (AC-14, miss arm): the record never
// appears → Matched=false with a reason-bearing gap, and the emitted gap
// row is board-shaped and passes the validator.
func TestObserveMissFilesDetectionGapRow(t *testing.T) {
	land := time.Unix(1000, 0)
	c := newCensus(t, []Line{{ID: "TROUBLE-HUB-003", TS: 900}})
	l, nowp := c.client(1 * time.Second)

	// Crank the clock past the deadline on first check so the miss is
	// immediate under the frozen clock seam.
	*nowp = land.Add(2 * time.Minute)

	res, err := l.Assert("I-002", decl("TROUBLE-HUB-004"), land)
	if err != nil {
		t.Fatalf("a miss is a RESULT, not an error: %v", err)
	}
	if res.Matched {
		t.Fatalf("matched=true, want false: %+v", res)
	}
	if res.Reason == "" {
		t.Fatal("gap without a reason — an unexplained null is junk")
	}
	if !strings.Contains(res.Reason, `"TROUBLE-HUB-004"`) || !strings.Contains(res.Reason, "1m0s") {
		t.Errorf("gap reason %q must name the expected record and the window (canonical Go duration)", res.Reason)
	}
	if res.Searched != 1 {
		t.Errorf("searched = %d, want 1 (one read happened before the deadline broke the loop)", res.Searched)
	}
	row := Gap(res, "trouble-hub", "run0000000000000001")
	if row.ID != "MSF-OBS-TROUBLE-HUB-004" {
		t.Errorf("gap id %q, want MSF-OBS-TROUBLE-HUB-004", row.ID)
	}
	if row.Priority != "P1" || row.Status != "pending" {
		t.Errorf("gap priority/status = %s/%s, want P1/pending", row.Priority, row.Status)
	}
	b, err := FileGaps([]GapRow{row})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateGapRows(b); err != nil {
		t.Fatalf("the filer's own output failed the validator: %v", err)
	}
	if !strings.Contains(string(b), `"expect_record":"TROUBLE-HUB-004"`) {
		t.Errorf("gap row must carry the expected record grep-able: %s", b)
	}
}

// TestObserveNeverWrites (the read-only proof, three layers):
//
//  1. census — every request the fake ledger saw was a GET with an EMPTY
//     body. No POST/PUT/PATCH/DELETE is ever issued, by any code path.
//  2. reflection — the Ledger type exposes NO mutating method: the
//     exported method set is exactly {Assert} (plus the value-receiver
//     mirrors). A write path cannot hide in a type whose surface has no
//     mutator to call.
//  3. route shape — the URL is a read route under /api/v1/.../records;
//     nothing in the built URL names a write verb.
func TestObserveNeverWrites(t *testing.T) {
	land := time.Unix(1000, 0)
	c := newCensus(t, []Line{{ID: "TROUBLE-HUB-004", TS: 1005}})
	l, _ := c.client(250 * time.Millisecond)

	if _, err := l.Assert("I-002", decl("TROUBLE-HUB-004"), land); err != nil {
		t.Fatal(err)
	}
	methods, paths, bodies := c.census()
	if len(methods) == 0 {
		t.Fatal("no requests observed — the census must witness the reads")
	}
	for i := range methods {
		if methods[i] != http.MethodGet {
			t.Errorf("request %d method = %s, want GET — observe must never write (AC-14 read-only)", i+1, methods[i])
		}
		if bodies[i] != "" {
			t.Errorf("request %d carried a body (%q) — a read carries none", i+1, bodies[i])
		}
		if !strings.Contains(paths[i], "/records") {
			t.Errorf("request %d path %q is not the records read route", i+1, paths[i])
		}
	}

	// Layer 2: the exported surface has no mutator. Every exported
	// method of *Ledger must be Assert (value receivers produce a
	// mirror in the pointer's set; both must be read-only names).
	lv := reflect.TypeOf(&Ledger{})
	for i := 0; i < lv.NumMethod(); i++ {
		name := lv.Method(i).Name
		if name != "Assert" {
			t.Errorf("Ledger.%s exists — the read-only client's method set is {Assert} only; a mutator must never ship", name)
		}
	}
}

// TestObserveMissOnLateRecordIsStillAGap: the record EXISTS but only
// after the window — detected-too-late is a detection gap (the reason
// names the lateness), never a pass.
func TestObserveMissOnLateRecordIsStillAGap(t *testing.T) {
	land := time.Unix(1000, 0)
	c := newCensus(t, []Line{{ID: "TROUBLE-HUB-004", TS: 1400}}) // 340 s after land, window 60 s
	l, nowp := c.client(1 * time.Second)
	*nowp = land.Add(2 * time.Minute)

	res, err := l.Assert("I-002", decl("TROUBLE-HUB-004"), land)
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched {
		t.Fatalf("a late record matched — detected-too-late is a gap: %+v", res)
	}
	if !strings.Contains(res.Reason, "after the 1m0s window") {
		t.Errorf("gap reason %q must name the lateness", res.Reason)
	}
}

// TestObservePreLandRecordNeverMatches: a record predating the land is
// not this fault's record — it must never satisfy the assertion.
func TestObservePreLandRecordNeverMatches(t *testing.T) {
	land := time.Unix(1000, 0)
	c := newCensus(t, []Line{{ID: "TROUBLE-HUB-004", TS: 500}})
	l, nowp := c.client(1 * time.Second)
	*nowp = land.Add(2 * time.Minute)

	res, err := l.Assert("I-002", decl("TROUBLE-HUB-004"), land)
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched {
		t.Fatalf("a pre-land record matched — it is not this fault's record: %+v", res)
	}
}

// TestObserveErrorIsNotAGap: an unreachable ledger is an ERROR ("the
// check could not run"), never a silently-filed gap and never a nil.
func TestObserveErrorIsNotAGap(t *testing.T) {
	c := newCensus(t, nil)
	c.srv.Close() // unreachable
	l := &Ledger{BaseURL: c.srv.URL, Namespace: "trouble", HTTP: c.srv.Client(), Poll: time.Second}

	_, err := l.Assert("I-002", decl("TROUBLE-HUB-004"), time.Unix(1000, 0))
	if err == nil {
		t.Fatal("unreachable ledger returned nil error — the check cannot have run")
	}
	if !strings.Contains(err.Error(), "ledger read") {
		t.Errorf("error %v should name the ledger read", err)
	}
}

// TestObserveRefusesBadShapes: non-200 and non-array responses are wiring
// defects (errors), including 404 (a missing namespace route is never a
// detection gap).
func TestObserveRefusesBadShapes(t *testing.T) {
	declOK := decl("TROUBLE-HUB-004")
	t.Run("status500", func(t *testing.T) {
		c := newCensus(t, nil)
		c.failStatus = 500
		l, _ := c.client(time.Second)
		if _, err := l.Assert("I-002", declOK, time.Unix(1000, 0)); err == nil || !strings.Contains(err.Error(), "status 500") {
			t.Errorf("err = %v, want a status-500 refusal", err)
		}
	})
	t.Run("notFoundIsWiringDefect", func(t *testing.T) {
		c := newCensus(t, nil)
		c.failStatus = 404
		l, _ := c.client(time.Second)
		_, err := l.Assert("I-002", declOK, time.Unix(1000, 0))
		if err == nil {
			t.Fatal("404 accepted — a missing route is a wiring defect")
		}
		if !strings.Contains(err.Error(), "wiring defect, not a detection gap") {
			t.Errorf("error %v must name the wiring-defect class", err)
		}
	})
	t.Run("objectNotArray", func(t *testing.T) {
		c := newCensus(t, nil)
		c.rawBody = `{"records": []}`
		l, _ := c.client(time.Second)
		if _, err := l.Assert("I-002", declOK, time.Unix(1000, 0)); err == nil || !strings.Contains(err.Error(), "bare JSON array") {
			t.Errorf("err = %v, want the array-shape refusal", err)
		}
	})
}

// TestValidateDeclRefusals: the declaration validator names each missing
// field (namespace / expect_record / within) and refuses an unparsable
// window.
func TestValidateDeclRefusals(t *testing.T) {
	if err := ValidateDecl(nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("nil decl: err=%v", err)
	}
	for _, tc := range []struct {
		name string
		d    *journal.TroubleObserve
		want string
	}{
		{"no namespace", &journal.TroubleObserve{ExpectRecord: "R", Within: "60s"}, "namespace"},
		{"no record", &journal.TroubleObserve{Namespace: "trouble", Within: "60s"}, "expect_record"},
		{"no window", &journal.TroubleObserve{Namespace: "trouble", ExpectRecord: "R"}, "within"},
		{"bad window", &journal.TroubleObserve{Namespace: "trouble", ExpectRecord: "R", Within: "soon"}, "unparsable"},
	} {
		if err := ValidateDecl(tc.d); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err=%v, want it to name %q", tc.name, err, tc.want)
		}
	}
}

// TestGapRowsValidationRefusals: the validator refuses each malformation
// by name (the boardctl-style self-check).
func TestGapRowsValidationRefusals(t *testing.T) {
	good := `{"id":"MSF-OBS-x","status":"pending","title":"t","priority":"P1","depends_on":[]}` + "\n"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"not json", "hello\n", "not a JSON object"},
		{"missing id", `{"status":"pending","title":"t","priority":"P1","depends_on":[]}` + "\n", "id: missing"},
		{"bad status", `{"id":"x","status":"done-ish","title":"t","priority":"P1","depends_on":[]}` + "\n", `status: "done-ish" not in the board vocabulary`},
		{"missing title", `{"id":"x","status":"pending","priority":"P1","depends_on":[]}` + "\n", "title: missing"},
		{"bad priority", `{"id":"x","status":"pending","title":"t","priority":"urgent","depends_on":[]}` + "\n", `priority: "urgent" not in the board vocabulary`},
		{"duplicate id", good + good, "duplicate id"},
		{"empty line mid-file", good + "\n" + good, "empty line"},
	}
	for _, tc := range cases {
		err := ValidateGapRows([]byte(tc.in))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err=%v, want it to name %q", tc.name, err, tc.want)
		}
	}
	if err := ValidateGapRows(nil); err != nil {
		t.Errorf("empty gap file must validate (no gaps ≠ not checked): %v", err)
	}
}

// TestFileGapsStableOrderAndNoFaultP2: gap rows sort priority-first (P2
// before P1 by the scale's order, then id), and a faultless gap files P2.
func TestFileGapsStableOrderAndNoFaultP2(t *testing.T) {
	land := time.Unix(1000, 0)
	// The faultless gaps (R-1/R-2) grade P2 (a coverage finding); the
	// fault-carrying gap (R-0) grades P1 (the product's detection
	// missed an injected fault).
	r1 := Result{Decl: *decl("R-2"), Reason: "absent", Land: land}
	r2 := Result{Decl: *decl("R-1"), Reason: "absent", Land: land}
	rows := []GapRow{Gap(r1, "proj", "run1"), Gap(r2, "proj", "run1")}
	rows = append(rows, Gap(Result{Fault: "I-002", Decl: *decl("R-0"), Reason: "absent", Land: land}, "proj", "run1"))

	b, err := FileGaps(rows)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("rows %d, want 3", len(lines))
	}
	// P1 (the fault-carrying R-0) sorts FIRST on the scale; the P2
	// coverage rows follow id-sorted.
	if !strings.Contains(lines[0], `"priority":"P1"`) || !strings.Contains(lines[0], `"id":"MSF-OBS-R-0"`) {
		t.Errorf("first row is not the fault-carrying P1: %s", lines[0])
	}
	for _, i := range []int{1, 2} {
		if !strings.Contains(lines[i], `"priority":"P2"`) {
			t.Errorf("row %d is not P2: %s", i, lines[i])
		}
	}
	if !strings.Contains(lines[1], `"id":"MSF-OBS-R-1"`) || !strings.Contains(lines[2], `"id":"MSF-OBS-R-2"`) {
		t.Errorf("P2 rows not id-sorted: %s | %s", lines[1], lines[2])
	}
	if _, err := FileGaps(nil); err != nil {
		t.Errorf("no gaps must emit nil, not an error: %v", err)
	}
}

// TestS2ReplayDeclarationOnCorpusExperiment (S-2, TRBL-031): the corpus
// experiment carries the observe declaration and the expect pair; the
// replay property the acceptance names — not_recovered pre-fix,
// recovered post-fix — is the graded verdict set the journal vocabulary
// can express for this case (pinned against the REAL ex-002 file,
// read-only), with the observe declaration riding the same file.
func TestS2ReplayDeclarationOnCorpusExperiment(t *testing.T) {
	exps, err := journal.LoadDir("../../catalog/experiments")
	if err != nil {
		t.Fatalf("LoadDir(corpus): %v", err)
	}
	var ex2 *journal.Experiment
	for _, e := range exps {
		if e.ID == "ex-002-redis-loss-recovery" {
			ex2 = e
		}
	}
	if ex2 == nil {
		t.Fatal("ex-002-redis-loss-recovery not in the corpus (S-2's experiment)")
	}
	if ex2.Observe == nil || ex2.Observe.Trouble == nil {
		t.Fatal("ex-002 declares no observe.trouble — the S-2 replay cannot assert detection")
	}
	tr := ex2.Observe.Trouble
	if tr.ExpectRecord != "TROUBLE-HUB-004" || tr.Namespace != "trouble" || tr.Within != "60s" {
		t.Errorf("observe declaration = %+v, want trouble/TROUBLE-HUB-004/60s", tr)
	}
	if err := ValidateDecl(tr); err != nil {
		t.Fatalf("corpus declaration refused: %v", err)
	}
	// The expect pair: the replay grades differ pre/post fix — exactly
	// the closed-vocabulary values S-2 names.
	want := map[string]string{"replay_pre_fix": "not_recovered", "replay_post_fix": "recovered"}
	for i, k := range ex2.Expect.Keys {
		if v, ok := want[k]; ok && ex2.Expect.Values[i] != v {
			t.Errorf("expect.%s = %q, want %q", k, ex2.Expect.Values[i], v)
		}
	}
}

// TestCorpusDeclarationRunsAgainstFakeLedger (S-2 × AC-14 together): the
// corpus declaration, driven against the fake ledger serving the expected
// record in-window, matches and names the line — the whole SPEC-12 path
// (real declaration → read-only poll → named match) exercised end to end.
func TestCorpusDeclarationRunsAgainstFakeLedger(t *testing.T) {
	exps, err := journal.LoadDir("../../catalog/experiments")
	if err != nil {
		t.Fatal(err)
	}
	var tr *journal.TroubleObserve
	for _, e := range exps {
		if e.ID == "ex-002-redis-loss-recovery" {
			tr = e.Observe.Trouble
		}
	}
	if tr == nil {
		t.Fatal("no declaration")
	}
	land := time.Unix(2000, 0)
	c := newCensus(t, []Line{{ID: tr.ExpectRecord, TS: land.Add(20 * time.Second).Unix()}})
	l, _ := c.client(250 * time.Millisecond)

	res, err := l.Assert("I-002", tr, land)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Matched || res.Line.ID != tr.ExpectRecord {
		t.Fatalf("corpus declaration did not match: %+v (%v)", res, err)
	}
	methods, _, _ := c.census()
	for _, m := range methods {
		if m != http.MethodGet {
			t.Errorf("method %s in the corpus-declaration run — read-only violated", m)
		}
	}
	if want := fmt.Sprintf("fault=I-002 detected=1 t_detect=%s", 20*time.Second); res.MatrixRow() != want {
		t.Errorf("matrix row %q, want %q", res.MatrixRow(), want)
	}
}
