package backend_docker

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// journal.go — the write-ahead inverse (the internal/reverter pattern,
// SPEC-04, restated for the docker backend): the hold — fault, DECLARATIVE
// inverse, DECLARATIVE check, restore parameters — is appended to a JSONL
// journal and fsynced BEFORE the caller may land the fault; Land refuses
// when the write-ahead line is not durable. The inverse kinds are native,
// shell-free and executable from JOURNAL BYTES ALONE (ExecInverseFromJournal
// — a detached process re-executes them; no closures cross the journal
// boundary). Reverts append a measured revert_proof record; a revert whose
// check fails records revert_failed, never "reverted".
//
// Record vocabulary (closed, validated at the write path):
//
//	arm           — the write-ahead hold (before any landing)
//	land          — the caller landed the fault (post write-ahead)
//	land_refused  — the write-ahead line was not durable; landing refused
//	revert_proof  — the MEASURED revert outcome (reverted|revert_failed)
//
// The record shape is this package's own (the reverter package's holds
// file-domain kinds; docker kinds are additive here without editing that
// package — the same record philosophy, one backend family).

// RecordType is the journal's record vocabulary.
type RecordType string

const (
	RecordArm        RecordType = "arm"
	RecordLand       RecordType = "land"
	RecordLandRefuse RecordType = "land_refused"
	RecordRevert     RecordType = "revert_proof"
)

func (t RecordType) valid() bool {
	switch t {
	case RecordArm, RecordLand, RecordLandRefuse, RecordRevert:
		return true
	}
	return false
}

// InverseDecl is the declarative inverse: Kind + Params, all flat strings.
// Kinds: docker-unpause, docker-start, docker-memory-restore. An unknown
// kind fails CLOSED (refuses to execute).
type InverseDecl struct {
	Kind   string            `json:"kind"`
	Params map[string]string `json:"params"`
}

func (d InverseDecl) Validate() error {
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

// DeclOf derives the declared inverse of an armed fault kind. An empty
// container refuses at derivation (a decl whose params would touch nothing
// is how a "restore" that restores nothing would pass).
func DeclOf(kind Kind, container string, restoreMemory int64) (InverseDecl, error) {
	if err := CheckScratch(container); err != nil {
		return InverseDecl{}, fmt.Errorf("backend_docker: inverse decl: %w", err)
	}
	switch kind {
	case KindPause:
		return InverseDecl{Kind: "docker-unpause", Params: map[string]string{"container": container}}, nil
	case KindKill:
		return InverseDecl{Kind: "docker-start", Params: map[string]string{"container": container}}, nil
	case KindOOM:
		mem := "0"
		if restoreMemory > 0 {
			mem = strconv.FormatInt(restoreMemory, 10)
		}
		return InverseDecl{Kind: "docker-memory-restore", Params: map[string]string{
			"container": container, "memory": mem}}, nil
	default:
		return InverseDecl{}, fmt.Errorf("backend_docker: no declared inverse for fault kind %q", kind)
	}
}

// CheckDecl is the declarative post-revert check: what the detached
// executor measures to call a revert "reverted". Kind: docker-running (the
// container is observed Running), docker-unpaused (Running AND not Paused),
// docker-memory (Running AND Memory == the declared value).
type CheckDecl = InverseDecl

// record is one journal line.
type record struct {
	Type      RecordType  `json:"type"`
	TS        int64       `json:"ts"`
	Primitive string      `json:"primitive"`
	Container string      `json:"container"`
	Inverse   InverseDecl `json:"inverse"`
	Check     CheckDecl   `json:"check"`
	// Detail carries the measured proof/reason text (revert_proof, land).
	Detail string `json:"detail,omitempty"`
	// Outcome carries "reverted" | "revert_failed" on revert_proof records.
	Outcome string `json:"outcome,omitempty"`
}

// Journal is the append-only write-ahead journal (one JSON object per line).
type Journal struct {
	path string
}

// OpenJournal opens (creating) the journal at path. Parent dirs are made.
func OpenJournal(path string) (*Journal, error) {
	if path == "" {
		return nil, fmt.Errorf("backend_docker: journal path: empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("backend_docker: journal dir: %w", err)
	}
	return &Journal{path: path}, nil
}

// Path is the journal file's path.
func (j *Journal) Path() string { return j.path }

// append writes one record as one fsynced JSONL line (the write-ahead
// durability: the line is on disk before append returns).
func (j *Journal) append(r record) error {
	if !r.Type.valid() {
		return fmt.Errorf("backend_docker: journal: unknown record type %q (vocabulary is closed)", r.Type)
	}
	r.TS = time.Now().Unix()
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("backend_docker: journal: marshal: %w", err)
	}
	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("backend_docker: journal: open: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("backend_docker: journal: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("backend_docker: journal: fsync: %w", err)
	}
	return nil
}

// Arm writes the write-ahead hold for one fault: the inverse and check are
// on disk (fsynced) before Land may be called.
func (j *Journal) Arm(primitive, container string, inv InverseDecl, chk CheckDecl) error {
	if err := inv.Validate(); err != nil {
		return fmt.Errorf("backend_docker: arm %s: inverse: %w", primitive, err)
	}
	if err := chk.Validate(); err != nil {
		return fmt.Errorf("backend_docker: arm %s: check: %w", primitive, err)
	}
	return j.append(record{Type: RecordArm, Primitive: primitive, Container: container, Inverse: inv, Check: chk})
}

// Land writes the land record. WriteAheadOK answers whether the arm line
// for (primitive, container) is durable on disk; when it is not, Land
// records land_refused and refuses (the inverse would not predate the
// fault).
func (j *Journal) Land(primitive, container string) error {
	ok, err := j.WriteAheadOK(primitive, container)
	if err != nil {
		_ = j.append(record{Type: RecordLandRefuse, Primitive: primitive, Container: container,
			Detail: "write-ahead probe failed: " + err.Error()})
		return fmt.Errorf("backend_docker: land %s refused: write-ahead probe failed: %w", primitive, err)
	}
	if !ok {
		_ = j.append(record{Type: RecordLandRefuse, Primitive: primitive, Container: container,
			Detail: "no durable arm line — the inverse would not predate the fault"})
		return fmt.Errorf("backend_docker: land %s refused: write-ahead inverse not durable — land refused", primitive)
	}
	return j.append(record{Type: RecordLand, Primitive: primitive, Container: container})
}

// WriteAheadOK scans the journal for a durable arm line matching
// (primitive, container). A read error is reported, never folded into
// "false" silently — the caller distinguishes absent from unreadable.
func (j *Journal) WriteAheadOK(primitive, container string) (bool, error) {
	f, err := os.Open(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	found := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return false, fmt.Errorf("journal %s: malformed line: %w", j.path, err)
		}
		if r.Type == RecordArm && r.Primitive == primitive && r.Container == container {
			found = true
		}
	}
	return found, sc.Err()
}

