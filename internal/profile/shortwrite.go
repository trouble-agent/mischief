package profile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// shortwrite.go — the S-004 (short write) actuator: the one primitive this
// build can inject end to end.
//
// What a short write IS (and why it is not an errno fault): POSIX lets a
// successful write(2) land FEWER bytes than it was given, returning the count
// it landed with no error. A write path that trusts the call — "no error, so
// everything is written" — silently drops the remainder. Nothing reports it,
// the file is short, and the caller believes it succeeded. S-001 (write EIO)
// is the easy version of the same class: that one at least returns -1.
//
// How this actuator lands it (the L0 scratch shape):
//
//  1. GENERATE a short-write LD_PRELOAD shim, per run, beside the profile's
//     generated scenario writer. The shim matches ONE exact target path
//     (resolved through /proc/self/fd/N, never a substring), shortens at most
//     `budget` matching writes by `fraction`, writes the proof counter to its
//     own path (self-excluded), and returns the short count with NO error.
//  2. CONTROL RUN: the scenario writer writes the payload WITHOUT the shim
//     env. It must land every byte in exactly one write(2), or the run cannot
//     discriminate a fault from a broken harness (inconclusive, not a pass).
//  3. FAULTED RUN: the same writer, same payload, with the shim attached. The
//     landing witness is the writer's own short-write count plus the shim's
//     counter: a short write that reached the syscall boundary is what the
//     descriptor's landed_proof names.
//  4. The parent independently reads the target's size afterwards and hands
//     that observation to the probe. The probe grades; the actuator does not.
//  5. INVERSE (descriptor `inverse`): the same write re-run once more with the
//     fault env withdrawn must apply the whole payload in one call —
//     "next write applies all bytes". Per-child arming means there is no
//     persistent rule to remove; the re-run MEASURES that, it does not assume
//     it.
//
// The scenario writer implements the CORRECT discipline (advance by the return
// count, continue until the buffer is on the file), so the shipped profile's
// expected outcome is passed: the probe proves the injected short write
// reached the write path and that a correct loop still lands the payload. The
// fail arm is covered by the executor's unit tests with a probe-breaking
// actuator — a shipped scenario that always fails its own target would tell an
// agent its code is broken when it is not.
//
// A missing compiler, an unparseable fraction, a target that does not match
// the step's path_substr, or a dirty control run are all NOT-LANDED results
// with a named reason — the grader turns them into inconclusive. None of them
// can produce a pass.

// shortWriteCounterRel is the shim counter's path relative to the run dir.
// Witness evidence embeds the RELATIVE path, not the absolute one: the
// journal is the injection log that `profile compare` diffs between two run
// dirs, and an absolute counter path would differ for every --dir root the
// operator picks, breaking the byte-identity of two same-seed journals
// (PROF-031).
const shortWriteCounterRel = "shim/shortwrite.counter"

// shortWritePayloadBytes is the scenario payload size. 32 bytes is small
// enough that a single write(2) is the whole payload (so a short write leaves
// a visibly short file) and large enough that half of it is non-zero.
const shortWritePayloadBytes = 32

// ShortWrite is the S-004 actuator.
type ShortWrite struct{}

// writerRun is one scenario-writer execution, as measured from its report.
type writerRun struct {
	Attempts int
	Shorts   int
	Wrote    int
	Payload  int
	Stderr   string
}

