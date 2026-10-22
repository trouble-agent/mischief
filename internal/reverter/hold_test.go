package reverter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// holdIDOf arms h and returns the content-derived id (the legacy-vs-canonical
// identity comparisons use it).
func holdIDOf(t *testing.T, lf *Lifecycle, h Hold) string {
	t.Helper()
	id, err := lf.Arm(h)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	return id
}

// legacyDeclPairs is the measured legacy shape (MISCHIEF-DF-68's finding:
// hold b7e9f744dfb5a67d): inverse kind "restore_file" with the restore
// source named "from", check kind "file_exists".
func legacyRestoreInverse(file, src string) Decl {
	return Decl{Kind: "restore_file", Params: map[string]string{"path": file, "from": src}}
}

func canonicalRestoreInverse(file, src string) Decl {
	return Decl{Kind: "restore-file", Params: map[string]string{"path": file, "backup": src}}
}

// TestNormalizeKindAndDecl: the unit surface of the normalization — a legacy
// underscore spelling maps onto the canonical kind exactly when the
// hyphenated form is in the slot's vocabulary; a spelling with no canonical
// equivalent is untouched; the legacy restore-source param "from" moves onto
// "backup" only when "backup" is absent; the caller's map is never mutated.
func TestNormalizeKindAndDecl(t *testing.T) {
	t.Run("underscore kinds of canonical kinds map", func(t *testing.T) {
		for _, tc := range []struct {
			slot Slot
			from string
			want string
		}{
			{SlotInverse, "restore_file", "restore-file"},
			{SlotInverse, "remove_file", "remove-file"},
			{SlotCheck, "file_matches_backup", "file-matches-backup"},
			{SlotCheck, "file_absent", "file-absent"},
		} {
			got, moved := normalizeKind(tc.slot, tc.from)
			if got != tc.want || !moved {
				t.Errorf("%s %q: got %q moved=%v, want %q moved=true", tc.slot, tc.from, got, moved, tc.want)
			}
		}
	})

	t.Run("canonical kinds and unmappable spellings are untouched", func(t *testing.T) {
		for _, tc := range []struct {
			slot Slot
			kind string
		}{
			{SlotInverse, "restore-file"},
			{SlotCheck, "file-absent"},
			{SlotCheck, "file_exists"}, // no canonical equivalent: fails closed later
			{SlotInverse, "delete_everything"},
		} {
			if got, moved := normalizeKind(tc.slot, tc.kind); moved || got != tc.kind {
				t.Errorf("%s %q: got %q moved=%v, want untouched", tc.slot, tc.kind, got, moved)
			}
		}
	})

	t.Run("from moves to backup only when backup is absent, caller map untouched", func(t *testing.T) {
		in := map[string]string{"path": "/x", "from": "/src"}
		out, note := normalizeParams(in)
		if note == "" {
			t.Fatal("no rename note for the legacy shape")
		}
		if out["backup"] != "/src" || len(out) != 2 {
			t.Fatalf("params: %v, want from moved onto backup", out)
		}
		if _, still := in["from"]; !still {
			t.Fatal("caller's map was mutated")
		}

		in2 := map[string]string{"path": "/x", "backup": "/b", "from": "/src"}
		out2, note2 := normalizeParams(in2)
		if note2 != "" || out2["backup"] != "/b" {
			t.Fatalf("backup present: got %v note=%q, want untouched", out2, note2)
		}

		out3, note3 := normalizeParams(nil)
		if note3 != "" || out3 != nil {
			t.Fatalf("nil params: %v %q, want nil/untouched", out3, note3)
		}
	})

	t.Run("NormalizeHold annotates both slots and is idempotent", func(t *testing.T) {
		h := Hold{
			Inverse: legacyRestoreInverse("/x", "/src"),
			Check:   Decl{Kind: "file_matches_backup", Params: map[string]string{"path": "/x", "backup": "/src"}},
		}
		notes := NormalizeHold(&h)
		if h.Inverse.Kind != "restore-file" || h.Inverse.Params["backup"] != "/src" {
			t.Fatalf("inverse: %+v, want canonical kind with backup param", h.Inverse)
		}
		if h.Check.Kind != "file-matches-backup" {
			t.Fatalf("check: %+v, want canonical kind", h.Check)
		}
		if len(notes) != 2 {
			t.Fatalf("notes %v, want one per renamed decl", notes)
		}
		for _, n := range notes {
			if !strings.Contains(n, "->") {
				t.Fatalf("note %q does not name the rename", n)
			}
		}
		if again := NormalizeHold(&h); len(again) != 0 {
			t.Fatalf("second pass renamed %v — normalization is not idempotent", again)
		}
	})
}