// RecordRevert appends the MEASURED revert outcome.
func (j *Journal) RecordRevert(primitive, container, outcome, detail string) error {
	if outcome != "reverted" && outcome != "revert_failed" {
		return fmt.Errorf("backend_docker: journal: outcome %q not in the closed vocabulary (reverted|revert_failed)", outcome)
	}
	return j.append(record{Type: RecordRevert, Primitive: primitive, Container: container,
		Detail: detail, Outcome: outcome})
}

// Records returns every parsed record (tests and status readers).
func (j *Journal) Records() ([]record, error) {
	f, err := os.Open(j.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("journal %s: malformed line: %w", j.path, err)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

// ExecuteInverse executes one declarative inverse natively (no shell) and
// returns the measured detail. Unknown kinds fail closed.
func (c *CLI) ExecuteInverse(inv InverseDecl) (string, error) {
	if err := inv.Validate(); err != nil {
		return "", fmt.Errorf("backend_docker: inverse: %w", err)
	}
	container := inv.Params["container"]
	if err := CheckScratch(container); err != nil {
		return "", err
	}
	switch inv.Kind {
	case "docker-unpause":
		if _, errOut, err := c.run("unpause", container); err != nil {
			return "", fmt.Errorf("docker unpause %s: %v: %s", container, err, oneLine(errOut))
		}
		return "unpaused", nil
	case "docker-start":
		if _, errOut, err := c.run("start", container); err != nil {
			return "", fmt.Errorf("docker start %s: %v: %s", container, err, oneLine(errOut))
		}
		return "started", nil
	case "docker-memory-restore":
		mem := inv.Params["memory"]
		swap := "-1"
		if mem != "0" {
			swap = mem
		}
		if _, errOut, err := c.run("update", "--memory", mem, "--memory-swap", swap, container); err != nil {
			return "", fmt.Errorf("docker update --memory %s %s: %v: %s", mem, container, err, oneLine(errOut))
		}
		return "memory-restored:" + mem, nil
	default:
		return "", fmt.Errorf("backend_docker: inverse kind %q: unknown (refusing to execute)", inv.Kind)
	}
}

// EvalCheck evaluates one declarative check against LIVE state and returns
// (measured detail, ok). Unknown kinds fail (fail closed). Kind validity is
// checked BEFORE any host contact so an unknown check never grades on an
// accidental reading.
func (c *CLI) EvalCheck(chk CheckDecl) (string, bool, error) {
	if err := chk.Validate(); err != nil {
		return "", false, fmt.Errorf("backend_docker: check: %w", err)
	}
	switch chk.Kind {
	case "docker-running", "docker-unpaused", "docker-memory":
	default:
		return "", false, fmt.Errorf("backend_docker: check kind %q: unknown (failing closed)", chk.Kind)
	}
	container := chk.Params["container"]
	if err := CheckScratch(container); err != nil {
		return "", false, err
	}
	st, err := c.Inspect(container)
	if err != nil {
		return "inspect-failed", false, nil
	}
	switch chk.Kind {
	case "docker-running":
		if st.Running {
			return "running", true, nil
		}
		return fmt.Sprintf("not-running(exit=%d)", st.ExitCode), false, nil
	case "docker-unpaused":
		if st.Running && !st.Paused {
			return "running-unpaused", true, nil
		}
		return fmt.Sprintf("running=%t paused=%t", st.Running, st.Paused), false, nil
	case "docker-memory":
		want, err := strconv.ParseInt(chk.Params["memory"], 10, 64)
		if err != nil {
			return "", false, fmt.Errorf("check %s: memory param %q: %w", chk.Kind, chk.Params["memory"], err)
		}
		if st.Running && st.MemoryBytes == want {
			return fmt.Sprintf("running memory=%d", st.MemoryBytes), true, nil
		}
		return fmt.Sprintf("running=%t memory=%d want=%d", st.Running, st.MemoryBytes, want), false, nil
	default:
		return "", false, fmt.Errorf("backend_docker: check kind %q: unknown (failing closed)", chk.Kind)
	}
}

// ExecInverseFromJournal is the detached executor's entry: fold the journal,
// find every landed-but-unreverted hold, execute its declared inverse and
// append one MEASURED revert_proof per hold. It needs only the journal bytes
// and a CLI — no in-process state (the AC-5 detached-owner shape, applied to
// this backend). Returns the per-hold proofs in journal order.
func ExecInverseFromJournal(c *CLI, path string) ([]string, error) {
	j, err := OpenJournal(path)
	if err != nil {
		return nil, err
	}
	recs, err := j.Records()
	if err != nil {
		return nil, err
	}
	type hold struct {
		primitive string
		container string
		inv       InverseDecl
		chk       CheckDecl
		land      bool
		done      bool
	}
	order := []string{}
	holds := map[string]*hold{}
	for _, r := range recs {
		key := r.Primitive + "\x1f" + r.Container
		switch r.Type {
		case RecordArm:
			if _, ok := holds[key]; !ok {
				order = append(order, key)
			}
			holds[key] = &hold{primitive: r.Primitive, container: r.Container, inv: r.Inverse, chk: r.Check}
		case RecordLand:
			if h, ok := holds[key]; ok {
				h.land = true
			}
		case RecordRevert:
			if h, ok := holds[key]; ok {
				h.done = true
			}
		}
	}
	var proofs []string
	for _, key := range order {
		h := holds[key]
		if h == nil || !h.land || h.done {
			continue
		}
		detail, rerr := c.ExecuteInverse(h.inv)
		if rerr == nil {
			var cdetail string
			var cok bool
			// the check observes state AFTER the inverse; poll briefly so a
			// just-started container is observable (bounded, load-immune)
			deadline := time.Now().Add(proofWindow)
			for {
				cdetail, cok, rerr = c.EvalCheck(h.chk)
				if cok || rerr != nil || time.Now().After(deadline) {
					break
				}
				time.Sleep(journalWait)
			}
			if cok {
				proof := fmt.Sprintf("%s [%s]: reverted (inverse=%s, check=%s)", key, detail, h.inv.Kind, cdetail)
				_ = j.RecordRevert(h.primitive, h.container, "reverted", proof)
				proofs = append(proofs, proof)
				continue
			}
			rerr = fmt.Errorf("check did not pass after inverse (%s)", cdetail)
		}
		proof := fmt.Sprintf("%s: revert_failed (%v)", key, rerr)
		_ = j.RecordRevert(h.primitive, h.container, "revert_failed", proof)
		proofs = append(proofs, proof)
	}
	return proofs, nil
}
