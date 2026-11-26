package profile

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// fsynclie.go — the S-010 (fsync lie) actuator: a successful fsync/fdatasync
// that does not advance stable storage, landed on a REAL database process.
//
// What the lie IS: S-010 is the storage-side sibling of S-001/S-004 — the sync
// call returns success, so a commit path that treats the return as its
// durability gate believes the record is stable. Nothing reports the gap; the
// acked bytes are volatile; the power loss keeps the ledger and loses the
// record. The descriptor's landed_proof names exactly that pairing: the child
// reports successful syncs before every ack, and stable storage holds none of
// the acked records afterwards.
//
// The L0 shape (mirrors the S-004 short-write posture — the executor builds
// and spawns the target child; it never attaches to a live process):
//
//  1. GENERATE a controlled commit-log database child and an fsync-lie
//     LD_PRELOAD shim, per run, in the run dir's shim/ directory. The child is
//     a REAL process with a REAL database file and a REAL ack ledger; its
//     commit discipline is the TARGET's property and comes from the selector
//     (dbdurability:<fsync|none>:<path>), not from the fault.
//  2. CONTROL RUN: commit + recover without the fault env. Without the fault
//     nothing may be lost (the parent independently reads the database file),
//     or the harness cannot attribute a later loss to the lie.
//  3. FAULTED RUN: the same commit phase with the shim attached. The shim
//     models the volatile write-back layer the lie hides: writes to the
//     database file are accepted into the shim's own memory (the cache took
//     them; the caller saw full success), and fsync/fdatasync on the database
//     file returns success WITHOUT flushing the cache or syncing the file —
//     literally a successful sync that does not advance stable storage. The
//     commit child's exit is the simulated power loss: the volatile layer
//     lives in the child's address space and dies with it. The PARENT then
//     reads the database file — an injector-independent observation — and the
//     acked records are gone. The ledger (a different path the shim never
//     touches, fsynced for real by the child) still says acked.
//  4. RECOVERY: the same child re-runs without the fault env, reads the
//     ledger and the database file back and reports what survived. An fsync
//     discipline verifies every acked record against the file and names what
//     is lost; a none discipline trusts the ack. The probe grades that
//     report against the parent's own read.
//  5. INVERSE: the same commit + recover re-run with the fault env withdrawn
//     must leave every acked record present and confirmed — per-child arming
//     means there is no persistent rule to remove; the re-run MEASURES it.
//
// The probe (commit-durable) grades the TARGET's durability discipline, never
// the injector: every acked commit must have been covered by a successful
// sync of the database file before its ack, and no record absent from stable
// storage may be claimed durable by the target's own recovery report. A
// fsync-on-commit target under the lie passes (it synced before every ack and
// its recovery names the loss instead of claiming it); a no-fsync target
// fails with cause_class durability:storage-ack-not-durable.
//
// A missing compiler, an unparseable commits count, a selector naming a
// discipline this build does not ship, or a control run that loses records
// are all NOT-LANDED results with a named reason — none of them can pass.

// fsyncLieCounterRel is the shim counter's path relative to the run dir
// (shortWriteCounterRel precedent: relative, so two same-seed run journals
// are byte-identical across --dir roots).
const fsyncLieCounterRel = "shim/fsynclie.counter"

// durabilityPayloadBytes is the per-record payload size.
const durabilityPayloadBytes = 32

// durabilityAcklogSuffix is the ack ledger's name: the database file's path
// plus this suffix. The ledger is the target's durable ack log; the shim
// never matches it, so its fsyncs are real under the fault.
const durabilityAcklogSuffix = ".acklog"

// maxCommits bounds the controlled child's record count (the shim's volatile
// cache is sized for it; a bound is a named refusal, not a silent spill).
const maxCommits = 4096

// FsyncLie is the S-010 actuator.
type FsyncLie struct{}

// durabilityReport is one child execution's phase report.
type durabilityReport struct {
	Phase      string
	Discipline string
	Commits    int
	Acks       int
	Syncs      int
	SyncFails  int
	Present    int
	Durable    int
	Lost       int
	Checked    int
}

