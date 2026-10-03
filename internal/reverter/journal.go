package reverter

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// journalFileName is the append-only JSONL file inside Dir.
const journalFileName = "reverter.jsonl"

// bootMarkerFile carries the boot identity: {host_boot, reverter_boot}.
// All processes on one HOST boot share one reverter boot id (the marker is
// adopted); a new host boot rotates it — which is exactly PRD §7's
// "leftovers from a previous boot": same host, previous boot.
const bootMarkerFile = "boot.json"

// bootMarker is the on-disk boot identity record.
type bootMarker struct {
	// HostBoot is the Linux host boot id (/proc/sys/kernel/random/boot_id),
	// "" when the host does not expose one.
	HostBoot string `json:"host_boot"`
	// ReverterBoot is the reverter boot id shared by every process of this
	// host boot (hex16).
	ReverterBoot string `json:"reverter_boot"`
}

// hostBootID reports the host's boot id (Linux: /proc/sys/kernel/random/
// boot_id); "" when unavailable — the marker then scopes the reverter boot
// to the marker's lifetime instead of the host's (the conservative fallback:
// a host that cannot prove its boot cannot silently adopt old holds as
// current, and a fresh marker rotates the id).
func hostBootID() string {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// randomBootID derives a fresh random id: hex(sha256(32 random bytes))[:16].
func randomBootID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("reverter: boot id: %v", err))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8]) // [:16] hex chars = first 8 bytes
}