// Apply implements FaultActuator.
func (s *ShortWrite) Apply(ctx context.Context, req LandRequest) LandResult {
	target := req.Target.Path
	if req.Target.Kind != "scratch" || target == "" {
		return LandResult{Reason: fmt.Sprintf("target kind %q cannot be faulted by the short-write actuator (want scratch:<path>)", req.Target.Kind)}
	}
	if sub := req.Step.Params["path_substr"]; sub != "" && !strings.Contains(target, sub) {
		return LandResult{Reason: fmt.Sprintf("target %s does not contain the step's path_substr %q — the rule would never match", target, sub)}
	}
	num, den, err := parseFraction(orDefault(req.Step.Params["fraction"], "0.5"))
	if err != nil {
		return LandResult{Reason: fmt.Sprintf("param fraction: %v", err)}
	}
	budget, err := parseBudget(orDefault(req.Step.Params["budget"], "1"))
	if err != nil {
		return LandResult{Reason: fmt.Sprintf("param budget: %v", err)}
	}
	if _, err := exec.LookPath("cc"); err != nil {
		return LandResult{Reason: "no C compiler (cc) on PATH: the LD_PRELOAD shim cannot be built"}
	}

	shimDir := filepath.Join(req.Dir, "shim")
	if err := os.MkdirAll(shimDir, 0o700); err != nil {
		return LandResult{Reason: fmt.Sprintf("shim dir: %v", err)}
	}
	payload := scenarioPayload(req.RunID, req.Seed, shortWritePayloadBytes)
	writerBin, err := buildScenarioWriter(shimDir, payload)
	if err != nil {
		return LandResult{Reason: err.Error()}
	}
	soPath, err := buildShortWriteShim(shimDir)
	if err != nil {
		return LandResult{Reason: err.Error()}
	}

	// 1. Control: the harness must discriminate before it may fault anything.
	ctrl, err := runScenarioWriter(ctx, writerBin, target, nil)
	if err != nil {
		return LandResult{Reason: "control run: " + err.Error()}
	}
	if ctrl.Attempts != 1 || ctrl.Shorts != 0 || ctrl.Wrote != ctrl.Payload {
		return LandResult{Reason: fmt.Sprintf(
			"control run was not clean (attempts=%d shorts=%d wrote=%d payload=%d): without the shim the payload must land in ONE write — the harness cannot discriminate a short write",
			ctrl.Attempts, ctrl.Shorts, ctrl.Wrote, ctrl.Payload)}
	}

	// 2. Faulted run.
	proofPath := filepath.Join(shimDir, "shortwrite.counter")
	env := []string{
		"LD_PRELOAD=" + soPath,
		"MCSHORT_PATH=" + target,
		"MCSHORT_PROOF=" + proofPath,
		"MCSHORT_BUDGET=" + strconv.Itoa(budget),
		fmt.Sprintf("MCSHORT_FRACTION=%d/%d", num, den),
	}
	faulted, err := runScenarioWriter(ctx, writerBin, target, env)
	if err != nil {
		return LandResult{Reason: "faulted run: " + err.Error()}
	}
	onDisk, statErr := fileSize(target)
	if statErr != nil {
		return LandResult{Reason: fmt.Sprintf("target stat: %v", statErr)}
	}
	hits, _ := readCounterHits(proofPath)
	if faulted.Shorts == 0 || hits == 0 {
		return LandResult{Reason: fmt.Sprintf(
			"no short write was delivered (shorts=%d shim hits=%d): the rule did not match %s", faulted.Shorts, hits, target)}
	}

	obs := Observation{
		TargetPath:  target,
		PayloadLen:  faulted.Payload,
		OnDisk:      onDisk,
		Attempts:    faulted.Attempts,
		ShortWrites: faulted.Shorts,
		Note:        fmt.Sprintf("first write asked %d got %d", faulted.Payload, firstShortCount(faulted, den, num)),
	}
	return LandResult{
		Landed: true,
		Obs:    obs,
		Witness: &Witness{
			Kind:   "external",
			Source: "target child writes(2) return counts + shim counter",
			Evidence: fmt.Sprintf(
				"the write path received %d short write(s) (%s): attempts=%d payload=%d; injector counter hits=%d at %s; target %s now holds %d bytes",
				faulted.Shorts, obs.Note, faulted.Attempts, faulted.Payload, hits, shortWriteCounterRel, target, onDisk),
		},
	}
}