// Apply implements FaultActuator.
func (f *FsyncLie) Apply(ctx context.Context, req LandRequest) LandResult {
	if req.Target.Kind != "dbdurability" || req.Target.Path == "" {
		return LandResult{Reason: fmt.Sprintf(
			"target kind %q cannot be faulted by the fsync-lie actuator (want dbdurability:<fsync|none>:<path>)", req.Target.Kind)}
	}
	if disc := req.Target.Discipline; disc != "fsync" && disc != "none" {
		return LandResult{Reason: fmt.Sprintf(
			"target discipline %q is not one of fsync|none — the controlled database cannot be built", disc)}
	}
	dbFile := req.Target.Path
	ledgerFile := dbFile + durabilityAcklogSuffix
	commits, err := parseCommits(orDefault(req.Step.Params["commits"], "3"))
	if err != nil {
		return LandResult{Reason: fmt.Sprintf("param commits: %v", err)}
	}
	if _, err := exec.LookPath("cc"); err != nil {
		return LandResult{Reason: "no C compiler (cc) on PATH: the fsync-lie shim and the controlled database cannot be built"}
	}

	shimDir := filepath.Join(req.Dir, "shim")
	if err := os.MkdirAll(shimDir, 0o700); err != nil {
		return LandResult{Reason: fmt.Sprintf("shim dir: %v", err)}
	}
	payload := scenarioPayload(req.RunID, req.Seed, durabilityPayloadBytes)
	dbBin, err := buildDurabilityChild(shimDir, payload)
	if err != nil {
		return LandResult{Reason: err.Error()}
	}
	soPath, err := buildFsyncLieShim(shimDir)
	if err != nil {
		return LandResult{Reason: err.Error()}
	}

	// 1. Control: the harness must discriminate before it may fault anything.
	//    Without the fault env nothing may be lost.
	ctrlCommit, err := runDurabilityChild(ctx, dbBin, dbFile, ledgerFile, "commit", req.Target.Discipline, commits, nil)
	if err != nil {
		return LandResult{Reason: "control commit run: " + err.Error()}
	}
	if ctrlCommit.Acks != commits || ctrlCommit.SyncFails != 0 {
		return LandResult{Reason: fmt.Sprintf(
			"control commit run was not clean (acks=%d commits=%d syncfails=%d): without the fault the child must ack every commit with no sync failure — the harness cannot attribute a loss",
			ctrlCommit.Acks, ctrlCommit.Commits, ctrlCommit.SyncFails)}
	}
	if _, err := runDurabilityChild(ctx, dbBin, dbFile, ledgerFile, "recover", req.Target.Discipline, commits, nil); err != nil {
		return LandResult{Reason: "control recover run: " + err.Error()}
	}
	ctrlStable, err := countCommitRecords(dbFile)
	if err != nil {
		return LandResult{Reason: fmt.Sprintf("control database read: %v", err)}
	}
	ctrlAcks, err := countLedgerAcks(ledgerFile)
	if err != nil {
		return LandResult{Reason: fmt.Sprintf("control ack log read: %v", err)}
	}
	if ctrlAcks != commits || ctrlStable != commits {
		return LandResult{Reason: fmt.Sprintf(
			"control run lost records without any fault (ack log=%d, records on file=%d, commits=%d): the harness cannot discriminate a durability lie",
			ctrlAcks, ctrlStable, commits)}
	}

	// 2. Faulted commit run: the shim attached. The child's exit is the
	//    simulated power loss — the volatile layer dies with it.
	proofPath := filepath.Join(shimDir, "fsynclie.counter")
	env := []string{
		"LD_PRELOAD=" + soPath,
		"MCDUR_PATH=" + dbFile,
		"MCDUR_PROOF=" + proofPath,
	}
	faulted, err := runDurabilityChild(ctx, dbBin, dbFile, ledgerFile, "commit", req.Target.Discipline, commits, env)
	if err != nil {
		return LandResult{Reason: "faulted commit run: " + err.Error()}
	}
	// The parent's independent post-exit read: what stable storage holds.
	stable, err := countCommitRecords(dbFile)
	if err != nil {
		return LandResult{Reason: fmt.Sprintf("database read after the commit child exited: %v", err)}
	}
	ledgerAcks, err := countLedgerAcks(ledgerFile)
	if err != nil {
		return LandResult{Reason: fmt.Sprintf("ack log read after the commit child exited: %v", err)}
	}
	hits, stashEvents, counterErr := readFsyncLieCounter(proofPath)
	if faulted.Acks != commits {
		return LandResult{Reason: fmt.Sprintf(
			"the faulted child did not ack its commits (acks=%d commits=%d)", faulted.Acks, faulted.Commits)}
	}
	if ledgerAcks != commits {
		return LandResult{Reason: fmt.Sprintf(
			"the ack log holds %d acks, want %d — the ack channel the recovery reads is broken", ledgerAcks, commits)}
	}
	if stable >= faulted.Acks {
		return LandResult{Reason: fmt.Sprintf(
			"no acked record was lost (acked=%d stable=%d): stable storage holds everything the child acked — the lie did not land", faulted.Acks, stable)}
	}
	if counterErr != nil || (hits < 1 && stashEvents < 1) {
		return LandResult{Reason: fmt.Sprintf(
			"the shim counter records no fsync-lie hit (hits=%d stash_events=%d, %v): the lie was not delivered", hits, stashEvents, counterErr)}
	}

	// 3. Recovery: the target reads back and reports durability — with the
	//    fault env withdrawn, so its report is about the real state.
	recovered, err := runDurabilityChild(ctx, dbBin, dbFile, ledgerFile, "recover", req.Target.Discipline, commits, nil)
	if err != nil {
		return LandResult{Reason: "faulted recover run: " + err.Error()}
	}

	obs := Observation{
		TargetPath:     dbFile,
		PayloadLen:     durabilityPayloadBytes,
		Acks:           faulted.Acks,
		Syncs:          faulted.Syncs,
		SyncFails:      faulted.SyncFails,
		StableRecords:  stable,
		ClaimedDurable: recovered.Durable,
		Lost:           recovered.Lost,
		LieHits:        hits,
		Note: fmt.Sprintf(
			"control run acked %d with %d on file; the faulted run lost %d of %d acked record(s)",
			ctrlCommit.Acks, ctrlStable, faulted.Acks-stable, faulted.Acks),
	}
	return LandResult{
		Landed: true,
		Obs:    obs,
		Witness: &Witness{
			Kind:   "external",
			Source: "commit child report + the parent's post-exit read of the database file",
			Evidence: fmt.Sprintf(
				"the commit child reported acks=%d syncs=%d syncfails=%d (the target's own report) and its ack log %s holds %d acks; after the child exited the parent read the database file: %d of %d acked commit records present — the successful sync did not advance stable storage; shim counter hits=%d stash_events=%d at %s (injector corroboration); recovery then reported durable=%d lost=%d",
				faulted.Acks, faulted.Syncs, faulted.SyncFails, durabilityAcklogSuffix, ledgerAcks,
				stable, faulted.Acks, hits, stashEvents, fsyncLieCounterRel, recovered.Durable, recovered.Lost),
		},
	}
}

