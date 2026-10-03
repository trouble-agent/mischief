package reverter

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// RecordType is the reverter journal's record vocabulary. It mirrors
// internal/journal's record style (typed constants + closed vocabulary
// validated at the write path) with the lifecycle records SPEC-04 owns.
type RecordType string

const (
	// RecordArm is the WRITE-AHEAD record: the hold (fault, inverse, check,
	// ttl) is on disk and fsynced before Land may be called. It carries the
	// declared forms verbatim in Payload so a detached owner process (a
	// different pid) can execute the inverse and the check from journal
	// bytes alone.
	RecordArm RecordType = "arm"
	// RecordLand records that the caller landed the fault (post write-ahead),
	// naming the pid that landed it.
	RecordLand RecordType = "land"
	// RecordLandRefused records a Land refusal: the write-ahead line for the
	// hold was not durable — the inverse would not predate the fault.
	RecordLandRefused RecordType = "land_refused"
	// RecordHoldExpired records the TTL boundary observed by a TTL owner
	// (ownpid set; boot reconcile records it with ownpid absent).
	RecordHoldExpired RecordType = "hold_expired"
	// RecordRevertProof is the MEASURED revert outcome (AC-4): fault id,
	// measured check, outcome (reverted|revert_failed), duration, who, pid.
	RecordRevertProof RecordType = "revert_proof"
	// RecordHoldStuck marks a non-terminal hold inherited from a previous
	// boot (boot reconcile, AC-6's "leftover faults from a previous boot are
	// listed stuck before the call").
	RecordHoldStuck RecordType = "hold_stuck"
)

// Valid reports whether t is one of the declared record types (the
// vocabulary is closed and enforced at the write path).
func (t RecordType) Valid() bool {
	switch t {
	case RecordArm, RecordLand, RecordLandRefused, RecordHoldExpired,
		RecordRevertProof, RecordHoldStuck:
		return true
	}
	return false
}

// Outcome is the closed outcome vocabulary of a revert proof. "reverted" is
// only ever written from a MEASURED check (AC-4); a failed or impossible
// measurement is revert_failed.
type Outcome string

const (
	// OutcomeReverted: the inverse executed AND the measured check passed.
	OutcomeReverted Outcome = "reverted"
	// OutcomeRevertFailed: the inverse failed to execute, or the measured
	// check failed, or the check could not be evaluated — the fault may
	// still be live; the proof carries the measured detail.
	OutcomeRevertFailed Outcome = "revert_failed"
)

// Valid reports whether o is a declared outcome value.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomeReverted, OutcomeRevertFailed:
		return true
	}
	return false
}

// Role is who executed a revert: the API in-process, the detached TTL owner
// process, or the kill switch.
type Role string

const (
	// RoleAPI: the in-process Revert call.
	RoleAPI Role = "api"
	// RoleOwner: the detached TTL owner process (survives the CLI).
	RoleOwner Role = "owner"
	// RoleKillSwitch: the RevertAll kill switch path.
	RoleKillSwitch Role = "kill_switch"
)

// Valid reports whether r is a declared role value.
func (r Role) Valid() bool {
	switch r {
	case RoleAPI, RoleOwner, RoleKillSwitch:
		return true
	}
	return false
}

// Decl is a declarative action or check: Kind + Params as flat strings, so a
// DETACHED process can execute it from journal bytes alone (no closures, no
// back-references into the arming process — the owner is a separate pid by
// design, AC-5).
type Decl struct {
	// Kind is the action/check kind. Inverse kinds: restore-file,
	// remove-file. Check kinds: file-matches-backup, file-absent. An unknown
	// kind fails CLOSED: an unknown inverse refuses to execute (the revert
	// grades revert_failed); an unknown check grades revert_failed.
	Kind string `json:"kind"`
	// Params are the kind's parameters ("path", "backup" ...).
	Params map[string]string `json:"params,omitempty"`
}

// declText is Decl's canonical text form (sorted k=v, unit-separated) —
// stable across map iteration, so content-derived ids and comparisons never
// depend on serialisation order.
func (d Decl) declText() string {
	keys := make([]string, 0, len(d.Params))
	for k := range d.Params {
		keys = append(keys, k)
	}
	sortStrings(keys)
	parts := make([]string, 0, len(keys)+1)
	parts = append(parts, d.Kind)
	for _, k := range keys {
		parts = append(parts, k+"="+d.Params[k])
	}
	return canonicalJoin(parts...)
}

