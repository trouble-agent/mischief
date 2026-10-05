package backend_signal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// The LD_PRELOAD fault shim backend (SPEC-06, backend "shim", the flagship
// rootless primitive — probe/RESULTS.md "LD_PRELOAD fault + landed-proof |
// PROVEN end-to-end | the flagship rootless primitive and the proof
// contract").
//
// The measured proof contract (probe/RESULTS.md): the control run wrote 6
// bytes and had no counter file; with the rule the write returned -1 EIO
// and the shim WROTE A COUNTER FILE (per-pid, independent of the target —
// "this is the mechanism the whole design is built on"). That counter file
// IS the landed-proof: Prove reads it back and grades landed only when the
// shim itself recorded a hit. A target that never wrote, or a rule the
// target never triggered, produces a no_op with the counter file absent or
// empty — never a green.
//
// The v0.1 arm attaches the rule to a not-yet-started CHILD (env
// LD_PRELOAD/MCFAULT/MCFAULT_PROOF); the fault is per-process by
// construction.
//
// DELIBERATE DEVIATION from the legacy probe shim (MSF-028,
// probe/libfault-probe.c): the legacy C file (a) matched any
// /proc/<pid>/fd target that CONTAINED the pattern — bare substring, no
// anchoring — and (b) could match the proof counter file itself. The shim
// this backend arms is GENERATED (ShimSourceC) with the anchored matching
// of rule.go (exact / "prefix:*" / "*:suffix" at path boundaries), a hard
// self-exclusion of the proof path read from MCFAULT_PROOF, and a
// three-slot MCFAULT parse with budget/proof in their own env vars. The
// legacy file is never shipped to a faulted child.
type ShimArm struct {
	// Primitive is the primitive id ("S-001").
	Primitive string
	// Rule is the parsed, anchored rule.
	Rule Rule
	// Dir is the scratch directory holding the proof counters.
	Dir string

	libPath   string
	proofPath string
}

// proofLine is one line of the shim's counter file:
// "hits=<n> rule=<sys>:<pattern>:<errno>" (the probe shim's format — the
// landed-proof format the catalog's S-001 check names).
type proofLine struct {
	Hits int
	Rule string
}

var proofLineRe = regexp.MustCompile(`hits=(\d+) rule=(.+)`)

// ReadProofFile parses a counter file; (zero, nil) when absent. A present
// but unparsable counter is an error, not a zero: a counter we cannot read
// proves nothing. Exported for the selftest harness's control arm (the
// control run must be counter-less — the discrimination the landed-proof
// leans on).
func ReadProofFile(path string) (proofLine, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return proofLine{}, nil
		}
		return proofLine{}, err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return proofLine{}, nil
	}
	m := proofLineRe.FindStringSubmatch(s)
	if m == nil {
		return proofLine{}, fmt.Errorf("shim proof file %s: unparsable content %q (want hits=N rule=...)", path, s)
	}
	n, err := parseHits(m[1])
	if err != nil {
		return proofLine{}, fmt.Errorf("shim proof file %s: %w", path, err)
	}
	return proofLine{Hits: n, Rule: m[2]}, nil
}

// standaloneShimCandidates are the candidate locations of a precompiled
// shim .so (used when arming outside the selftest; the generated source is
// always compiled fresh for a child otherwise).
var standaloneShimCandidates = []string{
	"/tmp/mischief-shim/libfault-anchored.so",
}