// Detach implements FaultActuator: the descriptor's inverse is "with the
// fault withdrawn the same commit run leaves every acked record present and
// confirmed", so the fault is gone exactly when a fresh commit + recover
// without the fault env produces what the recovery report can verify.
func (f *FsyncLie) Detach(ctx context.Context, req LandRequest) DetachResult {
	dbFile := req.Target.Path
	ledgerFile := dbFile + durabilityAcklogSuffix
	shimDir := filepath.Join(req.Dir, "shim")
	dbBin := filepath.Join(shimDir, "dbchild")
	if _, err := os.Stat(dbBin); err != nil {
		return DetachResult{Reason: fmt.Sprintf("controlled database missing (%v): the inverse cannot be measured", err)}
	}
	commits, err := parseCommits(orDefault(req.Step.Params["commits"], "3"))
	if err != nil {
		return DetachResult{Reason: fmt.Sprintf("param commits: %v", err)}
	}
	commit, err := runDurabilityChild(ctx, dbBin, dbFile, ledgerFile, "commit", req.Target.Discipline, commits, nil)
	if err != nil {
		return DetachResult{Reason: "control re-run: " + err.Error()}
	}
	recovered, err := runDurabilityChild(ctx, dbBin, dbFile, ledgerFile, "recover", req.Target.Discipline, commits, nil)
	if err != nil {
		return DetachResult{Reason: "control re-run recovery: " + err.Error()}
	}
	stable, err := countCommitRecords(dbFile)
	if err != nil {
		return DetachResult{Reason: fmt.Sprintf("database read: %v", err)}
	}
	if commit.Acks != commits || commit.SyncFails != 0 || stable != commits {
		return DetachResult{Reason: fmt.Sprintf(
			"the next commit run still lost records (acks=%d syncfails=%d on-file=%d commits=%d)",
			commit.Acks, commit.SyncFails, stable, commits)}
	}
	if recovered.Durable != commits {
		return DetachResult{Reason: fmt.Sprintf(
			"the recovery report still does not confirm every acked record durable (durable=%d lost=%d of %d)",
			recovered.Durable, recovered.Lost, commits)}
	}
	return DetachResult{Witness: &Witness{
		Kind:   "external",
		Source: "control re-run with the fault env withdrawn",
		Evidence: fmt.Sprintf(
			"the next commit run left all %d records on the database file and the recovery report confirms durable=%d lost=%d — no lie survives the child",
			commits, recovered.Durable, recovered.Lost),
	}}
}

