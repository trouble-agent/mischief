package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// MISCHIEF-DF-73: battery's journal is the RUN DIR's evidence surface, and
// --out is the ARTEFACT surface — an operator preserving only --out must be
// able to tell from the run's own output where the per-event evidence
// (battery.jsonl) went, and it must never land in a default store the
// operator did not point at.
//
// The regression arms BOTH halves:
//   - a "split" run (--out A, --dir B) puts battery.jsonl in B (the run
//     dir), never in A, and the summary line names B;
//   - a run WITHOUT --out puts BOTH artefact and journal in --dir (the
//     artefact's documented default is the run dir), so the default store
//     (~/.mischief/runs/default) is never touched when --dir is passed.

// TestBatteryJournalFollowsDirNotOut: --out moves the matrix artefact,
// --dir moves the journal, and the summary line names BOTH so an operator
// can reconstruct the cell grading from either surface alone.
func TestBatteryJournalFollowsDirNotOut(t *testing.T) {
	writeSanctionMarker(t) // the MSF-032 gate runs before the live matrix
	writeHealthyLoad(t)    // MISCHIEF-DF-28: the AC-8 gate reads the injected posture, not the box's /proc
	base := t.TempDir()
	runDir := filepath.Join(base, "run")
	outDir := filepath.Join(base, "out")
	code, out := captureStdout(t, func() int {
		return run([]string{"battery", "--quick", "--dir", runDir, "--out", outDir})
	})
	if code != 0 {
		t.Fatalf("quick run exit %d\nstdout:\n%s", code, out)
	}

	// The journal landed in the RUN dir, not --out.
	jb, err := os.ReadFile(filepath.Join(runDir, "battery.jsonl"))
	if err != nil {
		t.Fatalf("journal not in --dir: %v\nstdout:\n%s", err, out)
	}
	if _, serr := os.Stat(filepath.Join(outDir, "battery.jsonl")); !os.IsNotExist(serr) {
		t.Errorf("journal must NOT land in --out (stat err=%v)", serr)
	}
	if ents, _ := os.ReadDir(outDir); len(ents) == 0 {
		t.Errorf("--out is empty: the matrix artefact never landed")
	}
	for _, ln := range strings.Split(strings.TrimRight(string(jb), "\n"), "\n") {
		if !strings.Contains(ln, `"run_id":"`) {
			t.Errorf("journal line not a record: %q", ln)
		}
	}

	// The summary names the journal's real path (the measured location),
	// with the run's per-event evidence reachable from the output alone.
	var journalLine string
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "journal: ") {
			journalLine = ln
		}
	}
	if journalLine == "" {
		t.Fatalf("no journal line in the summary:\n%s", out)
	}
	if !strings.Contains(journalLine, filepath.Join(runDir, "battery.jsonl")) {
		t.Errorf("journal line does not name --dir's journal:\n%s", journalLine)
	}
	if strings.Contains(journalLine, outDir) {
		t.Errorf("journal line claims --out for the journal:\n%s", journalLine)
	}
}

// TestBatteryDefaultArtefactHomesToDir: with NO --out, both the artefact
// and the journal land in --dir (the artefact's documented default is "the
// run dir"), so --dir alone is the self-contained evidence home and the
// hardcoded defaultRunDir store is never touched.
func TestBatteryDefaultArtefactHomesToDir(t *testing.T) {
	writeSanctionMarker(t)
	writeHealthyLoad(t)
	runDir := t.TempDir()
	code, out := captureStdout(t, func() int {
		return run([]string{"battery", "--quick", "--dir", runDir})
	})
	if code != 0 {
		t.Fatalf("quick run exit %d\nstdout:\n%s", code, out)
	}
	for _, f := range []string{"battery.jsonl", "matrix.json", "matrix.md"} {
		if _, err := os.Stat(filepath.Join(runDir, f)); err != nil {
			t.Errorf("%s not in --dir (the run dir is the evidence home): %v\nstdout:\n%s", f, err, out)
		}
	}
}