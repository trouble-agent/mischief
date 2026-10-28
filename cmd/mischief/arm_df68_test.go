package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArmLegacyKindNormalizesBeforePreOpenGate (MISCHIEF-DF-68-R2-R2): the
// real CLI arm path refused the documented legacy vocabulary. arm.go builds
// `pending` and runs the pre-open reverter.ValidateForArm gate BEFORE
// reverter.Open / Lifecycle.Arm, but only Arm normalizes the legacy
// spellings (restore_file, the "from" restore-source param,
// file_matches_backup) — so the merged DF-68 normalizer was unreachable from
// the CLI: the canonical hyphen control armed rc=0 while the legacy spelling
// was rejected rc=2 ("inverse kind \"restore_file\": unknown") before any
// journal existed. The pre-open gate now normalizes its local copy first
// (the same NormalizeHold Arm applies before validating), so the gate and
// Arm agree on every declaration. This test drives the REAL verb path
// (run([...])) end to end, scratch-only and deterministic:
//
//	legacy arm → exit 0; one journal arm record with canonical hyphenated
//	             kinds, the canonical backup param, and the legacy_spelling
//	             annotation;
//	revert --all → exit 0; the target measurably restored;
//	canonical control → still exit 0, unannotated;
//	file_exists inverse → still refused fail-closed, nothing minted.
func TestArmLegacyKindNormalizesBeforePreOpenGate(t *testing.T) {
	writeSanctionMarker(t)

	scratch := t.TempDir()
	backup := filepath.Join(scratch, "target.pristine")
	target := filepath.Join(scratch, "target")
	const pristine, faulted = "pristine\n", "faulted\n"
	for _, p := range []string{backup, target} {
		if err := os.WriteFile(p, []byte(pristine), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	// ── leg 1: the legacy CLI input arms through the pre-open gate. ──────
	runDir := filepath.Join(t.TempDir(), "runs")
	legacy := []string{
		"arm",
		"--fault", "S-001",
		"--target", "scratch:" + target,
		"--ttl", "30s",
		"--inverse-kind", "restore_file",
		"--inverse-params", "path=" + target + ",from=" + backup,
		"--check-kind", "file_matches_backup",
		"--check-params", "path=" + target + ",backup=" + backup,
		"--dir", runDir,
	}
	code, out := captureStdout(t, func() int { return run(legacy) })
	if code != 0 || !strings.Contains(out, "armed ") {
		t.Fatalf("legacy arm: exit %d out=%q, want 0 + armed", code, out)
	}

	// The journal carries ONE arm record with canonical hyphenated kinds and
	// the canonical backup param; the legacy spellings survive only inside
	// the legacy_spelling annotation (the record of the rename, never the
	// declared vocabulary).
	b, err := os.ReadFile(filepath.Join(runDir, "reverter.jsonl"))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	lines := nonEmptyLines(string(b))
	if len(lines) != 1 {
		t.Fatalf("%d journal lines, want 1: %v", len(lines), lines)
	}
	line := lines[0]
	for _, banned := range []string{`"kind":"restore_file"`, `"kind":"file_matches_backup"`, `"from":`} {
		if strings.Contains(line, banned) {
			t.Fatalf("journal line carries the legacy spelling %s: %s", banned, line)
		}
	}
	for _, want := range []string{`"kind":"restore-file"`, `"kind":"file-matches-backup"`, `"backup":"`, "legacy_spelling"} {
		if !strings.Contains(line, want) {
			t.Fatalf("journal line lacks %q: %s", want, line)
		}
	}
	// (The annotation's "->" is JSON-escaped as \u003e in the raw bytes;
	// assert the escaped form the file actually carries.)
	for _, note := range []string{
		`inverse.kind restore_file-\u003erestore-file`,
		`param from-\u003ebackup`,
		`check.kind file_matches_backup-\u003efile-matches-backup`,
	} {
		if !strings.Contains(line, note) {
			t.Fatalf("legacy_spelling annotation does not name %q: %s", note, line)
		}
	}

	// The fault lands AFTER the write-ahead arm; the kill switch must
	// measure the restore: exit 0, the target's pristine bytes back.
	if err := os.WriteFile(target, []byte(faulted), 0o644); err != nil {
		t.Fatalf("land fault: %v", err)
	}
	code, out = captureStdout(t, func() int { return run([]string{"revert", "--all", "--dir", runDir}) })
	if code != 0 || !strings.Contains(out, "kill switch: 1 holds reverted") {
		t.Fatalf("revert --all after legacy arm: exit %d out=%q, want 0 + 1 reverted", code, out)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != pristine {
		t.Fatalf("target not restored: %q err=%v, want %q", got, err, pristine)
	}

	// ── leg 2: the canonical hyphen control arms exactly as before, with
	// no legacy_spelling annotation (normalization is a no-op there). ─────
	ctrlDir := filepath.Join(t.TempDir(), "runs")
	canonical := []string{
		"arm",
		"--fault", "S-001",
		"--target", "scratch:" + target,
		"--ttl", "30s",
		"--inverse-kind", "restore-file",
		"--inverse-params", "path=" + target + ",backup=" + backup,
		"--check-kind", "file-matches-backup",
		"--check-params", "path=" + target + ",backup=" + backup,
		"--dir", ctrlDir,
	}
	code, out = captureStdout(t, func() int { return run(canonical) })
	if code != 0 || !strings.Contains(out, "armed ") {
		t.Fatalf("canonical control arm: exit %d out=%q, want 0 + armed", code, out)
	}
	ctrl, err := os.ReadFile(filepath.Join(ctrlDir, "reverter.jsonl"))
	if err != nil {
		t.Fatalf("read control journal: %v", err)
	}
	if strings.Contains(string(ctrl), "legacy_spelling") {
		t.Fatalf("canonical control annotated legacy_spelling: %s", ctrl)
	}

	// ── leg 3: file_exists has no canonical equivalent — offered as the
	// inverse it stays fail-closed at the pre-open gate: exit 2, the
	// refusal names the untouched spelling, and NO run dir is minted (the
	// refusal precedes reverter.Open, the MISCHIEF-DF-72 property). ───────
	badDir := filepath.Join(t.TempDir(), "runs")
	badParent := filepath.Dir(badDir)
	bad := append([]string{}, canonical...)
	for i, a := range bad {
		switch a {
		case "--inverse-kind":
			bad[i+1] = "file_exists"
		case "--dir":
			bad[i+1] = badDir
		}
	}
	code, stderr := captureStderr(t, func() int { return run(bad) })
	if code != 2 {
		t.Fatalf("file_exists inverse: exit %d stderr=%q, want 2", code, stderr)
	}
	if !strings.Contains(stderr, "file_exists") || !strings.Contains(stderr, "unknown") {
		t.Fatalf("refusal does not name the unmappable spelling as unknown: %s", stderr)
	}
	if ents, err := os.ReadDir(badParent); err != nil || len(ents) != 0 {
		t.Fatalf("refused arm minted %s or wrote beside it: ents=%v err=%v", badDir, ents, err)
	}
}

// nonEmptyLines splits a journal file's bytes into its non-blank lines.
func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
