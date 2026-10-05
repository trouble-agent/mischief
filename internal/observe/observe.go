// Package observe is SPEC-12's trouble integration: the read-only ledger
// assertion (docs/prd/mischief-v0.1.md §7 integration point 1, AC-14), the
// detection-gap row a miss files, and the verify-play v0.2 stub (§7
// integration point 4).
//
// The contract (PRD §7): "An experiment may declare 'the target's ledger
// must contain record R within N seconds'. Costs trouble nothing; turns its
// detection claims into a measured matrix (fault × detected ×
// time-to-detect)." The declaration half already exists —
// journal.ObserveDecl / journal.TroubleObserve (SPEC-02, MSF-003); this
// package is the assertion half: poll the ledger READ-ONLY until the
// declared window expires, name the matched ledger line in the journal's
// observe_result record, and file a detection-gap row on a miss.
//
// Read-only is structural, not a promise: the Ledger type has NO mutating
// methods — the only HTTP method it ever builds is GET, with a nil body,
// and nothing it returns can write. The tests pin this twice: a reflection
// sweep over the exported surface (no method named like a mutator) and a
// fake-ledger request census (every request observed was a GET; no POST is
// ever issued).
//
// The ledger WIRE SHAPE is the v0.1 scratch contract, defined here because
// no live shape is pinnable in this repo (trouble's real findings-ledger
// API is not wired in v0.1 — SPEC-12 marks the live wiring v0.2):
//
//	GET {base}/api/v1/namespaces/{namespace}/records?since={unix}&limit=1000
//	200 → JSON array of Line (bare array; any other top-level shape refuses)
//
// A record whose id equals the declared expect_record and whose ts falls in
// [land, land+window] is the match; time-to-detect is line.ts − land. A
// record with the right id but a ts after the window is a MISS whose reason
// names the lateness (detected-too-late is a detection gap, not a pass),
// and a record predating the land never matches (it is not this fault's
// record). Pagination beyond the since/limit parameters is v0.2, as is any
// auth header — the scratch shape assumes a read-open ledger.
//
// NOT-LIST (what SPEC-12 does not promise in v0.1):
//
//   - No live trouble instance is contacted by this build's tests or by
//     anything wired in-repo: the only ledger any test exercises is a
//     local httptest fake. The production wiring (which runner constructs
//     the Ledger from which experiment field) is the fleet-rollout step.
//   - No write path exists, not even behind a flag: observe never files
//     INTO trouble; a detection-gap row is an emitted artefact the
//     operator/foreman merge step files (battery's AC-18 precedent).
//   - verify-play is a refusal, not a partial engine: it names what is
//     absent and returns, under every option combination including
//     --allow-real (which arms the L5 real-provider plane, not missing
//     engines).
package observe

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/trouble-agent/mischief/internal/journal"
)

// ObserverName is the observer id every detection entry and gap row this
// package produces carries (the battery's Detector.Name value).
const ObserverName = "trouble-ledger"

// fetchLimit is the record cap per read (the scratch contract's page bound;
// pagination is v0.2).
const fetchLimit = 1000

// maxReadBytes bounds one ledger read (4 MiB) — a read-only client still
// refuses to swallow an unbounded response.
const maxReadBytes = 4 << 20

// Line is one trouble ledger line as the read API returns it. The shape is
// the v0.1 scratch contract (see the package doc); when trouble's live
// findings-ledger API lands (v0.2) the decode maps onto it and this struct
// is the seam that changes, not the assertion.
type Line struct {
	// ID is the ledger record id ("TROUBLE-HUB-004") — the value the
	// experiment's expect_record names.
	ID string `json:"id"`
	// TS is the record's unix timestamp (seconds).
	TS int64 `json:"ts"`
	// Kind is the record kind ("finding", "incident", "play") when the
	// ledger carries one.
	Kind string `json:"kind,omitempty"`
	// Fields carries record-specific detail verbatim.
	Fields map[string]string `json:"fields,omitempty"`
}

// Ledger is the READ-ONLY trouble ledger client. Construct it per
// experiment: BaseURL + Namespace from the deployment, Poll for the assert
// cadence. The type exposes no mutating method (pinned by test): the only
// request it builds is a GET with a nil body.
type Ledger struct {
	// BaseURL is the ledger's base URL ("http://127.0.0.1:7661") — the
	// records route is appended.
	BaseURL string
	// Namespace is the trouble namespace read ("trouble").
	Namespace string
	// HTTP is the transport seam; nil = http.DefaultClient. Tests inject
	// the httptest server's client.
	HTTP interface {
		Do(*http.Request) (*http.Response, error)
	}
	// Poll is the assert cadence; <= 0 means 250ms.
	Poll time.Duration
	// Now is the clock seam; nil = time.Now.
	Now func() time.Time
	// Sleep is the wait seam; nil = time.Sleep.
	Sleep func(time.Duration)
}