// buildShimLib compiles ShimSourceC — the ANCHORED shim, generated here,
// never the legacy probe file — into dir and returns the .so path.
func buildShimLib(dir string) (string, error) {
	src := filepath.Join(dir, "libfault-anchored.c")
	if err := os.WriteFile(src, []byte(ShimSourceC), 0o600); err != nil {
		return "", fmt.Errorf("shim source write: %w", err)
	}
	so := filepath.Join(dir, "libfault-anchored.so")
	cmd := exec.Command("cc", "-shared", "-fPIC", "-O2", "-o", so, src, "-ldl")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("shim build failed: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return so, nil
}

// ArmShim prepares a shim fault for a child that has NOT started yet: it
// resolves the compiled shim library (built fresh from the generated
// anchored source, rootless cc) and derives the proof counter path inside
// dir. It returns the env additions the caller must attach to the child
// process (Rule.Env).
//
// AC-11: a missing compiler is a NAMED error — the run refuses with the
// reason instead of silently running the target unfaulted (a
// silently-unfaulted run is exactly the green-but-meaningless shape AC-3
// exists to catch at Prove time).
func ArmShim(primitive string, r Rule, dir string) (*ShimArm, []string, error) {
	if r.match == "" {
		return nil, nil, fmt.Errorf("shim arm: rule was not produced by ParseRule (unvalidated rules are refused)")
	}
	if r.ProofPath != "" && r.matchesUnchecked(r.ProofPath) {
		return nil, nil, fmt.Errorf(
			"shim arm: proof path %q matches the rule's own pattern — the proof channel must be self-excluded (MSF-028)",
			r.ProofPath)
	}
	lib := ""
	for _, c := range standaloneShimCandidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			lib = c
			break
		}
	}
	if lib == "" {
		var err error
		lib, err = buildShimLib(dir)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"capability_unavailable: no compiled shim library (%s) and none could be built (%v) — the shim primitive cannot arm; refusing instead of running the target unfaulted",
				strings.Join(standaloneShimCandidates, ", "), err)
		}
	}
	a := &ShimArm{
		Primitive: primitive,
		Rule:      r,
		Dir:       dir,
		libPath:   lib,
	}
	if r.ProofPath == "" {
		// the env MUST carry the derived path: the shim writes its counter
		// to MCFAULT_PROOF, and a counter-less shim cannot prove anything
		r.ProofPath = DeriveProofPath(r.canonicalText(), dir)
	}
	a.proofPath = r.ProofPath
	return a, r.Env(lib), nil
}

// ProofPath is the counter file this arm will grade its landing on.
func (a *ShimArm) ProofPath() string { return a.proofPath }

// Prove grades the landing AFTER the child has run: the shim's own counter
// file must exist with hits >= 1 carrying this rule's canonical text.
func (a *ShimArm) Prove() Outcome {
	pl, err := ReadProofFile(a.proofPath)
	if err != nil {
		return NewFailed(a.Primitive, "shim", err, nil)
	}
	if pl.Hits < 1 {
		return NewNoOp(a.Primitive, "shim",
			fmt.Sprintf("no shim counter file with hits >= 1 at %s: the target never triggered the rule — the anti-gaming arm (AC-3: a counter-less run grades no_op and FAILS)", a.proofPath), nil)
	}
	if pl.Rule != a.Rule.canonicalText() {
		return NewNoOp(a.Primitive, "shim",
			fmt.Sprintf("counter file at %s carries rule %q, not this rule %q — a stale counter is not this fault's proof",
				a.proofPath, pl.Rule, a.Rule.canonicalText()), nil)
	}
	proof := fmt.Sprintf("shim-counter: %s (hits=%d rule=%s)", a.proofPath, pl.Hits, pl.Rule)
	return NewLanded(a.Primitive, "shim", proof, &Receipt{
		Kind:   "env",
		Detail: fmt.Sprintf("LD_PRELOAD=%s MCFAULT=%s attached to child", a.libPath, a.Rule.canonicalText()),
	})
}

