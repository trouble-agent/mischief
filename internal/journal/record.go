package journal

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RecordType is the journal record vocabulary of PRD §6.3. The record set is
// the run lifecycle (plan..verdict..close); a type a reader does not know is
// carried generically (Type + Fields) rather than dropped.
type RecordType string

const (
	// RecordRunPlanned: the resolved plan (no side effects, AC-1).
	RecordRunPlanned RecordType = "run_planned"
	// RecordFaultArmed: the fault actuator is armed (pre-land).
	RecordFaultArmed RecordType = "fault_armed"
	// RecordFaultLanded carries the landing PROOF.
	RecordFaultLanded RecordType = "fault_landed"
	// RecordProbeSample: one probe observation.
	RecordProbeSample RecordType = "probe_sample"
	// RecordAssertionResult: one probe/assertion grading.
	RecordAssertionResult RecordType = "assertion_result"
	// RecordObserveResult: the observe.trouble outcome (AC-14).
	RecordObserveResult RecordType = "observe_result"
	// RecordFaultReverted carries the revert PROOF (AC-4).
	RecordFaultReverted RecordType = "fault_reverted"
	// RecordVerdict carries the graded verdict value (AC-9).
	RecordVerdict RecordType = "verdict"
	// RecordRunClosed: the run's terminal record.
	RecordRunClosed RecordType = "run_closed"
	// RecordFinding: a finding filed from the run (AC-18 shape).
	RecordFinding RecordType = "finding"
)

// Valid reports whether t is one of the declared record types.
func (t RecordType) Valid() bool {
	switch t {
	case RecordRunPlanned, RecordFaultArmed, RecordFaultLanded, RecordProbeSample,
		RecordAssertionResult, RecordObserveResult, RecordFaultReverted,
		RecordVerdict, RecordRunClosed, RecordFinding:
		return true
	}
	return false
}

// Record is one journal line. Common fields are typed; record-specific
// detail lives in Fields (flat string map) so a reader never drops unknown
// keys — the AC-9 round trip needs Verdict and RunID exact, everything else
// is evidence.
type Record struct {
	// Type is the record type (one of the declared RecordType values).
	Type RecordType `json:"type"`
	// RunID is the content-derived run id this record belongs to.
	RunID string `json:"run_id"`
	// Seq is the run-local sequence number (0-based, append order).
	Seq int `json:"seq"`
	// TS is the record's unix timestamp (seconds).
	TS int64 `json:"ts"`
	// Verdict carries the graded verdict value; set only on
	// RecordVerdict/RecordRunClosed (AC-9's field).
	Verdict Verdict `json:"verdict,omitempty"`
	// Fields carries record-specific detail. Key order is canonicalised on
	// write (sorted) so identical records serialise identically.
	Fields map[string]string `json:"fields,omitempty"`
}

// validate checks the invariants a record must hold before it may be
// written: a known type, a run id, a non-negative seq, and (when a verdict
// is carried) a declared verdict value — the closed vocabulary is enforced
// at the write path, so an undeclared value cannot enter a journal.
func (r *Record) validate() error {
	if !r.Type.Valid() {
		return fmt.Errorf("type: unknown record type %q", r.Type)
	}
	if r.RunID == "" {
		return fmt.Errorf("run_id: missing")
	}
	if r.Seq < 0 {
		return fmt.Errorf("seq: negative")
	}
	hasVerdict := r.Type == RecordVerdict || r.Type == RecordRunClosed
	if hasVerdict && !r.Verdict.valid() {
		return fmt.Errorf("verdict: value %q is not in the closed vocabulary", r.Verdict)
	}
	if !hasVerdict && r.Verdict != "" {
		return fmt.Errorf("verdict: record type %s does not carry a verdict", r.Type)
	}
	return nil
}

// canonicalFields renders the fields map as sorted "k=v" pairs joined by
// unit separators — a stable text form used by tests and summaries (map
// iteration order is random; nothing derived from a Record may depend on it).
func (r *Record) canonicalFields() string {
	keys := make([]string, 0, len(r.Fields))
	for k := range r.Fields {
		keys = append(keys, k)
	}
	strings_ := keys // keep the dependency surface tiny
	_ = strings_
	sortStrings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+r.Fields[k])
	}
	return strings.Join(parts, "\x1f")
}

// sortStrings is insertion sort — the field counts per record are small and
// the package avoids importing sort for one caller.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Summary is the run-level structure a journal read builds (AC-9's final
// hop: journal → read → summary). The verdict value here must equal the
// verdict value written — byte for byte — and the fault set must match the
// declared, sorted set.
type Summary struct {
	// RunID is the run id every record carried.
	RunID string
	// Verdict is the graded verdict value from the verdict record.
	Verdict Verdict
	// RecordCount is the number of records read.
	RecordCount int
	// FaultSet is the resolved fault set (from run_planned), in the sorted
	// canonical form.
	FaultSet string
	// StartedAt / ClosedAt are the first/last record timestamps.
	StartedAt int64
	ClosedAt  int64
}

// journalLine is the wire form of one record. Fields is kept as a raw JSON
// message so the read side preserves key order in principle... (it does not:
// Go maps lose order — the read side therefore never relies on field order,
// only on the sorted canonical form).
type journalLine struct {
	Type    RecordType        `json:"type"`
	RunID   string            `json:"run_id"`
	Seq     int               `json:"seq"`
	TS      int64             `json:"ts"`
	Verdict Verdict           `json:"verdict,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// MarshalJSON renders the record with scrub applied LAST — the bytes this
// method returns are the bytes a journal file receives, so AC-20 holds no
// matter which field the credential-shaped material entered through.
func (r Record) MarshalJSON() ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	line := journalLine{
		Type:    r.Type,
		RunID:   r.RunID,
		Seq:     r.Seq,
		TS:      r.TS,
		Verdict: r.Verdict,
		Fields:  nil,
	}
	if len(r.Fields) > 0 {
		line.Fields = make(map[string]string, len(r.Fields))
		for k, v := range r.Fields {
			line.Fields[k] = scrub(v)
		}
	}
	// scrub the typed verdict path too: a verdict VALUE cannot carry a
	// credential shape (closed vocabulary), but the run id is free-form text
	// supplied by callers — same single pass, one rule set.
	line.RunID = scrub(line.RunID)
	b, err := json.Marshal(line)
	if err != nil {
		return nil, err
	}
	return scrubLine(b), nil
}

// scrubLine is the byte-level backstop over the fully serialised line (the
// JSON-encoded map may still contain a shape the per-field pass missed —
// e.g. a key NAME carrying a secret-like token). It rewrites the line bytes
// in place semantics: same rule set, applied to the whole line.
func scrubLine(b []byte) []byte {
	s := scrub(string(b))
	return []byte(s)
}

// UnmarshalJSON reads a record back. Validation is read-side too: an unknown
// type or an out-of-vocabulary verdict fails the read loudly rather than
// flowing into a summary (fail closed, catalog precedent).
func (r *Record) UnmarshalJSON(b []byte) error {
	var line journalLine
	if err := json.Unmarshal(b, &line); err != nil {
		return err
	}
	r.Type = line.Type
	r.RunID = line.RunID
	r.Seq = line.Seq
	r.TS = line.TS
	r.Verdict = line.Verdict
	r.Fields = line.Fields
	return r.validate()
}

// encode renders the record as one journal line (no trailing newline).
func (r *Record) encode() ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return b, nil
}