// TestArmNormalizesLegacyVocabularyOnWrite (MISCHIEF-DF-68): a hold armed via
// the legacy vocabulary enters the journal under CANONICAL spellings — the
// arm succeeds where the pre-fix code refused it fail-closed at validate,
// the raw journal line carries no underscore kind, the payload annotation
// records that a rename happened, and the legacy arm and its canonical twin
// land on the SAME content-derived id (one vocabulary, one identity).
func TestArmNormalizesLegacyVocabularyOnWrite(t *testing.T) {
	st, lf := newTestStore(t)
	dir := localTempDir(t)
	backup := dir + "/x.pristine"
	file := dir + "/x.live"
	for _, p := range []string{backup, file} {
		if err := writeFileString(p, "GOOD-CONTENT\n"); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	legacy := Hold{
		FaultID: "P-DF68",
		Target:  "file:" + file,
		Inverse: legacyRestoreInverse(file, backup),
		Check:   Decl{Kind: "file_matches_backup", Params: map[string]string{"path": file, "backup": backup}},
		TTL:     time.Minute,
	}
	idLegacy, err := lf.Arm(legacy)
	if err != nil {
		t.Fatalf("arm of a legacy-vocabulary hold was refused: %v", err)
	}

	// The journal bytes: only canonical spellings, the rename annotated.
	lines := journalLines(t, st.Dir)
	if len(lines) != 1 {
		t.Fatalf("%d lines, want 1", len(lines))
	}
	line := lines[0]
	for _, banned := range []string{`"kind":"restore_file"`, `"kind":"file_matches_backup"`, `"from":`} {
		if strings.Contains(line, banned) {
			t.Fatalf("journal line carries the legacy spelling %s: %s", banned, line)
		}
	}
	for _, want := range []string{`"kind":"restore-file"`, `"kind":"file-matches-backup"`, `"backup":"`, `legacy_spelling`} {
		if !strings.Contains(line, want) {
			t.Fatalf("journal line lacks %q: %s", want, line)
		}
	}
	var rec record
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec.Payload.Extra["legacy_spelling"] == "" {
		t.Fatalf("payload extra: %v, want the legacy_spelling annotation", rec.Payload.Extra)
	}

	// Identity: same declaration, same content-derived id — the legacy
	// spelling and its canonical form are ONE hold.
	canonical := legacy
	canonical.Inverse = canonicalRestoreInverse(file, backup)
	canonical.Check = Decl{Kind: "file-matches-backup", Params: map[string]string{"path": file, "backup": backup}}
	idCanonical := holdIDOf(t, lf, canonical)
	if idLegacy != idCanonical {
		t.Fatalf("legacy arm id %s != canonical arm id %s — the ids diverge on spelling", idLegacy, idCanonical)
	}

	// And the normalized hold actually REVERTS (the undo is executable —
	// the DF-23 probe passed it and the executor reads the renamed param).
	if err := lf.Land(idLegacy); err != nil {
		t.Fatalf("land: %v", err)
	}
	if err := writeFileString(file, "FAULTED-CONTENT\n"); err != nil {
		t.Fatalf("land fault: %v", err)
	}
	pr, err := lf.Revert(context.Background(), idLegacy, "direct", RoleAPI)
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if pr.Outcome != OutcomeReverted {
		t.Fatalf("normalized hold graded %s (%s), want reverted", pr.Outcome, pr.Detail)
	}
}

// TestArmRefusesLegacyKindWithNoCanonicalEquivalen: normalization is
// spelling-only. "file_exists" — the legacy check spelling the measured
// journal carries — has NO canonical equivalent, so it stays unknown and the
// arm refuses before any byte is written (the pre-fix behavior, preserved
// where it was right). A legacy spelling mapped across into the WRONG slot
// (restore_file offered as a check) is refused as cross-slot — the
// normalization happens BEFORE slot validation, so the refusal names the
// slots, not an unknown kind.
func TestArmRefusesLegacyKindWithoutCanonicalEquivalent(t *testing.T) {
	st, lf := newTestStore(t)
	t.Run("file_exists check refused, nothing written", func(t *testing.T) {
		h := Hold{
			FaultID: "P-DF68-NOMAP",
			Target:  "file:/x",
			Inverse: Decl{Kind: "remove-file", Params: map[string]string{"path": "/x"}},
			Check:   Decl{Kind: "file_exists"},
			TTL:     time.Second,
		}
		_, err := lf.Arm(h)
		if err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("file_exists check: err=%v, want an unknown-kind refusal", err)
		}
		if !strings.Contains(err.Error(), "file_exists") {
			t.Fatalf("refusal does not name the untouched legacy spelling: %v", err)
		}
	})

	t.Run("file_exists inverse refused", func(t *testing.T) {
		h := Hold{
			FaultID: "P-DF68-NOMAP-INV",
			Target:  "file:/x",
			Inverse: Decl{Kind: "file_exists", Params: map[string]string{"path": "/x"}},
			Check:   Decl{Kind: "file-absent", Params: map[string]string{"path": "/x"}},
			TTL:     time.Second,
		}
		_, err := lf.Arm(h)
		if err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("file_exists inverse: err=%v, want an unknown-kind refusal", err)
		}
	})

	t.Run("cross-slot legacy spelling refuses without rewriting the slot", func(t *testing.T) {
		// file_absent is a CHECK-kind spelling offered as an inverse.
		// Normalization is slot-grounded: it maps within a slot's own
		// vocabulary ONLY (restore_file as an inverse), never across —
		// rewriting "file_absent" onto the inverse slot would be inventing
		// an inverse that never existed. So the spelling stays untouched
		// and the refusal is the unknown-kind one, naming it.
		_, err := lf.Arm(Hold{
			FaultID: "P-DF68-CROSS",
			Target:  "file:/x",
			Inverse: Decl{Kind: "file_absent", Params: map[string]string{"path": "/x"}},
			Check:   Decl{Kind: "file-absent", Params: map[string]string{"path": "/x"}},
			TTL:     time.Second,
		})
		if err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("file_absent as inverse: err=%v, want the unknown-kind refusal (normalization is slot-grounded, never cross-slot)", err)
		}
		if !strings.Contains(err.Error(), "file_absent") {
			t.Fatalf("refusal does not name the untouched legacy spelling: %v", err)
		}
	})

	if lines := journalLines(t, st.Dir); len(lines) != 0 {
		t.Fatalf("refused legacy arms wrote %d lines, want 0", len(lines))
	}
}