// parseCommits parses the commits count: at least one, bounded by maxCommits
// (the shim's volatile cache is sized for the bound).
func parseCommits(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%q is not an integer", s)
	}
	if n < 1 {
		return 0, fmt.Errorf("%d is fewer than one commit", n)
	}
	if n > maxCommits {
		return 0, fmt.Errorf("%d exceeds the %d-commit bound (the shim's volatile cache is sized for it)", n, maxCommits)
	}
	return n, nil
}

// buildDurabilityChild generates and compiles the controlled commit-log
// database (a real process: real files, real syscalls, real exit).
func buildDurabilityChild(dir string, payload []byte) (string, error) {
	src := filepath.Join(dir, "dbchild.c")
	if err := os.WriteFile(src, []byte(durabilityChildC(payload)), 0o600); err != nil {
		return "", fmt.Errorf("database source: %v", err)
	}
	bin := filepath.Join(dir, "dbchild")
	if b, err := exec.Command("cc", "-O2", "-o", bin, src).CombinedOutput(); err != nil {
		return "", fmt.Errorf("database build: %v: %s", err, strings.TrimSpace(string(b)))
	}
	return bin, nil
}

// buildFsyncLieShim generates and compiles the fsync-lie LD_PRELOAD shim.
func buildFsyncLieShim(dir string) (string, error) {
	src := filepath.Join(dir, "libfsynclie.c")
	if err := os.WriteFile(src, []byte(fsyncLieShimC), 0o600); err != nil {
		return "", fmt.Errorf("shim source: %v", err)
	}
	so := filepath.Join(dir, "libfsynclie.so")
	cmd := exec.Command("cc", "-shared", "-fPIC", "-O2", "-o", so, src, "-ldl")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("shim build: %v: %s", err, strings.TrimSpace(string(b)))
	}
	return so, nil
}

// runDurabilityChild executes one child phase and parses its report line.
// env nil means no fault attachment (control / recovery / inverse).
func runDurabilityChild(ctx context.Context, bin, dbFile, ledgerFile, phase, discipline string, commits int, env []string) (durabilityReport, error) {
	cmd := exec.CommandContext(ctx, bin, dbFile, ledgerFile, phase, discipline, strconv.Itoa(commits))
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	rep := durabilityReport{}
	report := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "phase=") {
			report = strings.TrimSpace(line)
		}
	}
	var stderr string
	if ee, ok := err.(*exec.ExitError); ok {
		stderr = strings.TrimSpace(string(ee.Stderr))
	}
	if err != nil {
		return rep, fmt.Errorf("%v: %s", err, nonEmpty(stderr, strings.TrimSpace(string(out))))
	}
	if report == "" {
		return rep, fmt.Errorf("child printed no report line (got %q)", strings.TrimSpace(string(out)))
	}
	for _, field := range strings.Fields(report) {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch k {
		case "phase":
			rep.Phase = v
		case "discipline":
			rep.Discipline = v
		default:
			n, convErr := strconv.Atoi(v)
			if convErr != nil {
				return rep, fmt.Errorf("child report %q: %s is not an integer", report, k)
			}
			switch k {
			case "commits":
				rep.Commits = n
			case "acks":
				rep.Acks = n
			case "syncs":
				rep.Syncs = n
			case "syncfails":
				rep.SyncFails = n
			case "present":
				rep.Present = n
			case "durable":
				rep.Durable = n
			case "lost":
				rep.Lost = n
			case "checked":
				rep.Checked = n
			case "payload":
				// informational; not graded
			}
		}
	}
	if rep.Phase != phase {
		return rep, fmt.Errorf("child report %q names phase %q, want %q", report, rep.Phase, phase)
	}
	return rep, nil
}