// Detach implements FaultActuator: the descriptor's inverse is "next write
// applies all bytes", so the fault is gone exactly when a fresh write without
// the fault env lands the whole payload in one call.
func (s *ShortWrite) Detach(ctx context.Context, req LandRequest) DetachResult {
	target := req.Target.Path
	shimDir := filepath.Join(req.Dir, "shim")
	writerBin := filepath.Join(shimDir, "writer")
	if _, err := os.Stat(writerBin); err != nil {
		return DetachResult{Reason: fmt.Sprintf("scenario writer missing (%v): the inverse cannot be measured", err)}
	}
	run, err := runScenarioWriter(ctx, writerBin, target, nil)
	if err != nil {
		return DetachResult{Reason: "control re-run: " + err.Error()}
	}
	onDisk, statErr := fileSize(target)
	if statErr != nil {
		return DetachResult{Reason: fmt.Sprintf("target stat: %v", statErr)}
	}
	if run.Shorts != 0 || run.Attempts != 1 || onDisk != int64(run.Payload) {
		return DetachResult{Reason: fmt.Sprintf(
			"next write still did not apply all bytes (attempts=%d shorts=%d on-disk=%d payload=%d)", run.Attempts, run.Shorts, onDisk, run.Payload)}
	}
	return DetachResult{Witness: &Witness{
		Kind:   "external",
		Source: "control re-run with the fault env withdrawn",
		Evidence: fmt.Sprintf("the next write applied all %d bytes in one call (shorts=0) and %s holds %d bytes — no rule survives the child",
			run.Payload, target, onDisk),
	}}
}

// firstShortCount renders what the first shortened write was asked for and
// what it got, derived from the configured fraction (the writer's stderr line
// carries the same pair verbatim; this keeps the evidence line readable).
func firstShortCount(run writerRun, den, num int) int {
	got := run.Payload * num / den
	if got == 0 {
		got = 1
	}
	if got >= run.Payload {
		got = run.Payload - 1
	}
	return got
}

// buildScenarioWriter generates and compiles the reference writer.
func buildScenarioWriter(dir string, payload []byte) (string, error) {
	src := filepath.Join(dir, "writer.c")
	if err := os.WriteFile(src, []byte(scenarioWriterC(payload)), 0o600); err != nil {
		return "", fmt.Errorf("writer source: %v", err)
	}
	bin := filepath.Join(dir, "writer")
	if b, err := exec.Command("cc", "-O2", "-o", bin, src).CombinedOutput(); err != nil {
		return "", fmt.Errorf("writer build: %v: %s", err, strings.TrimSpace(string(b)))
	}
	return bin, nil
}

// buildShortWriteShim generates and compiles the short-write LD_PRELOAD shim.
func buildShortWriteShim(dir string) (string, error) {
	src := filepath.Join(dir, "libshortwrite.c")
	if err := os.WriteFile(src, []byte(shortWriteShimC), 0o600); err != nil {
		return "", fmt.Errorf("shim source: %v", err)
	}
	so := filepath.Join(dir, "libshortwrite.so")
	cmd := exec.Command("cc", "-shared", "-fPIC", "-O2", "-o", so, src, "-ldl")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("shim build: %v: %s", err, strings.TrimSpace(string(b)))
	}
	return so, nil
}

// runScenarioWriter executes the writer and parses its report line. env nil
// means the control run (no fault attachment).
func runScenarioWriter(ctx context.Context, bin, target string, env []string) (writerRun, error) {
	cmd := exec.CommandContext(ctx, bin, target)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	run := writerRun{}
	report := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "attempts=") {
			report = strings.TrimSpace(line)
		}
	}
	var stderr string
	if ee, ok := err.(*exec.ExitError); ok {
		stderr = strings.TrimSpace(string(ee.Stderr))
	}
	if err != nil {
		return run, fmt.Errorf("%v: %s", err, nonEmpty(stderr, strings.TrimSpace(string(out))))
	}
	if report == "" {
		return run, fmt.Errorf("writer printed no report line (got %q)", strings.TrimSpace(string(out)))
	}
	for _, field := range strings.Fields(report) {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		n, convErr := strconv.Atoi(v)
		if convErr != nil {
			return run, fmt.Errorf("writer report %q: %s is not an integer", report, k)
		}
		switch k {
		case "attempts":
			run.Attempts = n
		case "shorts":
			run.Shorts = n
		case "wrote":
			run.Wrote = n
		case "payload":
			run.Payload = n
		}
	}
	if run.Payload <= 0 {
		return run, fmt.Errorf("writer report %q carries no payload size", report)
	}
	return run, nil
}