// ShimSourceC is the shim actually shipped to faulted children: GENERATED
// from this constant, anchored per rule.go, proof-channel self-excluded.
// Differences from the legacy probe shim (probe/libfault-probe.c, the
// MSF-028 finding):
//
//  1. matching is exact / anchored-prefix ("dir:*": the dir itself or
//     anything under it at a '/') / anchored-suffix ("*:name": the name or
//     anything ending in "/name") — never bare substring;
//  2. the proof path arrives via MCFAULT_PROOF and is NEVER matched by
//     the rule (the legacy shim could count a write to its own counter);
//  3. MCFAULT is parsed as exactly three slots with a greedy middle (the
//     legacy strtok split every colon, so an anchored pattern broke the
//     parse); budget and proof travel in MCFAULT_BUDGET / MCFAULT_PROOF;
//  4. every matching decision is recorded, and a hit beyond the budget
//     passes through unmolested.
const ShimSourceC = `/* mischief LD_PRELOAD fault shim — GENERATED, anchored (SPEC-06).
 * Rule (env): MCFAULT=sys:pattern:errno  (exactly 3 slots, greedy middle)
 * Budget:     MCFAULT_BUDGET=N           (absent = unlimited)
 * Proof:      MCFAULT_PROOF=path         (per-pid counter, self-excluded)
 * Anchoring:  pattern ends ":*"  -> prefix: the dir itself or under it
 *             pattern starts "*:"-> suffix: the name or /name
 *             otherwise          -> exact match
 * This is NOT the legacy probe shim: no bare substring, no self-matching
 * proof channel (MSF-028). */
#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <dlfcn.h>
#include <errno.h>
#include <unistd.h>
#include <fcntl.h>

static ssize_t (*real_write)(int, const void *, size_t) = NULL;
static char rule_sys[64], rule_pattern[512], rule_errno[32];
static char proof_path[512];
static int rule_budget = -1, hits = 0, loaded = 0;

static int errno_for(const char *name) {
  if (!strcmp(name, "EIO")) return EIO;
  if (!strcmp(name, "ENOSPC")) return ENOSPC;
  if (!strcmp(name, "EACCES")) return EACCES;
  if (!strcmp(name, "EDQUOT")) return EDQUOT;
  if (!strcmp(name, "ENOMEM")) return ENOMEM;
  if (!strcmp(name, "EINTR")) return EINTR;
  if (!strcmp(name, "ETIMEDOUT")) return ETIMEDOUT;
  if (!strcmp(name, "EAGAIN")) return EAGAIN;
  if (!strcmp(name, "EPIPE")) return EPIPE;
  return EIO;
}

static void landproof(void) {
  if (!proof_path[0]) return;
  int f = open(proof_path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
  if (f >= 0) {
    dprintf(f, "hits=%d rule=%s:%s:%s\n", hits, rule_sys, rule_pattern, rule_errno);
    close(f);
  }
}

/* anchored target match: exact / prefix "dir:*" / suffix "*:name",
   path-boundary at '/'; the proof path NEVER matches (self-exclusion). */
static int pattern_matches(const char *tgt) {
  size_t plen = strlen(rule_pattern);
  if (plen >= 2 && !strcmp(rule_pattern + plen - 2, ":*")) {
    size_t alen = plen - 2;
    if (strncmp(tgt, rule_pattern, alen) != 0) return 0;
    return tgt[alen] == '\0' || tgt[alen] == '/';
  }
  if (plen >= 2 && rule_pattern[0] == '*' && rule_pattern[1] == ':') {
    const char *name = rule_pattern + 2;
    size_t nlen = plen - 2;
    size_t tlen = strlen(tgt);
    if (tlen < nlen) return 0;
    if (strcmp(tgt + tlen - nlen, name) != 0) return 0;
    return tlen == nlen || tgt[tlen - nlen - 1] == '/';
  }
  return strcmp(tgt, rule_pattern) == 0;
}

static int target_fd_matches(int fd) {
  char link[64], tgt[1024];
  snprintf(link, sizeof link, "/proc/self/fd/%d", fd);
  ssize_t n = readlink(link, tgt, sizeof tgt - 1);
  if (n <= 0) return 0;
  tgt[n] = 0;
  if (proof_path[0] && strcmp(tgt, proof_path) == 0) return 0;
  return pattern_matches(tgt);
}

ssize_t write(int fd, const void *buf, size_t n) {
  if (!real_write) real_write = dlsym(RTLD_NEXT, "write");
  if (loaded && !strcmp(rule_sys, "write") && target_fd_matches(fd)) {
    if (rule_budget < 0 || hits < rule_budget) {
      hits++;
      landproof();
      errno = errno_for(rule_errno);
      return -1;
    }
  }
  return real_write(fd, buf, n);
}

__attribute__((constructor)) static void init(void) {
  if (loaded) return;
  const char *r = getenv("MCFAULT");
  if (!r) return;
  loaded = 1;
  /* exactly three slots: sys, greedy middle = pattern, errno = last */
  const char *first = strchr(r, ':');
  const char *last = strrchr(r, ':');
  if (!first || last == first) return;
  size_t syslen = (size_t)(first - r);
  size_t patlen = (size_t)(last - first - 1);
  size_t errlen = strlen(last + 1);
  if (syslen >= sizeof rule_sys || patlen >= sizeof rule_pattern ||
      errlen >= sizeof rule_errno) return;
  memcpy(rule_sys, r, syslen); rule_sys[syslen] = 0;
  memcpy(rule_pattern, first + 1, patlen); rule_pattern[patlen] = 0;
  memcpy(rule_errno, last + 1, errlen); rule_errno[errlen] = 0;
  const char *b = getenv("MCFAULT_BUDGET");
  if (b && *b) rule_budget = atoi(b);
  const char *p = getenv("MCFAULT_PROOF");
  if (p && *p) { snprintf(proof_path, sizeof proof_path, "%s", p); }
  real_write = dlsym(RTLD_NEXT, "write");
}
`

