package battery

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/trouble-agent/mischief/internal/journal"
)

// journal.go — the battery run's journal file (AC-18's journal-path half).
//
// The record set comes from RunResult.JournalRecords (the SPEC-02
// lifecycle vocabulary); the file is <run-dir>/battery.jsonl, one JSONL
// line per record, written through journal.Record.MarshalJSON so the
// validate + scrub-on-write path (AC-20) is the journal package's own —
// an out-of-vocabulary value cannot enter the file. Mirrors
// selftest.WriteJournal (the package owns its filename; the format is
// shared).

// WriteJournal writes the record set to <dir>/battery.jsonl (created on
// demand, 0600, append — re-runs append under their own run id) and
// returns the file path.
func WriteJournal(dir string, recs []journal.Record) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("battery journal: %w", err)
	}
	path := filepath.Join(dir, "battery.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", fmt.Errorf("battery journal: %w", err)
	}
	defer f.Close()
	for i := range recs {
		b, err := recs[i].MarshalJSON()
		if err != nil {
			return "", fmt.Errorf("battery journal: record %d: %w", i, err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return "", fmt.Errorf("battery journal: %w", err)
		}
	}
	return path, f.Sync()
}