// fileSize is the target's size, or -1 when it does not exist (a fault that
// prevented creation is a measurement, not a read error).
func fileSize(path string) (int64, error) {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return -1, nil
		}
		return 0, err
	}
	return st.Size(), nil
}

// readCounterHits reads the shim's injector-owned counter ("hits=N rule=...").
// The count is CORROBORATION only: the independent witness is the writer's own
// write(2) return counts plus the parent's read of the target.
func readCounterHits(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, field := range strings.Fields(string(b)) {
		if v, ok := strings.CutPrefix(field, "hits="); ok {
			return strconv.Atoi(v)
		}
	}
	return 0, fmt.Errorf("counter %s carries no hits= field", path)
}

// scenarioPayload derives the run's payload from the run id and seed, so the
// same profile + seed writes the same bytes (AC-10's replay precondition).
func scenarioPayload(runID string, seed int64, n int) []byte {
	h := sha256.New()
	fmt.Fprintf(h, "mischief.profile.scenario\n%s\n%d\n", runID, seed)
	hexed := hex.EncodeToString(h.Sum(nil))
	b := []byte(hexed)
	for len(b) < n {
		b = append(b, hexed...)
	}
	return b[:n]
}

// parseFraction parses "1/2" or a decimal in (0,1) ("0.5" -> 50/100). A
// fraction outside (0,1) is refused: a short write that lands everything (or
// nothing) is not a short write.
func parseFraction(s string) (num, den int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, fmt.Errorf("empty fraction")
	}
	if a, b, ok := strings.Cut(s, "/"); ok {
		num, err = strconv.Atoi(strings.TrimSpace(a))
		if err != nil {
			return 0, 0, fmt.Errorf("%q is not a fraction (want num/den or 0<x<1)", s)
		}
		den, err = strconv.Atoi(strings.TrimSpace(b))
		if err != nil || den <= 0 {
			return 0, 0, fmt.Errorf("%q has no positive denominator (want num/den or 0<x<1)", s)
		}
	} else {
		f, convErr := strconv.ParseFloat(s, 64)
		if convErr != nil {
			return 0, 0, fmt.Errorf("%q is not a fraction (want num/den or 0<x<1)", s)
		}
		num, den = int(f*100+0.5), 100
	}
	if num <= 0 || num >= den {
		return 0, 0, fmt.Errorf("%q must be strictly between 0 and 1 (a short write lands fewer bytes, not all of them)", s)
	}
	return num, den, nil
}

// parseBudget parses the matching-write budget; 0 or negative means "every
// matching write is short" (the shim reads <=0 that way).
func parseBudget(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%q is not an integer", s)
	}
	return n, nil
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// scenarioWriterC renders the reference writer: the POSIX-correct loop a
// production write path must have (advance by the return count, continue until
// the whole buffer is written, treat 0 as an error).
func scenarioWriterC(payload []byte) string {
	return fmt.Sprintf(`/* mischief profile scenario writer — GENERATED per run, never committed.
 *
 * Writes PAYLOAD to argv[1] the way a correct write path must: advance by the
 * count write(2) returned and continue until the whole buffer is on the file.
 * Prints attempts=/shorts=/wrote=/payload= so the executor measures what the
 * injected fault actually did instead of trusting the shim.
 */
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

static const char payload[] = "%s";

int main(int argc, char **argv) {
  if (argc < 2) { fprintf(stderr, "usage: writer <path>\n"); return 2; }
  const size_t total = sizeof(payload) - 1;
  int fd = open(argv[1], O_WRONLY | O_CREAT | O_TRUNC, 0600);
  if (fd < 0) { fprintf(stderr, "open %%s: %%s\n", argv[1], strerror(errno)); return 3; }
  size_t off = 0;
  int attempts = 0, shorts = 0;
  while (off < total) {
    size_t want = total - off;
    ssize_t n = write(fd, payload + off, want);
    attempts++;
    if (n < 0) { fprintf(stderr, "write: %%s\n", strerror(errno)); close(fd); return 4; }
    if (n == 0) { fprintf(stderr, "write returned 0\n"); close(fd); return 5; }
    if ((size_t)n < want) { shorts++; fprintf(stderr, "short write: asked %%zu got %%zd\n", want, n); }
    off += (size_t)n;
  }
  if (close(fd) != 0) { fprintf(stderr, "close: %%s\n", strerror(errno)); return 6; }
  printf("attempts=%%d shorts=%%d wrote=%%zu payload=%%zu\n", attempts, shorts, off, total);
  return 0;
}
`, string(payload))
}

