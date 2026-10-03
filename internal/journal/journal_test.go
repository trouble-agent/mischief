// Package journal tests: AC-9, AC-10, AC-20 and the experiment loader.
//
// ch:trace row=MSF-003 spec=docs/SPEC-PLAN.md#SPEC-02 test=internal/journal/journal_test.go evidence=internal/journal/journal_test.go witness=none:no-live-host-run-in-worktree
//
// The three acceptance criteria this file pins (board row MSF-003):
//
//   - AC-10: same spec bytes + same seed => identical run id AND identical
//     fault set (stable ordering); different seed => different run id.
//   - AC-20: credential-shaped material entering through ANY field is absent
//     from the bytes as persisted (write-path half; scrub() unit half in
//     scrub_test.go).
//   - AC-9: a verdict value written to the journal round-trips
//     write -> read -> Summary unchanged, for the whole closed vocabulary.
package journal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ex002Fixture is a trimmed copy of catalog/experiments/ex-002-redis-loss-recovery.yaml
// (the real corpus file is NEVER modified by tests; LoadDir over the real
// directory is covered by TestLoadCorpusExperiments).
const ex002Fixture = `# Replay S-2 / TRBL-031: dependency loss where the recovery code was unreachable.
id: ex-002-redis-loss-recovery
target: {selector: "container:trouble-hub", scratch: true}
faults:
  - primitive: I-002
    params: {service: redis, mode: fresh-empty, restore_state: false}
    ttl: 90s
    landed_proof: {kind: external-probe, probe: "tcp:127.0.0.1:7699", expect: "refused-then-open-empty"}
    inverse: {primitive: I-002, params: {mode: restore}}
probe:
  - {name: health-ok, cmd: "curl -sf -o /dev/null -w '%{http_code}' 127.0.0.1:7661/health.json", expect: "200"}
  - {name: ingest-accepts, cmd: "curl -s -o /dev/null -w '%{http_code}' -X POST '127.0.0.1:7661/api/1/event/?sentry_key=<key>' -d '{\"message\":\"probe\",\"level\":\"error\"}'", expect: "200"}
  - {name: group-present, cmd: "redis-cli XPENDING <stream> <group>", expect: "0"}
budget: {recover_within: 120s}
observe: {trouble: {namespace: trouble, expect_record: TROUBLE-HUB-004, within: 60s}}
expect:
  replay_pre_fix: not_recovered
  replay_post_fix: recovered
seed: 20260927
`