// Validate checks a Decl's invariants: a kind, and no empty param keys or
// values (an empty path is how a "restore" that touches nothing would pass).
func (d Decl) Validate() error {
	if d.Kind == "" {
		return fmt.Errorf("kind: missing")
	}
	for k, v := range d.Params {
		if k == "" {
			return fmt.Errorf("params: empty key")
		}
		if v == "" {
			return fmt.Errorf("params.%s: empty value", k)
		}
	}
	return nil
}

// durationNanos is a duration carried as int64 nanoseconds (JSON has no
// duration type; ns keeps the value exact).
type durationNanos int64

// Hold is one armed fault: the fault's declared id, its target (informational
// here — the fault lands on it), the WRITE-AHEAD inverse, the measured
// post-revert check, and the TTL.
type Hold struct {
	// FaultID is the declared fault id (primitive or experiment-local id).
	FaultID string `json:"fault_id"`
	// Target is the declared target selector the fault lands on.
	Target string `json:"target"`
	// Inverse is the write-ahead undo (SPEC-04: recorded before the fault
	// lands).
	Inverse Decl `json:"inverse"`
	// Check is the measured post-revert probe (AC-4: measured, not asserted).
	Check Decl `json:"check"`
	// TTL bounds how long the fault may be held.
	TTL time.Duration `json:"-"`
}

// Validate checks a Hold's invariants (naming the offending field — the
// catalog precedent): named fault, non-empty target, valid non-empty inverse
// and check decls, positive TTL.
func (h *Hold) Validate() error {
	if h.FaultID == "" {
		return fmt.Errorf("fault_id: missing")
	}
	if h.Target == "" {
		return fmt.Errorf("target: missing")
	}
	if err := h.Inverse.Validate(); err != nil {
		return fmt.Errorf("inverse: %w", err)
	}
	if err := h.Check.Validate(); err != nil {
		return fmt.Errorf("check: %w", err)
	}
	if h.TTL <= 0 {
		return fmt.Errorf("ttl: must be positive")
	}
	return nil
}

// ID is the hold's content-derived id (hex(sha256)[:16], the journal RunID
// convention): a pure function of the declared content — fault, target, TTL
// in nanoseconds, and both decls' canonical text.
func (h *Hold) ID() string {
	return HoldID(h.FaultID, h.Target, int64(h.TTL), h.Inverse.declText(), h.Check.declText())
}

// armPayload is the write-ahead record's declared content — the full hold,
// verbatim, plus the fields boot reconcile and Status need: the arming
// process's boot id and pid. A detached owner (another pid) reads THIS to
// execute the inverse and the check from journal bytes alone.
type armPayload struct {
	FaultID string            `json:"fault_id"`
	Target  string            `json:"target"`
	Inverse Decl              `json:"inverse"`
	Check   Decl              `json:"check"`
	TTL     durationNanos     `json:"ttl"`
	BootID  string            `json:"boot_id"`
	ArmPID  int               `json:"arm_pid,omitempty"`
	Extra   map[string]string `json:"extra,omitempty"`
}

// record is one journal line: the typed envelope plus the flat field map
// (internal/journal's Record shape, reverter vocabulary) and — for arm
// records — the embedded write-ahead payload. One JSON object per line.
type record struct {
	// Type is the record type (closed vocabulary).
	Type RecordType `json:"type"`
	// HoldID is the content-derived hold id every record belongs to.
	HoldID string `json:"hold_id"`
	// TS is the record's unix timestamp (seconds).
	TS int64 `json:"ts"`
	// BootID identifies the boot that wrote the record (boot reconcile's
	// discriminator).
	BootID string `json:"boot_id"`
	// OwnPID is the pid of the process that EXECUTED the thing the record
	// describes (the reverter pid for owner-executed reverts — AC-5's
	// "written by the reverter's own pid"; the landing pid for land).
	OwnPID int `json:"own_pid,omitempty"`
	// Outcome carries the revert outcome on revert_proof records.
	Outcome Outcome `json:"outcome,omitempty"`
	// Payload is the write-ahead hold declaration; set only on arm records.
	Payload *armPayload `json:"payload,omitempty"`
	// Fields carries record-specific detail. Values are scrubbed on write.
	Fields map[string]string `json:"fields,omitempty"`
}