// shimCompileOnce guards concurrent buildShimLib calls in one process (two
// selftests arming at once must not race the same scratch path).
var shimCompileOnce sync.Mutex

// SelftestShim is the shim primitive's selftest: it compiles the GENERATED
// anchored shim into a scratch dir, spawns a REAL child (the probe's
// victim pattern: write to the anchored target, then to an unaffected
// path) under LD_PRELOAD, runs a control first, and proves the landing
// from the shim's counter file. Land + prove landed on THIS host.
func SelftestShim() error {
	shimCompileOnce.Lock()
	defer shimCompileOnce.Unlock()

	dir, err := os.MkdirTemp("", "mischief-shim-selftest-*")
	if err != nil {
		return fmt.Errorf("selftest shim: scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	r, err := ParseRule("write:*:selftest-target.tmp:EIO")
	if err != nil {
		return fmt.Errorf("selftest shim: rule: %w", err)
	}
	arm, env, err := ArmShim("S-001", r, dir)
	if err != nil {
		return fmt.Errorf("selftest shim: arm: %w", err)
	}

	// The victim: a real child that writes to the anchored target, then to
	// an unaffected path. Compiled from the probe's victim.c pattern (the
	// same shape probe/RESULTS.md measured: control 6 bytes, faulted -1
	// EIO).
	target := filepath.Join(dir, "selftest-target.tmp")
	other := filepath.Join(dir, "unaffected.tmp")
	victimSrc := filepath.Join(dir, "victim.c")
	if err := os.WriteFile(victimSrc, BuildVictim(target, other), 0o600); err != nil {
		return fmt.Errorf("selftest shim: victim source: %w", err)
	}
	victim := filepath.Join(dir, "victim")
	cc := exec.Command("cc", "-O2", "-o", victim, victimSrc)
	if b, err := cc.CombinedOutput(); err != nil {
		return fmt.Errorf("selftest shim: victim build failed: %w: %s", err, strings.TrimSpace(string(b)))
	}

	// control run (no fault): the target file must hold its 6 bytes and no
	// counter may exist — the control pins the discrimination.
	if b, err := exec.Command(victim, target).CombinedOutput(); err != nil {
		return fmt.Errorf("selftest shim: control run failed: %w: %s", err, strings.TrimSpace(string(b)))
	}
	cb, err := os.ReadFile(target)
	if err != nil || len(cb) == 0 {
		return fmt.Errorf("selftest shim: control wrote no bytes (got %v, err %v)", cb, err)
	}
	if pl, err := ReadProofFile(arm.ProofPath()); err != nil || pl.Hits != 0 {
		return fmt.Errorf("selftest shim: control produced a proof counter (hits=%d, err %v) — the control must be counter-less", pl.Hits, err)
	}

	// faulted run: same child, now with the shim env. The write to the
	// anchored target must fail EIO; the unaffected path must still write.
	faulted := exec.Command(victim, target)
	faulted.Env = append(os.Environ(), env...)
	if out, err := faulted.CombinedOutput(); err != nil {
		return fmt.Errorf("selftest shim: faulted run failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	res := arm.Prove()
	if !res.Landed() {
		return fmt.Errorf("selftest shim: land did not prove: %s", res)
	}
	ob, err := os.ReadFile(other)
	if err != nil || len(ob) == 0 {
		return fmt.Errorf("selftest shim: unaffected path did not survive the fault (got %v, err %v)", ob, err)
	}
	return nil
}

// DetachShim is the recorded inverse of a shim fault (S-001): remove the
// fault env so a child started WITHOUT it runs the un-faulted write path,
// then PROVE the undo measured — a fresh control child writes through and
// the file holds the written bytes (the descriptor's verify: "the next
// write to the path succeeds"; measured by the write's own success, not
// asserted). Disarm is environmental by construction: the armed rule lived
// in the previous child's env, so a child without the env cannot carry it.
func DetachShim(primitive string, victimBin, target string) Outcome {
	after := exec.Command(victimBin, target)
	b, err := after.CombinedOutput()
	if err != nil {
		return NewFailed(primitive, "shim", fmt.Errorf("inverse: post-detach write run failed: %w: %s", err, strings.TrimSpace(string(b))), nil)
	}
	cb, err := os.ReadFile(target)
	if err != nil || len(cb) == 0 {
		return NewFailed(primitive, "shim", fmt.Errorf("inverse: issued but not proven: target %q after detach holds %v (err %v) — the next write did not succeed", target, cb, err), nil)
	}
	return NewLanded(primitive, "shim",
		fmt.Sprintf("shim-detach: next write to %s succeeded (%d bytes on disk, no fault env)", target, len(cb)), nil)
}

func victimC(dir string) string {
	_ = dir
	return `#define _GNU_SOURCE
#include <stdio.h>
#include <string.h>
#include <errno.h>
#include <fcntl.h>
#include <unistd.h>

/* the selftest victim: writes one line to argv[1] (the anchored target)
   and one line to a second path; mirrors probe/victim.c */
int main(int argc, char **argv) {
  const char *path = argc > 1 ? argv[1] : "TARGET";
  const char *other = "OTHER";
  int f = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
  ssize_t n = write(f, "hello\n", 6);
  printf("write(%s) -> %zd errno=%s\n", path, n, n < 0 ? strerror(errno) : "-");
  close(f);
  int d = open(other, O_WRONLY | O_CREAT | O_TRUNC, 0600);
  ssize_t m = write(d, "hello\n", 6);
  printf("write(%s) -> %zd errno=%s\n", other, m, m < 0 ? strerror(errno) : "-");
  close(d);
  return 0;
}
` // TARGET and OTHER are byte-replaced below by BuildVictim
}

// BuildVictim renders the selftest victim source with concrete paths.
func BuildVictim(target, other string) []byte {
	s := victimC("")
	s = strings.ReplaceAll(s, "TARGET", target)
	s = strings.ReplaceAll(s, "OTHER", other)
	return []byte(s)
}