// shortWriteShimC is the generated S-004 shim. It shortens matching writes and
// RETAINS no error: the caller must look at the count to notice. Matching is
// exact (resolved through /proc/self/fd), the proof channel is self-excluded,
// and the shortening budget bounds the fault so a retrying writer can finish.
const shortWriteShimC = `/* mischief S-004 short-write shim — GENERATED per run, never committed.
 *
 * write(2) for ONE exact target path returns FEWER bytes than requested with
 * NO error — the POSIX-legal short write that a trusting caller mistakes for
 * a complete one. There is no -1 to check: only the return count tells the
 * truth, which is exactly the bug class S-004 exists to inject.
 *
 * Env: MCSHORT_PATH     exact target path to shorten
 *      MCSHORT_PROOF    injector counter path (never matched by the rule)
 *      MCSHORT_BUDGET   matching writes to shorten; <= 0 means all of them
 *      MCSHORT_FRACTION num/den of the requested bytes to land
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
static char mshort_target[4096];
static char mshort_proof[4096];
static int mshort_budget = 1;
static int mshort_hits = 0;
static size_t mshort_num = 1;
static size_t mshort_den = 2;
static int mshort_ready = 0;

static void mshort_landproof(void) {
  if (mshort_proof[0] == '\0' || real_write == NULL) return;
  int f = open(mshort_proof, O_WRONLY | O_CREAT | O_TRUNC, 0600);
  if (f < 0) return;
  char line[4300];
  int n = snprintf(line, sizeof line, "hits=%d rule=write-short:%s:%zu/%zu\n",
                   mshort_hits, mshort_target, mshort_num, mshort_den);
  if (n > 0) (void)real_write(f, line, (size_t)n);
  close(f);
}

static int mshort_fd_matches(int fd) {
  char link[64];
  char path[4096];
  snprintf(link, sizeof link, "/proc/self/fd/%d", fd);
  ssize_t n = readlink(link, path, sizeof path - 1);
  if (n <= 0) return 0;
  path[n] = '\0';
  if (mshort_proof[0] != '\0' && strcmp(path, mshort_proof) == 0) return 0;
  return strcmp(path, mshort_target) == 0;
}

ssize_t write(int fd, const void *buf, size_t n) {
  if (real_write == NULL) {
    real_write = (ssize_t (*)(int, const void *, size_t))dlsym(RTLD_NEXT, "write");
  }
  if (real_write == NULL) { errno = ENOSYS; return -1; }
  if (mshort_ready && n > 1 && (mshort_budget <= 0 || mshort_hits < mshort_budget) && mshort_fd_matches(fd)) {
    mshort_hits++;
    mshort_landproof();
    size_t part = n * mshort_num / mshort_den;
    if (part == 0) part = 1;
    if (part >= n) part = n - 1;
    ssize_t w = real_write(fd, buf, part);
    if (w > 0) return w; /* THE SHORT WRITE: fewer bytes, no error */
  }
  return real_write(fd, buf, n);
}

__attribute__((constructor)) static void mshort_init(void) {
  const char *p = getenv("MCSHORT_PATH");
  if (p == NULL || p[0] == '\0') return;
  snprintf(mshort_target, sizeof mshort_target, "%s", p);
  const char *pr = getenv("MCSHORT_PROOF");
  if (pr != NULL) snprintf(mshort_proof, sizeof mshort_proof, "%s", pr);
  const char *b = getenv("MCSHORT_BUDGET");
  if (b != NULL) mshort_budget = atoi(b);
  const char *fr = getenv("MCSHORT_FRACTION");
  if (fr != NULL) {
    unsigned long a = 1, d = 2;
    if (sscanf(fr, "%lu/%lu", &a, &d) == 2 && d > 0 && a > 0 && a < d) {
      mshort_num = (size_t)a;
      mshort_den = (size_t)d;
    }
  }
  mshort_ready = 1;
}
`
