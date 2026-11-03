package main

import (
	"testing"

	"github.com/trouble-agent/mischief/internal/reverter"
)

// landArmedHolds records a land for every currently-armed hold in dir, the way
// a real injector does after it has actually applied a fault.
//
// PROF-002 made `reverted` unreachable without a landed record: a hold that was
// armed but never landed now grades not_landed, because grading only the UNDO
// (inverse ran + check passed) let an arm-then-expire cycle with no injection
// in between publish a green "reverted" proof — the false green an agent would
// read as "my code survived that fault".
//
// Several CLI tests applied the fault out-of-band (writing faulted bytes
// directly) but never recorded the land, so they now describe a fault that the
// journal cannot see. This helper closes that gap the same way a real injector
// must: apply the fault AND record that it landed.
func landArmedHolds(t *testing.T, dir string) int {
	t.Helper()
	store, err := reverter.Open(dir)
	if err != nil {
		t.Fatalf("land: open %s: %v", dir, err)
	}
	defer store.Close()

	n := 0
	for _, h := range reverter.New(store).StatusAll().Holds {
		if h.Status != reverter.StatusArmed {
			continue
		}
		if err := reverter.New(store).Land(h.HoldID); err != nil {
			t.Fatalf("land %s: %v", h.HoldID, err)
		}
		n++
	}
	return n
}