// countCommitRecords is the PARENT's independent read of the database file:
// how many COMMIT records stable storage holds. This is the external loss
// witness — the injector does not write this file's bytes under the fault.
func countCommitRecords(path string) (int, error) {
	return countTaggedLines(path, "COMMIT")
}

// countLedgerAcks reads the ack ledger back (the target's own durable ack
// log; the recovery phase consumes the same file).
func countLedgerAcks(path string) (int, error) {
	return countTaggedLines(path, "ACK")
}

// countTaggedLines counts lines whose first field is tag.
func countTaggedLines(path, tag string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == tag {
			n++
		}
	}
	return n, nil
}

// readFsyncLieCounter reads the shim's injector-owned counter
// ("hits=N stash_events=M rule=..."). The counts are CORROBORATION only: the
// independent witnesses are the child's own report plus the parent's
// post-exit read of the database file.
func readFsyncLieCounter(path string) (hits, stashEvents int, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	for _, field := range strings.Fields(string(b)) {
		if v, ok := strings.CutPrefix(field, "hits="); ok {
			hits, err = strconv.Atoi(v)
			if err != nil {
				return 0, 0, fmt.Errorf("counter hits: %w", err)
			}
		}
		if v, ok := strings.CutPrefix(field, "stash_events="); ok {
			stashEvents, err = strconv.Atoi(v)
			if err != nil {
				return 0, 0, fmt.Errorf("counter stash_events: %w", err)
			}
		}
	}
	if hits == 0 && stashEvents == 0 {
		return 0, 0, fmt.Errorf("counter %s carries no hits= or stash_events= field", path)
	}
	return hits, stashEvents, nil
}