// Assert runs the observe.trouble assertion for one experiment's
// declaration: poll the ledger until the declared window expires, looking
// for the record whose id matches expect_record with ts in
// [land, land+window]. fault is the primitive id the experiment injected
// (carried into the result's matrix row; "" is legal). land is the moment
// the fault landed (the window's anchor).
//
// A hit returns Matched=true with the matched Line and the measured
// time-to-detect; the window expiring without an in-window match returns
// Matched=false with a reason naming the expectation (a detection gap —
// the caller files the row). An error means the CHECK could not run (bad
// declaration, unreachable ledger, refused shape) — an error is never a
// gap (an unexplained null is junk).
func (l *Ledger) Assert(fault string, decl *journal.TroubleObserve, land time.Time) (Result, error) {
	if err := ValidateDecl(decl); err != nil {
		return Result{}, err
	}
	window, err := time.ParseDuration(decl.Within)
	if err != nil {
		return Result{}, fmt.Errorf("observe: within: unparsable duration %q", decl.Within)
	}
	now, sleep := l.now(), l.sleep()
	deadline := land.Add(window)
	res := Result{
		Fault:   fault,
		Decl:    *decl,
		Land:    land,
		Elapsed: -1, // set for real below; -1 marks "not completed" on early error
	}
	var lateTS int64
	for {
		lines, err := l.fetch(land.Unix())
		if err != nil {
			return Result{}, err
		}
		res.Searched += len(lines)
		for _, ln := range lines {
			if ln.ID != decl.ExpectRecord {
				continue
			}
			if ln.TS < land.Unix() {
				continue // predates the fault: not this fault's record
			}
			if ln.TS > deadline.Unix() {
				if lateTS == 0 {
					lateTS = ln.TS // exists, but late: keep polling for an in-window instance
				}
				continue
			}
			res.Matched = true
			res.Line = ln
			res.TimeToDetect = time.Duration(ln.TS-land.Unix()) * time.Second
			res.Elapsed = now().Sub(land)
			res.Reason = fmt.Sprintf("matched within window (%s): ledger line %s at ts %d", window, ln.ID, ln.TS)
			return res, nil
		}
		if !now().Before(deadline) {
			break
		}
		sleep(l.poll())
	}
	res.Elapsed = now().Sub(land)
	switch {
	case lateTS > 0:
		res.Reason = fmt.Sprintf("expected record %q exists but at ts %d — after the %s window (late by %d s): detected too late is a gap",
			decl.ExpectRecord, lateTS, window, lateTS-deadline.Unix())
	default:
		res.Reason = fmt.Sprintf("expected record %q absent within %s (searched %d lines since land)", decl.ExpectRecord, window, res.Searched)
	}
	return res, nil
}

// fetch performs ONE read: GET {base}/api/v1/namespaces/{ns}/records with
// the since/limit parameters, decoding a bare JSON array of Line. Any
// other method is unreachable from here — the request is built with
// http.MethodGet and a nil body, pinned by the fake-ledger census test.
func (l *Ledger) fetch(since int64) ([]Line, error) {
	if l.BaseURL == "" {
		return nil, fmt.Errorf("observe: ledger: base URL empty")
	}
	if l.Namespace == "" {
		return nil, fmt.Errorf("observe: ledger: namespace empty")
	}
	u := strings.TrimRight(l.BaseURL, "/") +
		"/api/v1/namespaces/" + url.PathEscape(l.Namespace) +
		"/records?since=" + strconv.FormatInt(since, 10) +
		"&limit=" + strconv.Itoa(fetchLimit)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("observe: build read: %w", err)
	}
	resp, err := l.transport().Do(req)
	if err != nil {
		return nil, fmt.Errorf("observe: ledger read: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		// A missing namespace route is a wiring defect, not a
		// detection gap — the honest answer is "the check could not
		// run", never "checked, absent".
		return nil, fmt.Errorf("observe: ledger read: 404 on %s — namespace route not found (wiring defect, not a detection gap)", u)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("observe: ledger read: status %d from %s", resp.StatusCode, u)
	}
	var lines []Line
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReadBytes)).Decode(&lines); err != nil {
		return nil, fmt.Errorf("observe: ledger read: decode (want a bare JSON array of records): %w", err)
	}
	return lines, nil
}