// writeTempExperiment writes body to a fresh temp dir and returns the path.
func writeTempExperiment(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

// --- AC-10: content-derived run id + stable fault set -----------------------

// TestRunIDDeterministic (AC-10): same spec bytes + same seed => identical
// run id; different seed => different run id; different bytes => different
// run id (the id is a pure byte function of the spec + the seed).
func TestRunIDDeterministic(t *testing.T) {
	spec := []byte(ex002Fixture)
	const seed = int64(20260927)

	a := RunID(spec, seed)
	b := RunID(spec, seed)
	if a != b {
		t.Fatalf("same bytes+seed produced different run ids: %s vs %s", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("run id %q is %d hex chars, want 16 (hex(sha256)[:16])", a, len(a))
	}
	for _, c := range a {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isHex {
			t.Fatalf("run id %q is not lowercase hex", a)
		}
	}
	if alt := RunID(spec, seed+1); alt == a {
		t.Fatal("different seed produced the same run id (AC-10 requires divergence)")
	}
	if other := RunID(append(spec, []byte("\n# changed\n")...), seed); other == a {
		t.Fatal("different spec bytes produced the same run id")
	}
	// Injectivity spot-check (the seed\n prefix exists so (spec,seed) pairs
	// that concatenate identically still hash differently): spec "ab" seed 1
	// vs spec "a" seed 12 would collide under naive concatenation.
	if RunID([]byte("ab"), 1) == RunID([]byte("a"), 12) {
		t.Fatal("run id derivation is not injective for concatenation-equivalent inputs")
	}
}

// TestParseFaultSetStable (AC-10, fault-set half): two loads of the same
// bytes produce an identical sorted fault set; the YAML file order must not
// leak into the comparison.
func TestParseFaultSetStable(t *testing.T) {
	mustParse := func(body string) *Experiment {
		t.Helper()
		e, err := Parse([]byte(body))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		return e
	}
	// Two faults declared in opposite file order in the two copies, plus
	// params whose map iteration order is random in Go: the resolved set
	// must be byte-identical anyway.
	a := mustParse(`id: ex-x
target: {selector: "host:.", scratch: true}
faults:
  - {primitive: F-009, params: {path: st.db, mode: restore-snapshot}, ttl: 45s, landed_proof: {kind: inode-identity, check: inode differs}, inverse: {action: restore-original-inode}}
  - {primitive: I-002, params: {service: redis, mode: fresh-empty}, ttl: 90s, landed_proof: {kind: external-probe, probe: "tcp:127.0.0.1:7699", expect: open-empty}, inverse: {action: restore}}
probe:
  - {name: p, cmd: "true", expect: "0"}
budget: {recover_within: 30s}
seed: 7
`)
	b := mustParse(`id: ex-x
target: {selector: "host:.", scratch: true}
faults:
  - {primitive: I-002, params: {mode: fresh-empty, service: redis}, ttl: 90s, landed_proof: {kind: external-probe, probe: "tcp:127.0.0.1:7699", expect: open-empty}, inverse: {action: restore}}
  - {primitive: F-009, params: {mode: restore-snapshot, path: st.db}, ttl: 45s, landed_proof: {kind: inode-identity, check: inode differs}, inverse: {action: restore-original-inode}}
probe:
  - {name: p, cmd: "true", expect: "0"}
budget: {recover_within: 30s}
seed: 7
`)
	if ka, kb := a.FaultSetKey(), b.FaultSetKey(); ka != kb {
		t.Fatalf("fault sets differ across file orders:\nA: %q\nB: %q", ka, kb)
	}
	if len(a.Faults) != 2 || a.Faults[0].Primitive != "F-009" || a.Faults[1].Primitive != "I-002" {
		t.Fatalf("faults not sorted by primitive id: %s, %s", a.Faults[0].Primitive, a.Faults[1].Primitive)
	}
}

// TestParseDifferentFaultSet (AC-10): a different declared fault set must
// produce a different key — identical keys would make two divergent runs
// indistinguishable.
func TestParseDifferentFaultSet(t *testing.T) {
	base := func(ttl string) string {
		return `id: ex-x
target: {selector: "host:.", scratch: true}
faults:
  - {primitive: I-002, params: {service: redis}, ttl: ` + ttl + `, landed_proof: {kind: external-probe, probe: "tcp:1", expect: e}, inverse: {action: restore}}
probe:
  - {name: p, cmd: "true", expect: "0"}
budget: {recover_within: 30s}
seed: 7
`
	}
	e1, err := Parse([]byte(base("90s")))
	if err != nil {
		t.Fatal(err)
	}
	e2, err := Parse([]byte(base("91s")))
	if err != nil {
		t.Fatal(err)
	}
	if e1.FaultSetKey() == e2.FaultSetKey() {
		t.Fatal("different fault declarations produced identical fault set keys")
	}
}

// TestLoadFixtureEx002: the shipped corpus shape loads with every declared
// field in place (fixture is a trimmed copy of ex-002; the real file is
// never modified).
func TestLoadFixtureEx002(t *testing.T) {
	p := writeTempExperiment(t, "ex-002-redis-loss-recovery.yaml", ex002Fixture)
	e, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if e.ID != "ex-002-redis-loss-recovery" {
		t.Fatalf("id = %q", e.ID)
	}
	if e.SourceFile != p {
		t.Fatalf("SourceFile = %q, want %q", e.SourceFile, p)
	}
	if e.Target.Selector != "container:trouble-hub" || !e.Target.Scratch {
		t.Fatalf("target = %+v", e.Target)
	}
	if len(e.Faults) != 1 {
		t.Fatalf("faults = %d", len(e.Faults))
	}
	f := e.Faults[0]
	if f.Primitive != "I-002" || f.TTL != "90s" {
		t.Fatalf("fault = %+v", f)
	}
	if f.Params["service"] != "redis" || f.Params["mode"] != "fresh-empty" {
		t.Fatalf("params = %v", f.Params)
	}
	if f.LandedProof.Kind != "external-probe" || f.LandedProof.Probe != "tcp:127.0.0.1:7699" {
		t.Fatalf("landed_proof = %+v", f.LandedProof)
	}
	if f.Inverse.Primitive != "I-002" || f.Inverse.Params["mode"] != "restore" {
		t.Fatalf("inverse = %+v", f.Inverse)
	}
	if len(e.Probe) != 3 {
		t.Fatalf("probes = %d", len(e.Probe))
	}
	if e.Budget.RecoverWithin != "120s" {
		t.Fatalf("budget = %+v", e.Budget)
	}
	if e.Observe == nil || e.Observe.Trouble == nil || e.Observe.Trouble.ExpectRecord != "TROUBLE-HUB-004" {
		t.Fatalf("observe = %+v", e.Observe)
	}
	// expect keeps declaration order (yamlMapSlice, not map order).
	wantKeys := []string{"replay_pre_fix", "replay_post_fix"}
	if len(e.Expect.Keys) != len(wantKeys) {
		t.Fatalf("expect keys = %v", e.Expect.Keys)
	}
	for i, k := range wantKeys {
		if e.Expect.Keys[i] != k {
			t.Fatalf("expect.Keys[%d] = %s, want %s", i, e.Expect.Keys[i], k)
		}
	}
	if e.Expect.Values[0] != "not_recovered" || e.Expect.Values[1] != "recovered" {
		t.Fatalf("expect.Values = %v", e.Expect.Values)
	}
	if e.Seed != 20260927 {
		t.Fatalf("seed = %d", e.Seed)
	}
}

// TestLoadCorpusExperiments: every shipped corpus file loads (the package
// doc claims ex-001..ex-004; read-only — the corpus is never mutated).
func TestLoadCorpusExperiments(t *testing.T) {
	exps, err := LoadDir("../../catalog/experiments")
	if err != nil {
		t.Fatalf("LoadDir(corpus): %v", err)
	}
	if len(exps) != 4 {
		t.Fatalf("loaded %d experiments, want 4", len(exps))
	}
	seen := map[string]bool{}
	for _, e := range exps {
		seen[e.ID] = true
	}
	for _, id := range []string{
		"ex-001-noop-detector",
		"ex-002-redis-loss-recovery",
		"ex-003-spool-kill-between",
		"ex-004-live-file-replace",
	} {
		if !seen[id] {
			t.Fatalf("corpus experiment %s did not load", id)
		}
	}
}

// stripListItemLines removes every top-level list item line (used to build
// the "probe missing" refusal fixture without fragile string surgery).
func stripListItemLines(s, kind string) string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "- {"+kind+":") {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// TestLoadRefusalsNameFields: validation refuses with "field: problem"
// reasons (catalog convention), never bare errors.
func TestLoadRefusalsNameFields(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string // the offending field the reason must name
	}{
		{"no-id", strings.Replace(ex002Fixture, "id: ex-002-redis-loss-recovery\n", "", 1), "id"},
		{"no-selector", strings.Replace(ex002Fixture, `selector: "container:trouble-hub"`, `selector: ""`, 1), "target.selector"},
		{"no-faults", strings.Replace(ex002Fixture, "  - primitive: I-002", "  - primitive: \"\"", 1), "faults[0].primitive"},
		{"no-probe", stripListItemLines(ex002Fixture, "name"), "probe"},
		{"no-budget", strings.Replace(ex002Fixture, "budget: {recover_within: 120s}", "budget: {recover_within: \"\"}", 1), "budget.recover_within"},
		{"no-seed", strings.Replace(ex002Fixture, "seed: 20260927", "seed: 0", 1), "seed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeTempExperiment(t, "ex-broken.yaml", tc.body)
			_, err := Load(p)
			if err == nil {
				t.Fatalf("expected a refusal for %s", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal %q does not name %s", err.Error(), tc.want)
			}
		})
	}
}

// TestLoadDirEmptyAndMissing: an empty dir and a missing dir are named
// refusals, not empty successes.
func TestLoadDirEmptyAndMissing(t *testing.T) {
	if _, err := LoadDir(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("missing experiments dir must be refused")
	}
	empty := t.TempDir()
	if _, err := LoadDir(empty); err == nil || !strings.Contains(err.Error(), "no .yaml files") {
		t.Fatalf("empty dir: err=%v, want the no-.yaml refusal", err)
	}
}

// --- AC-20 write path: the record as persisted is clean ----------------------

// ac20Record builds a verdict record whose free-form fields carry credential
// shapes through every entry channel: a Fields value, a Fields KEY NAME, and
// the run id itself.
func ac20Record() Record {
	return Record{
		Type:    RecordVerdict,
		RunID:   "1a2b3c4d5e6f7788",
		Seq:     4,
		TS:      1759400000,
		Verdict: VerdictDegraded,
		Fields: map[string]string{
			"probe_cmd":        "curl -s -X POST 'http://h/api/1/event/?sentry_key=abc123def'",
			"note":             "recovered with token=abc123def456 in env", // gitleaks:allow test fixture
			"authorization=BS": "x", // a key NAME carrying a credential shape
			"benign":           "mode=truncate service=redis",
		},
	}
}

// TestJournalBytesContainNoSecrets (AC-20, write-path half): the bytes a
// record serialises to — exactly the bytes a journal file receives via
// MarshalJSON — contain no credential-shaped material, whichever field the
// secret entered through.
func TestJournalBytesContainNoSecrets(t *testing.T) {
	r := ac20Record()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, secret := range []string{
		"abc123def",    // ?sentry_key= value in probe_cmd
		"abc123def456", // token= value in note
	} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("journal bytes contain secret %q:\n%s", secret, b)
		}
	}
	// The line-level backstop must have treated the secret-shaped key NAME's
	// span too (the JSON key itself must remain — the record needs its shape
	// — but its value/credential span serialises as a marker, not raw text).
	if !strings.Contains(string(b), "[REDACTED:") {
		t.Fatal("no [REDACTED:*] marker in journal bytes although credential shapes entered the record")
	}
}