// durabilityChildC renders the controlled commit-log database: a real
// process whose commit discipline is the TARGET's property. Both disciplines
// share the same durable ack ledger; the ONLY difference is whether the
// database file is synced before each ack.
func durabilityChildC(payload []byte) string {
	return fmt.Sprintf(`/* mischief S-010 controlled commit-log database — GENERATED per run, never committed.
 *
 * A real commit-log store in two phases:
 *   commit  argv: dbchild <dbfile> <ledger> commit <fsync|none> <n>
 *     appends n COMMIT records to the database file, syncs it per the
 *     discipline (fsync: one fsync before each ack; none: never), records
 *     every ack in the ledger file (ALWAYS fsynced — the ledger is the
 *     durable ack log both disciplines share) and acks.
 *   recover argv: dbchild <dbfile> <ledger> recover <fsync|none> <n>
 *     reads the ledger and the database file back and reports what survived.
 *     The fsync discipline VERIFIES every acked record against the file and
 *     names what is lost; the none discipline trusts the ack — the bug class
 *     S-010 catches: an ack of durability the storage never confirmed.
 * Prints one phase= report line.
 */
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define MAXSEQ 4096

static const char payload[] = "%s";

static int write_full(int fd, const char *buf, size_t len) {
  size_t off = 0;
  while (off < len) {
    ssize_t n = write(fd, buf + off, len - off);
    if (n < 0) { if (errno == EINTR) continue; return -1; }
    if (n == 0) return -1;
    off += (size_t)n;
  }
  return 0;
}

/* scan_seqs counts the lines whose first field is tag and collects their
 * sequence numbers. Returns the count, or -1 when the file is unreadable. */
static int scan_seqs(const char *path, const char *tag, int *out) {
  FILE *f = fopen(path, "r");
  if (f == NULL) return -1;
  char line[256];
  char word[64];
  unsigned seq;
  int n = 0;
  while (fgets(line, sizeof line, f) != NULL) {
    if (sscanf(line, "%%63s %%u", word, &seq) == 2 && strcmp(word, tag) == 0) {
      if (n < MAXSEQ) out[n] = (int)seq;
      n++;
    }
  }
  fclose(f);
  return n;
}

int main(int argc, char **argv) {
  if (argc < 6) { fprintf(stderr, "usage: dbchild <dbfile> <ledger> <commit|recover> <fsync|none> <n>\n"); return 2; }
  const char *dbfile = argv[1];
  const char *ledger = argv[2];
  const char *phase = argv[3];
  const char *discipline = argv[4];
  int n = atoi(argv[5]);
  if (n < 1 || n > MAXSEQ) { fprintf(stderr, "n out of range\n"); return 2; }
  if (strcmp(discipline, "fsync") != 0 && strcmp(discipline, "none") != 0) {
    fprintf(stderr, "discipline must be fsync or none\n"); return 2;
  }

  if (strcmp(phase, "commit") == 0) {
    int dbfd = open(dbfile, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if (dbfd < 0) { fprintf(stderr, "open %%s: %%s\n", dbfile, strerror(errno)); return 3; }
    int led = open(ledger, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if (led < 0) { fprintf(stderr, "open %%s: %%s\n", ledger, strerror(errno)); return 3; }
    int syncs = 0, syncfails = 0, acks = 0;
    for (int i = 1; i <= n; i++) {
      char rec[256];
      int rl = snprintf(rec, sizeof rec, "COMMIT %%d %%s\n", i, payload);
      if (write_full(dbfd, rec, (size_t)rl) != 0) {
        fprintf(stderr, "database write failed: %%s\n", strerror(errno));
        return 4;
      }
      if (strcmp(discipline, "fsync") == 0) {
        if (fsync(dbfd) != 0) {
          fprintf(stderr, "fsync: %%s\n", strerror(errno));
          return 5;
        }
        syncs++;
      }
      char ack[64];
      int al = snprintf(ack, sizeof ack, "ACK %%d\n", i);
      if (write_full(led, ack, (size_t)al) != 0) {
        fprintf(stderr, "ack log write failed: %%s\n", strerror(errno));
        return 6;
      }
      if (fsync(led) != 0) { fprintf(stderr, "ack log fsync: %%s\n", strerror(errno)); return 7; }
      acks++;
    }
    close(dbfd);
    close(led);
    printf("phase=commit discipline=%%s commits=%%d acks=%%d syncs=%%d syncfails=%%d payload=%%zu\n",
           discipline, n, acks, syncs, syncfails, sizeof(payload) - 1);
    return 0;
  }

  if (strcmp(phase, "recover") == 0) {
    int acks[MAXSEQ];
    int na = scan_seqs(ledger, "ACK", acks);
    if (na < 0) { fprintf(stderr, "ack log unreadable: %%s\n", strerror(errno)); return 3; }
    int checked = strcmp(discipline, "fsync") == 0;
    int present[MAXSEQ];
    int np = -1;
    if (checked) {
      /* a database file the crash lost entirely is a measurement, not a
       * harness error: unreadable counts as zero records present */
      np = scan_seqs(dbfile, "COMMIT", present);
      if (np < 0) np = 0;
    }
    int durable = 0, lost = 0;
    if (checked) {
      for (int i = 0; i < na; i++) {
        int found = 0;
        for (int j = 0; j < np; j++) {
          if (present[j] == acks[i]) { found = 1; break; }
        }
        if (found) durable++; else lost++;
      }
    } else {
      /* the none discipline trusts the ack: it never verified the file */
      durable = na;
      lost = 0;
    }
    printf("phase=recover discipline=%%s acked=%%d present=%%d durable=%%d lost=%%d checked=%%d\n",
           discipline, na, checked ? np : -1, durable, lost, checked);
    return 0;
  }

  fprintf(stderr, "phase must be commit or recover\n");
  return 2;
}
`, string(payload))
}

