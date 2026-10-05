package selftest

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/trouble-agent/mischief/internal/journal"
)

// journal.go — the selftest's journal record set, written in the
// internal/journal record format (the PRD §6.3 lifecycle vocabulary; the
// record types are the journal package's own constants, so a reader of a
// run journal reads a selftest journal unchanged).
//
// Mapping (the §6.3 lifecycle onto a selftest loop):
//
//	run_planned     — once, before the first primitive: the primitive set
//	fault_landed    — per primitive with a measured land proof (skips
//	                  carry no landed record — nothing landed)
//	fault_reverted  — per primitive with a measured revert proof (AC-4)
//	verdict         — once: pass ⇒ recovered, fail ⇒ no_op, skip ⇒ aborted
//	run_closed      — once: the terminal record carrying the counts
//
// Writes go to <dir>/selftest.jsonl as one JSONL line per record, in seq
// order, scrubbed by the journal package's own write path (AC-20 rides
// on Record.MarshalJSON).

// JournalRecords renders the selftest's record set for the given results
// (exported for the CLI's journal write). The verdict mapping is the
// honesty ladder's: a fail is NEVER recovered —
// it grades no_op (the primitive could not prove its landing), and a skip
// grades aborted (the run was refused by the host's capability, the
// rails' own verdict for "this run did not happen").
func JournalRecords(runID string, res []Result) []journal.Record {
	now := time.Now().Unix()
	recs := make([]journal.Record, 0, 3*len(res)+3)
	seq := 0
	add := func(r *journal.Record) {
		r.RunID, r.Seq, r.TS = runID, seq, now
		seq++
		recs = append(recs, *r)
	}

	ids := make([]string, 0, len(res))
	for _, r := range res {
		ids = append(ids, r.ID)
	}
	add(&journal.Record{Type: journal.RecordRunPlanned,
		Fields: map[string]string{"kind": "selftest", "primitives": joinIDs(ids)}})

	for _, r := range res {
		switch r.State {
		case "pass":
			add(&journal.Record{Type: journal.RecordFaultLanded, Fields: map[string]string{
				"primitive": r.ID, "proof": r.LandProof}})
			add(&journal.Record{Type: journal.RecordFaultReverted, Fields: map[string]string{
				"primitive": r.ID, "proof": r.RevertProof}})
		case "fail":
			add(&journal.Record{Type: journal.RecordFaultLanded, Fields: map[string]string{
				"primitive": r.ID, "proof": r.LandProof, "outcome": "no_op", "reason": r.Reason}})
		default: // skip
			add(&journal.Record{Type: journal.RecordFaultArmed, Fields: map[string]string{
				"primitive": r.ID, "outcome": "skipped", "reason": r.Reason}})
		}
	}

	verdict := journal.VerdictRecovered
	pass, skip, fail := 0, 0, 0
	for _, r := range res {
		switch r.State {
		case "pass":
			pass++
		case "skip":
			skip++
		default:
			fail++
		}
	}
	if fail > 0 {
		verdict = journal.VerdictNoOp
	} else if skip > 0 && pass == 0 {
		verdict = journal.VerdictAborted
	}
	add(&journal.Record{Type: journal.RecordVerdict, Verdict: verdict,
		Fields: map[string]string{"pass": strconv.Itoa(pass), "skip": strconv.Itoa(skip), "fail": strconv.Itoa(fail)}})
	add(&journal.Record{Type: journal.RecordRunClosed, Verdict: verdict,
		Fields: map[string]string{"kind": "selftest", "primitives": strconv.Itoa(len(res))}})
	return recs
}

// WriteJournal writes the record set to <dir>/selftest.jsonl (created on
// demand, 0600, append) and returns the file path. Every line is encoded
// by the journal package (validate + scrub-on-write), so an out-of-
// vocabulary value cannot enter the file.
func WriteJournal(dir string, recs []journal.Record) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "selftest.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	for i := range recs {
		b, err := recs[i].MarshalJSON()
		if err != nil {
			return "", err
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return "", err
		}
	}
	return path, f.Sync()
}