// bootIdentity resolves the reverter boot id for dir: adopt the marker when
// it names the CURRENT host boot; otherwise (no marker, or a previous
// host's marker) mint a new id and install the marker atomically
// (temp+rename). A rename race between two concurrent first-openers can
// leave each with its own id once — the loser's holds then read as stuck,
// which is the safe direction (a false stuck is surfaced and reverted, a
// false current is invisible); v0.1 accepts it and this comment names it.
func bootIdentity(dir string) string {
	host := hostBootID()
	path := filepath.Join(dir, bootMarkerFile)
	for attempt := 0; attempt < 5; attempt++ {
		if m, err := readBootMarker(path); err == nil && m.HostBoot == host && m.ReverterBoot != "" {
			return m.ReverterBoot
		}
		nb := randomBootID()
		b, err := json.Marshal(bootMarker{HostBoot: host, ReverterBoot: nb})
		if err != nil {
			break
		}
		tmp := filepath.Join(dir, bootMarkerFile+".tmp-"+nb[:8])
		if err := os.WriteFile(tmp, b, 0o600); err != nil {
			break
		}
		if err := os.Rename(tmp, path); err == nil {
			return nb
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	// Marker unreadable or unwritable after retries: adopt whatever the
	// marker says (a previous boot's id still reconciles safely — the
	// holds are treated as leftovers), else go process-local.
	if m, err := readBootMarker(path); err == nil && m.ReverterBoot != "" {
		return m.ReverterBoot
	}
	return randomBootID()
}

// readBootMarker parses the boot marker file.
func readBootMarker(path string) (bootMarker, error) {
	var m bootMarker
	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("reverter: boot marker: %w", err)
	}
	return m, nil
}

// DefaultDir is the default state directory: ~/.mischief/reverter/.
func DefaultDir() string {
	return filepath.Join(homeDir(), ".mischief", "reverter")
}

// Proof is the measured revert outcome for one fault (AC-4/AC-6): which
// fault, the measured check, the outcome, the duration — and who executed
// it (role + pid, AC-5's journal evidence).
type Proof struct {
	// HoldID is the content-derived hold id.
	HoldID string
	// FaultID is the declared fault id.
	FaultID string
	// Target is the declared target selector.
	Target string
	// Outcome is reverted or revert_failed (closed vocabulary).
	Outcome Outcome
	// Detail is the measured check result text (raw command output, trimmed
	// and scrubbed) — the measurement, not an assertion.
	Detail string
	// Duration is how long the measured revert took (action + check).
	Duration time.Duration
	// Role is who executed the revert (api/owner/kill_switch).
	Role Role
	// PID is the executing process's pid.
	PID int
	// Reason is why the revert ran ("ttl_expiry", "kill_switch", "direct").
	Reason string
}

// holdState is one hold's current fold: the write-ahead payload plus
// lifecycle flags, rebuilt from the journal lines.
type holdState struct {
	Arm     armPayload
	Status  HoldStatus
	Expiry  time.Time
	StuckAt time.Time
	LandPID int
	// OwnPID is the pid of the last prover that recorded a terminal proof
	// for this hold (AC-5's evidence hop: the reverter's own pid).
	OwnPID int
}

// HoldStatus is the closed hold status vocabulary Status reports.
type HoldStatus string

const (
	// StatusArmed: write-ahead recorded; the fault may land / may be held.
	StatusArmed HoldStatus = "armed"
	// StatusStuck: non-terminal hold inherited from a previous boot (boot
	// reconcile); the fault may still be live and the kill switch reverts it.
	StatusStuck HoldStatus = "stuck"
	// StatusReverted: a measured revert proof recorded the fault undone.
	StatusReverted HoldStatus = "reverted"
	// StatusRevertFailed: a measured revert proof recorded failure (AC-4:
	// the fault may still be live; never reported as reverted).
	StatusRevertFailed HoldStatus = "revert_failed"
)

// Valid reports whether s is a declared hold status.
func (s HoldStatus) Valid() bool {
	switch s {
	case StatusArmed, StatusStuck, StatusReverted, StatusRevertFailed:
		return true
	}
	return false
}

// Store is the reverter's state: an append-only JSONL journal under Dir.
// All writes are serialised through the store's mutex (the CLI, the TTL
// owner and a concurrent kill switch may write the same file). Lines are
// never rewritten; state is folded from the log at Open and refreshed
// incrementally on append.
type Store struct {
	// Dir is the state directory (the journal lives at Dir/reverter.jsonl).
	Dir string
	// BootID is this process generation's id (boot reconcile compares it
	// against arm records to find previous-boot leftovers).
	BootID string
	// Now returns the wall clock (tests inject). The clock feeds record
	// timestamps and expiry math only — never identities (ids are content-
	// derived).
	Now func() time.Time

	mu       sync.Mutex
	f        *os.File
	armed    map[string]armPayload // holdID -> write-ahead payload
	holds    map[string]*holdState // holdID -> current fold state
	expiries map[string]time.Time  // holdID -> absolute expiry (live holds)
	history  []Proof               // every proof ever appended (insertion order)
}

// Open opens (creating if needed) the journal under dir, folds the log,
// runs boot reconcile (marking non-terminal holds from PREVIOUS boots
// stuck) and returns the store. The reconcile is part of Open by design
// (SPEC-04: boot reconcile is a boot-time act, not a manual one).
func Open(dir string) (*Store, error) {
	if dir == "" {
		dir = DefaultDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("reverter: state dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, journalFileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("reverter: journal: %w", err)
	}
	s := &Store{
		Dir:      dir,
		BootID:   bootIdentity(dir),
		Now:      time.Now,
		f:        f,
		armed:    map[string]armPayload{},
		holds:    map[string]*holdState{},
		expiries: map[string]time.Time{},
	}
	if err := s.fold(); err != nil {
		f.Close()
		return nil, err
	}
	if err := s.reconcile(); err != nil {
		f.Close()
		return nil, err
	}
	return s, nil
}

// Close fsyncs and releases the journal file handle.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Sync()
	if cerr := s.f.Close(); err == nil {
		err = cerr
	}
	s.f = nil
	return err
}

// append writes one record line: serialise (scrub-on-write happens inside
// encode), append, fsync — the write-ahead guarantee's durability half. A
// record that fails validation is refused BEFORE any byte is written (the
// closed vocabularies are enforced at the write path).
func (s *Store) append(r record) error {
	b, err := r.encode()
	if err != nil {
		return fmt.Errorf("reverter: encode %s: %w", r.Type, err)
	}
	line := make([]byte, 0, len(b)+1)
	line = append(line, b...)
	line = append(line, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendLocked(line)
}

// appendLocked writes and fsyncs one already-serialised line; the caller
// holds s.mu. Applies the write-side fold so readers never rescan the file.
func (s *Store) appendLocked(line []byte) error {
	if s.f == nil {
		return fmt.Errorf("reverter: journal closed")
	}
	if _, err := s.f.Write(line); err != nil {
		return fmt.Errorf("reverter: journal write: %w", err)
	}
	if err := s.f.Sync(); err != nil {
		return fmt.Errorf("reverter: journal fsync: %w", err)
	}
	if err := s.foldLine(bytes.TrimSuffix(line, []byte("\n"))); err != nil {
		// The bytes are already durable; a fold bug must not be silent, but
		// it also must not make the caller believe the write failed — it is
		// reported, and the next Open folds the same line from disk.
		return fmt.Errorf("reverter: journal fold: %w", err)
	}
	return nil
}

// fold rebuilds the in-memory state from the journal file on disk (the log
// is the source of record; nothing else survives a restart).
func (s *Store) fold() error {
	path := filepath.Join(s.Dir, journalFileName)
	fh, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reverter: journal open: %w", err)
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := s.foldLine(line); err != nil {
			return err
		}
	}
	return sc.Err()
}

// foldLine applies one journal line to the in-memory fold. Read-side
// validation fails loudly on an unknown type/outcome (the vocabulary is
// closed; a corrupted or foreign line must not be silently skipped).
func (s *Store) foldLine(line []byte) error {
	var rec record
	dec := json.NewDecoder(bytes.NewReader(line))
	if err := dec.Decode(&rec); err != nil {
		return fmt.Errorf("reverter: journal parse: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("reverter: journal parse: trailing data after record")
	}
	if err := rec.validate(); err != nil {
		// Write-side validation ran at append time; a line that fails here
		// is corruption (or a foreign writer) — fail loudly, never skip.
		return fmt.Errorf("reverter: journal invalid: %w", err)
	}
	switch rec.Type {
	case RecordArm:
		s.armed[rec.HoldID] = *rec.Payload
		st, ok := s.holds[rec.HoldID]
		if !ok {
			st = &holdState{}
			s.holds[rec.HoldID] = st
		}
		st.Arm = *rec.Payload
		st.Status = StatusArmed
		st.Expiry = time.Unix(rec.TS, 0).Add(time.Duration(rec.Payload.TTL))
		s.expiries[rec.HoldID] = st.Expiry
	case RecordLand:
		if st, ok := s.holds[rec.HoldID]; ok {
			st.LandPID = rec.OwnPID
		}
	case RecordLandRefused:
		// informational: the hold stays armed (unlanded).
	case RecordHoldExpired:
		// informational: the TTL boundary; the proof record follows it.
	case RecordRevertProof:
		pr := proofFromRecord(rec)
		s.history = append(s.history, pr)
		if st, ok := s.holds[rec.HoldID]; ok {
			st.OwnPID = rec.OwnPID
			switch rec.Outcome {
			case OutcomeReverted:
				st.Status = StatusReverted
			case OutcomeRevertFailed:
				st.Status = StatusRevertFailed
			}
			delete(s.expiries, rec.HoldID)
		}
	case RecordHoldStuck:
		if st, ok := s.holds[rec.HoldID]; ok {
			st.Status = StatusStuck
			st.StuckAt = time.Unix(rec.TS, 0)
		}
	default:
		return fmt.Errorf("reverter: journal: unknown record type %q", rec.Type)
	}
	return nil
}

// reconcile is boot reconcile: every hold whose latest state is non-terminal
// (armed/stuck-with-no-proof) AND whose arm came from a DIFFERENT boot is
// marked stuck (reason previous_boot). Holds terminal before this boot
// (reverted / revert_failed) stay as they are; a stuck mark is never
// written twice.
func (s *Store) reconcile() error {
	now := s.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var lines [][]byte
	for id, st := range s.holds {
		if st.Status != StatusArmed {
			continue // terminal, or already stuck — never re-marked
		}
		if st.Arm.BootID == s.BootID {
			continue // this boot's own arm: not a leftover
		}
		rec := record{
			Type:   RecordHoldStuck,
			HoldID: id,
			TS:     now.Unix(),
			BootID: s.BootID,
			Fields: map[string]string{
				"fault_id":      st.Arm.FaultID,
				"previous_boot": "1",
			},
		}
		b, err := rec.encode()
		if err != nil {
			return err
		}
		line := make([]byte, 0, len(b)+1)
		line = append(line, b...)
		line = append(line, '\n')
		lines = append(lines, line)
		st.Status = StatusStuck
		st.StuckAt = now
	}
	for _, line := range lines {
		if err := s.appendLocked(line); err != nil {
			return err
		}
	}
	return nil
}