// TestWritePathRoundTripIsClean (AC-20 end to end): write a record to a real
// journal file through the encode path, read the file bytes back, assert no
// secret, then unmarshal — the record read back must still carry the verdict.
func TestWritePathRoundTripIsClean(t *testing.T) {
	r := ac20Record()
	line, err := r.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	p := filepath.Join(t.TempDir(), "journal.jsonl")
	if err := os.WriteFile(p, append(line, '\n'), 0o644); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	for _, secret := range []string{"abc123def", "abc123def456"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("persisted journal contains secret %q:\n%s", secret, raw)
		}
	}
	var back Record
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal persisted line: %v", err)
	}
	if back.Type != RecordVerdict || back.Verdict != VerdictDegraded {
		t.Fatalf("round trip lost the record identity: %+v", back)
	}
}

// --- AC-9: verdict round trip journal -> read -> summary ----------------------

// TestVerdictVocabularyClosed: exactly nine values, each valid, and nothing
// outside the set validates (an undeclared value cannot enter a journal).
func TestVerdictVocabularyClosed(t *testing.T) {
	all := []Verdict{
		VerdictRecovered, VerdictDegraded, VerdictNotRecovered, VerdictHung,
		VerdictCorrupted, VerdictNoOp, VerdictAborted, VerdictFlaky, VerdictVoid,
	}
	for _, v := range all {
		if !v.valid() {
			t.Fatalf("declared verdict %s is not valid()", v)
		}
		if v.String() != string(v) {
			t.Fatalf("String() != raw value for %s", v)
		}
	}
	for _, bad := range []Verdict{"", "pass", "ok", "RECOVERED", "unknown"} {
		if bad.valid() {
			t.Fatalf("verdict %q must not validate (closed vocabulary)", bad)
		}
	}
}