// fsyncLieShimC is the generated S-010 shim. It models the volatile
// write-back layer the lie hides: database-file writes are accepted into the
// shim's own memory (the caller saw full success), and fsync/fdatasync on
// the database file returns success WITHOUT flushing or syncing. The cache
// lives in this process: the commit child's exit is the simulated power
// loss. Every other fd — the ack ledger, stdout, stderr, the proof counter —
// passes through untouched.
const fsyncLieShimC = `/* mischief S-010 fsync-lie shim — GENERATED per run, never committed.
 *
 * Env: MCDUR_PATH    exact database file path the lie applies to
 *      MCDUR_PROOF   injector counter path (never matched by the rule)
 *
 * write() on the database file is accepted into an in-memory cache and NOT
 * passed to the real file; fsync()/fdatasync() on it return success without
 * flushing the cache or syncing the file — a successful sync that does not
 * advance stable storage. The cache dies with this process: the commit
 * child's exit is the simulated power loss. A write that would overflow the
 * cache spills through to the real file (no longer volatile) — the bounded
 * cache is a named model, not a silent lie about capacity.
 */
#define _GNU_SOURCE
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static ssize_t (*real_write)(int, const void *, size_t);
static int (*real_fsync)(int);
static int (*real_fdatasync)(int);
static char mdur_target[4096];
static char mdur_proof[4096];
static int mdur_ready = 0;
static int mdur_hits = 0;
static int mdur_stash_events = 0;
static char mdur_cache[1 << 20];
static size_t mdur_cache_len = 0;

static int mdur_fd_matches(int fd) {
  char link[64];
  char path[4096];
  snprintf(link, sizeof link, "/proc/self/fd/%d", fd);
  ssize_t n = readlink(link, path, sizeof path - 1);
  if (n <= 0) return 0;
  path[n] = '\0';
  if (mdur_proof[0] != '\0' && strcmp(path, mdur_proof) == 0) return 0;
  return strcmp(path, mdur_target) == 0;
}

static void mdur_landproof(void) {
  if (mdur_proof[0] == '\0' || real_write == NULL) return;
  int f = open(mdur_proof, O_WRONLY | O_CREAT | O_TRUNC, 0600);
  if (f < 0) return;
  char line[4300];
  int n = snprintf(line, sizeof line, "hits=%d stash_events=%d rule=fsync-lie:%s\n",
                   mdur_hits, mdur_stash_events, mdur_target);
  if (n > 0) (void)real_write(f, line, (size_t)n);
  if (real_fsync != NULL) (void)real_fsync(f);
  close(f);
}

ssize_t write(int fd, const void *buf, size_t n) {
  if (real_write == NULL) {
    real_write = (ssize_t (*)(int, const void *, size_t))dlsym(RTLD_NEXT, "write");
  }
  if (real_write == NULL) { errno = ENOSYS; return -1; }
  if (mdur_ready && n > 0 && mdur_fd_matches(fd)) {
    if (mdur_cache_len + n <= sizeof mdur_cache) {
      memcpy(mdur_cache + mdur_cache_len, buf, n);
      mdur_cache_len += n;
      mdur_stash_events++;
      mdur_landproof();
      return (ssize_t)n; /* the volatile cache accepted the write in full */
    }
    /* cache full: spill through to the real file (no longer volatile) */
  }
  return real_write(fd, buf, n);
}

static int mdur_lie(int fd, int (*real_sync)(int)) {
  if (real_sync == NULL) { errno = ENOSYS; return -1; }
  if (mdur_ready && mdur_fd_matches(fd)) {
    mdur_hits++;
    mdur_landproof();
    return 0; /* THE LIE: success, nothing synced, the cache is not flushed */
  }
  return real_sync(fd);
}

int fsync(int fd) {
  if (real_fsync == NULL) {
    real_fsync = (int (*)(int))dlsym(RTLD_NEXT, "fsync");
  }
  return mdur_lie(fd, real_fsync);
}

int fdatasync(int fd) {
  if (real_fdatasync == NULL) {
    real_fdatasync = (int (*)(int))dlsym(RTLD_NEXT, "fdatasync");
  }
  return mdur_lie(fd, real_fdatasync);
}

__attribute__((constructor)) static void mdur_init(void) {
  const char *p = getenv("MCDUR_PATH");
  if (p == NULL || p[0] == '\0') return;
  snprintf(mdur_target, sizeof mdur_target, "%s", p);
  const char *pr = getenv("MCDUR_PROOF");
  if (pr != NULL) snprintf(mdur_proof, sizeof mdur_proof, "%s", pr);
  mdur_ready = 1;
}
`