// TestLegacyJournalHoldStaysGraceful (MISCHIEF-DF-68 AC: existing holds with
// underscore kinds handled gracefully by status/revert queries). A journal
// line written by the PRE-whitelist build (hand-written JSON behind the
// store's back, the measured legacy shape verbatim: restore_file/from,
// file_exists) must: fold without error (the fold validates types and
// outcomes, not kinds), list in Status, and revert fail-CLOSED — the kill
// switch grades it revert_failed naming the unknown spelling, never a
// fabricated revert, and the hold terminates exactly as the live old
// journal's hold does on today's binary.
func TestLegacyJournalHoldStaysGraceful(t *testing.T) {
	dir := localTempDir(t)

	// First open mints the boot identity; the legacy line then claims the
	// SAME boot (armed by an older binary in this boot).
	st0, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	boot := st0.BootID
	if err := st0.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	legacy := `{"type":"arm","hold_id":"b7e9f744dfb5a67d","ts":1791000000,"boot_id":"` + boot +
		`","payload":{"fault_id":"S-001","target":"file:/tmp/df05-hold.txt","inverse":{"kind":"restore_file","params":{"path":"/tmp/df05-hold.txt","from":"/tmp/df05-pristine.txt"}},"check":{"kind":"file_exists"},"ttl":60000000000,"boot_id":"` + boot + `"}}` + "\n"
	if err := appendJournalLine(dir, legacy); err != nil {
		t.Fatalf("append legacy line: %v", err)
	}

	st, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen with a legacy-kind line failed (the fold must tolerate underscore kinds): %v", err)
	}
	defer st.Close()
	lf := New(st)

	// Status lists it without a vocabulary error — the graceful read side.
	s := lf.StatusAll()
	if len(s.Holds) != 1 {
		t.Fatalf("%d holds, want 1", len(s.Holds))
	}
	if s.Holds[0].HoldID != "b7e9f744dfb5a67d" {
		t.Fatalf("hold %s, want the legacy id", s.Holds[0].HoldID)
	}

	// Revert (the kill switch path's single-hold form): the unknown inverse
	// fails closed into a MEASURED revert_failed proof — the honest grade —
	// naming the spelling the journal actually carries.
	pr, err := lf.Revert(context.Background(), "b7e9f744dfb5a67d", "kill_switch", RoleKillSwitch)
	if err != nil {
		t.Fatalf("revert of a legacy hold errored: %v (want a measured revert_failed proof)", err)
	}
	if pr.Outcome != OutcomeRevertFailed {
		t.Fatalf("outcome %s, want revert_failed", pr.Outcome)
	}
	if !strings.Contains(pr.Detail, "unknown") || !strings.Contains(pr.Detail, "restore_file") {
		t.Fatalf("detail %q does not name the unknown legacy spelling", pr.Detail)
	}
	// The proof is in the journal and the hold is terminal (no dead end in
	// the fold: the sweep's outcome is recorded, one vocabulary of states).
	lines := journalLines(t, dir)
	foundProof := false
	for _, l := range lines {
		if recordTypeOf(t, l) == string(RecordRevertProof) && strings.Contains(l, string(OutcomeRevertFailed)) {
			foundProof = true
		}
	}
	if !foundProof {
		t.Fatalf("no revert_failed proof appended: %v", lines)
	}
}