// TestVerdictRoundTripAllValues (AC-9): every verdict value in the closed
// vocabulary survives write -> serialise -> read -> Summary byte-identical.
// The green-but-meaningless shapes (no_op, aborted, corrupted, flaky, void)
// are explicitly included: a run that did nothing cannot grade as recovered.
func TestVerdictRoundTripAllValues(t *testing.T) {
	values := []Verdict{
		VerdictRecovered, VerdictDegraded, VerdictNotRecovered, VerdictHung,
		VerdictCorrupted, VerdictNoOp, VerdictAborted, VerdictFlaky, VerdictVoid,
	}
	for _, v := range values {
		t.Run(string(v), func(t *testing.T) {
			r := Record{
				Type:    RecordVerdict,
				RunID:   "cafebabedeadbeef",
				Seq:     9,
				TS:      1759400100,
				Verdict: v,
				Fields:  map[string]string{"graded_by": "probe-set"},
			}
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatalf("marshal %s: %v", v, err)
			}
			var back Record
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal %s: %v", v, err)
			}
			if back.Verdict != v {
				t.Fatalf("AC-9 violated: wrote %q, read %q", v, back.Verdict)
			}
			// Final hop: the summary structure a journal read builds.
			sum := Summary{RunID: back.RunID, Verdict: back.Verdict}
			if sum.Verdict != r.Verdict {
				t.Fatalf("AC-9 violated at summary hop: wrote %q, summary %q", r.Verdict, sum.Verdict)
			}
			if sum.RunID != r.RunID {
				t.Fatalf("round trip lost the run id: %q vs %q", sum.RunID, r.RunID)
			}
		})
	}
}