// transport resolves the seam default.
func (l *Ledger) transport() interface {
	Do(*http.Request) (*http.Response, error)
} {
	if l.HTTP != nil {
		return l.HTTP
	}
	return http.DefaultClient
}

func (l *Ledger) now() func() time.Time {
	if l.Now != nil {
		return l.Now
	}
	return time.Now
}

func (l *Ledger) sleep() func(time.Duration) {
	if l.Sleep != nil {
		return l.Sleep
	}
	return time.Sleep
}

func (l *Ledger) poll() time.Duration {
	if l.Poll > 0 {
		return l.Poll
	}
	return 250 * time.Millisecond
}

// Result is the observe.trouble assertion's measured outcome (AC-14's
// evidence): fault × detected × time-to-detect, plus the matched ledger
// line the journal's observe_result record must name.
type Result struct {
	// Fault is the primitive id the experiment injected ("" when the
	// caller had none — e.g. a pure observe rehearsal).
	Fault string
	// Decl is the declaration the assertion ran (verbatim).
	Decl journal.TroubleObserve
	// Land is the landing moment the window was anchored to.
	Land time.Time
	// Matched reports whether the expected record appeared in-window.
	Matched bool
	// Line is the matched ledger line (zero when missed) — the AC-14
	// "the ledger line it matched".
	Line Line
	// TimeToDetect is line.ts − land (0 when missed).
	TimeToDetect time.Duration
	// Searched is the number of ledger lines examined across all polls.
	Searched int
	// Elapsed is the wall time the assertion held (land → decision).
	Elapsed time.Duration
	// Reason is "" never: on a hit it names the match; on a miss it is
	// the gap detail (an unexplained null is junk).
	Reason string
}

// MatrixRow renders the measured detection-matrix triple
// (fault × detected × time-to-detect) as stable text.
func (r Result) MatrixRow() string {
	detected := 0
	if r.Matched {
		detected = 1
	}
	return fmt.Sprintf("fault=%s detected=%d t_detect=%s", r.faultOrUnknown(), detected, r.TimeToDetect)
}

func (r Result) faultOrUnknown() string {
	if r.Fault == "" {
		return "unknown"
	}
	return r.Fault
}

// Record renders the result as the journal's observe_result record (AC-14's
// proof): matched=true carries ledger_line, ledger_ts and t_detect;
// matched=false carries the gap reason. Every field a reader might need is
// in the flat Fields map (the journal's record-specific detail convention).
func (r Result) Record(runID string, seq int, ts int64) journal.Record {
	f := map[string]string{
		"observer":      ObserverName,
		"namespace":     r.Decl.Namespace,
		"expect_record": r.Decl.ExpectRecord,
		"window":        r.Decl.Within,
		"matched":       strconv.FormatBool(r.Matched),
		"searched":      strconv.Itoa(r.Searched),
		"reason":        r.Reason,
		"matrix":        r.MatrixRow(),
	}
	if r.Fault != "" {
		f["fault"] = r.Fault
	}
	if r.Matched {
		f["ledger_line"] = r.Line.ID
		f["ledger_ts"] = strconv.FormatInt(r.Line.TS, 10)
		f["t_detect"] = r.TimeToDetect.String()
	}
	return journal.Record{
		Type:   journal.RecordObserveResult,
		RunID:  runID,
		Seq:    seq,
		TS:     ts,
		Fields: f,
	}
}

// ValidateDecl refuses a declaration the assertion could not run honestly:
// namespace, expect_record and within are all required, and within must
// parse as a Go duration. The refusal names the offending field.
func ValidateDecl(decl *journal.TroubleObserve) error {
	if decl == nil {
		return fmt.Errorf("observe: declaration: missing")
	}
	if decl.Namespace == "" {
		return fmt.Errorf("observe: namespace: missing")
	}
	if decl.ExpectRecord == "" {
		return fmt.Errorf("observe: expect_record: missing")
	}
	if decl.Within == "" {
		return fmt.Errorf("observe: within: missing")
	}
	if _, err := time.ParseDuration(decl.Within); err != nil {
		return fmt.Errorf("observe: within: unparsable duration %q", decl.Within)
	}
	return nil
}