// validate checks the invariants a record must hold before it may be
// written: a known type, a hold id, a boot id, payload only on arm (and
// always on arm), an outcome only on revert_proof (and a declared value
// there) — the closed vocabularies are enforced at the write path so an
// undeclared value cannot enter a journal.
func (r *record) validate() error {
	if !r.Type.Valid() {
		return fmt.Errorf("type: unknown record type %q", r.Type)
	}
	if r.HoldID == "" {
		return fmt.Errorf("hold_id: missing")
	}
	if r.BootID == "" {
		return fmt.Errorf("boot_id: missing")
	}
	if r.Type == RecordArm && r.Payload == nil {
		return fmt.Errorf("payload: arm record requires the write-ahead payload")
	}
	if r.Type != RecordArm && r.Payload != nil {
		return fmt.Errorf("payload: record type %s does not carry a payload", r.Type)
	}
	if r.Outcome != "" && r.Type != RecordRevertProof {
		return fmt.Errorf("outcome: record type %s does not carry an outcome", r.Type)
	}
	if r.Type == RecordRevertProof && !r.Outcome.Valid() {
		return fmt.Errorf("outcome: value %q is not in the closed vocabulary", r.Outcome)
	}
	return nil
}

// wireRecord is the wire form of one record — a METHOD-FREE twin of record
// (the same json tags). MarshalJSON must marshal THIS type, never record
// itself: json.Marshal on a type that implements json.Marshaler re-enters
// MarshalJSON and recurses until the stack dies (the journal package's
// journalLine exists for the same reason).
type wireRecord struct {
	Type    RecordType        `json:"type"`
	HoldID  string            `json:"hold_id"`
	TS      int64             `json:"ts"`
	BootID  string            `json:"boot_id"`
	OwnPID  int               `json:"own_pid,omitempty"`
	Outcome Outcome           `json:"outcome,omitempty"`
	Payload *armPayload       `json:"payload,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// scrubDecl returns a scrubbed copy of d (kinds and param values are caller
// text — paths, extra fields).
func scrubDecl(d Decl) Decl {
	out := Decl{Kind: scrub(d.Kind)}
	if len(d.Params) > 0 {
		out.Params = make(map[string]string, len(d.Params))
		for k, v := range d.Params {
			out.Params[scrub(k)] = scrub(v)
		}
	}
	return out
}

// MarshalJSON renders the record with scrub applied LAST — the bytes this
// method returns are the bytes a journal file receives, so no credential-
// shaped material survives no matter which field it entered through (the
// same contract as internal/journal's MarshalJSON).
func (r record) MarshalJSON() ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	line := wireRecord{
		Type:    r.Type,
		HoldID:  scrub(r.HoldID),
		TS:      r.TS,
		BootID:  scrub(r.BootID),
		OwnPID:  r.OwnPID,
		Outcome: r.Outcome,
	}
	if r.Payload != nil {
		p := *r.Payload
		p.FaultID = scrub(p.FaultID)
		p.Target = scrub(p.Target)
		p.Inverse = scrubDecl(p.Inverse)
		p.Check = scrubDecl(p.Check)
		p.BootID = scrub(p.BootID)
		if len(p.Extra) > 0 {
			e := make(map[string]string, len(p.Extra))
			for k, v := range p.Extra {
				e[scrub(k)] = scrub(v)
			}
			p.Extra = e
		}
		line.Payload = &p
	}
	if len(r.Fields) > 0 {
		f := make(map[string]string, len(r.Fields))
		for k, v := range r.Fields {
			f[scrub(k)] = scrub(v)
		}
		line.Fields = f
	}
	return json.Marshal(line)
}

// encode renders the record as one journal line (no trailing newline). The
// payload (when present) is scrubbed with the same pass — it carries caller
// text (paths, extra fields).
func (r *record) encode() ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return scrubLine(b), nil
}

// parseInt64 parses a decimal int64 from a record field ("" and garbage
// parse to 0 — the field is a measurement, never an identity).
func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// proofFromRecord extracts the measured proof from a revert_proof record
// (read-side; the record was already validated).
func proofFromRecord(rec record) Proof {
	return Proof{
		HoldID:   rec.HoldID,
		FaultID:  rec.Fields["fault_id"],
		Target:   rec.Fields["target"],
		Outcome:  rec.Outcome,
		Detail:   rec.Fields["detail"],
		Role:     Role(rec.Fields["role"]),
		Reason:   rec.Fields["reason"],
		PID:      rec.OwnPID,
		Duration: time.Duration(parseInt64(rec.Fields["duration_nanos"])),
	}
}