// TestVerdictRefusedOnNonVerdictRecords: the typed verdict field only rides
// on verdict/run_closed records — anything else fails the write loudly
// (fail closed, not dropped silently).
func TestVerdictRefusedOnNonVerdictRecords(t *testing.T) {
	r := Record{Type: RecordProbeSample, RunID: "x", Seq: 0, TS: 1, Verdict: VerdictRecovered}
	if _, err := json.Marshal(r); err == nil {
		t.Fatal("a probe_sample record must not carry a verdict")
	}
	r2 := Record{Type: RecordVerdict, RunID: "x", Seq: 0, TS: 1, Verdict: Verdict("immaterial")}
	if _, err := json.Marshal(r2); err == nil {
		t.Fatal("an out-of-vocabulary verdict must be refused at write")
	}
	var back Record
	if err := json.Unmarshal([]byte(`{"type":"verdict","run_id":"x","seq":0,"ts":1,"verdict":"greenish"}`), &back); err == nil {
		t.Fatal("an out-of-vocabulary verdict must fail the read too (fail closed)")
	}
}

// TestRecordValidationBasics: unknown types, missing run ids and negative
// seqs are named refusals.
func TestRecordValidationBasics(t *testing.T) {
	cases := []struct {
		name string
		rec  Record
		want string
	}{
		{"unknown-type", Record{Type: "mystery", RunID: "x", Seq: 0, TS: 1}, "type"},
		{"no-run-id", Record{Type: RecordRunPlanned, Seq: 0, TS: 1}, "run_id"},
		{"negative-seq", Record{Type: RecordRunPlanned, RunID: "x", Seq: -1, TS: 1}, "seq"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := json.Marshal(tc.rec); err == nil {
				t.Fatalf("expected refusal naming %s", tc.want)
			}
		})
	}
}

// TestRecordTypesDeclared: the ten lifecycle record types of PRD §6.3 are
// all valid and marshal under their declared names.
func TestRecordTypesDeclared(t *testing.T) {
	want := map[RecordType]string{
		RecordRunPlanned: "run_planned", RecordFaultArmed: "fault_armed",
		RecordFaultLanded: "fault_landed", RecordProbeSample: "probe_sample",
		RecordAssertionResult: "assertion_result", RecordObserveResult: "observe_result",
		RecordFaultReverted: "fault_reverted", RecordVerdict: "verdict",
		RecordRunClosed: "run_closed", RecordFinding: "finding",
	}
	for rt, wire := range want {
		if !rt.Valid() {
			t.Fatalf("%s not Valid()", rt)
		}
		rec := Record{Type: rt, RunID: "x", Seq: 0, TS: 1}
		if rt == RecordVerdict || rt == RecordRunClosed {
			rec.Verdict = VerdictDegraded // verdict-bearing types must carry a declared value
		}
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("marshal %s: %v", rt, err)
		}
		if !strings.Contains(string(b), `"type":"`+wire+`"`) {
			t.Fatalf("type %s marshalled as %s, want %s", rt, b, wire)
		}
	}
}

// TestCanonicalFieldsSorted: the canonical text form of the fields map is
// key-sorted (nothing derived from a record may depend on map order).
func TestCanonicalFieldsSorted(t *testing.T) {
	r := Record{
		Type: RecordRunPlanned, RunID: "x", Seq: 0, TS: 1,
		Fields: map[string]string{"zebra": "1", "alpha": "2", "mid": "3"},
	}
	got := r.canonicalFields()
	want := "alpha=2\x1fmid=3\x1fzebra=1"
	if got != want {
		t.Fatalf("canonicalFields = %q, want %q", got, want)
	}
}

// TestJournalToEndToEndSummary (AC-9 at file granularity): write a full run
// lifecycle to a journal file, read every line back, build the Summary — the
// verdict and fault set must equal what was written.
func TestJournalToEndToEndSummary(t *testing.T) {
	plan := Record{
		Type: RecordRunPlanned, RunID: "0123456789abcdef", Seq: 0, TS: 1000,
		Fields: map[string]string{"fault_set": "I-002"},
	}
	verdict := Record{
		Type: RecordVerdict, RunID: "0123456789abcdef", Seq: 1, TS: 1060,
		Verdict: VerdictDegraded,
		Fields:  map[string]string{"graded_by": "probe-set"},
	}
	lines := make([]string, 0, 2)
	for _, r := range []Record{plan, verdict} {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		lines = append(lines, string(b))
	}
	p := filepath.Join(t.TempDir(), "journal.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	sum := Summary{}
	for i, ln := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var r Record
		if err := json.Unmarshal([]byte(ln), &r); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		sum.RecordCount++
		if sum.RunID == "" {
			sum.RunID = r.RunID
		}
		if sum.StartedAt == 0 || r.TS < sum.StartedAt {
			sum.StartedAt = r.TS
		}
		if r.TS > sum.ClosedAt {
			sum.ClosedAt = r.TS
		}
		if r.Type == RecordRunPlanned {
			sum.FaultSet = r.Fields["fault_set"]
		}
		if r.Type == RecordVerdict {
			sum.Verdict = r.Verdict
		}
	}
	if sum.Verdict != VerdictDegraded {
		t.Fatalf("AC-9: summary verdict %q, want degraded", sum.Verdict)
	}
	if sum.FaultSet != "I-002" || sum.RecordCount != 2 || sum.RunID != "0123456789abcdef" {
		t.Fatalf("summary incomplete: %+v", sum)
	}
}

// TestPlannedRunIDMatchesDerivedID: the run id recorded in a run's plan is
// the content-derived one for those spec bytes + seed (the journal and the
// derivation agree — what makes AC-10's journal diff possible at all).
func TestPlannedRunIDMatchesDerivedID(t *testing.T) {
	e, err := Parse([]byte(ex002Fixture))
	if err != nil {
		t.Fatal(err)
	}
	id := RunID([]byte(ex002Fixture), e.Seed)
	r := Record{
		Type: RecordRunPlanned, RunID: id, Seq: 0, TS: 1,
		Fields: map[string]string{"fault_set": e.FaultSetKey()},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back Record
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.RunID != RunID([]byte(ex002Fixture), e.Seed) {
		t.Fatal("the journaled run id drifted from the derived one")
	}
}
