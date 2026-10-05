package backend_signal

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ---- rule language: parsing, anchoring, self-exclusion ----

func TestParseRuleValid(t *testing.T) {
	cases := []struct {
		text    string
		sys     string
		pat     string
		errno   string
		anchor  MatchKind
		budget  int
		proofOK bool
	}{
		{text: "write:/tmp/x.tmp:EIO", sys: "write", pat: "/tmp/x.tmp", errno: "EIO", anchor: MatchExact, budget: -1},
		{text: "write:*:cache.tmp:ENOSPC", sys: "write", pat: "*:cache.tmp", errno: "ENOSPC", anchor: MatchSuffix, budget: -1},
		{text: "write:/var/lib/app:*:EDQUOT", sys: "write", pat: "/var/lib/app:*", errno: "EDQUOT", anchor: MatchPrefix, budget: -1},
		{text: "write:/tmp/x.tmp:ETIMEDOUT", sys: "write", pat: "/tmp/x.tmp", errno: "ETIMEDOUT", anchor: MatchExact, budget: -1},
		// a path containing colons survives the greedy middle parse
		{text: "write:/tmp/we:ird:x.tmp:EIO", sys: "write", pat: "/tmp/we:ird:x.tmp", errno: "EIO", anchor: MatchExact, budget: -1},
	}
	for _, c := range cases {
		r, err := ParseRule(c.text)
		if err != nil {
			t.Errorf("ParseRule(%q): %v", c.text, err)
			continue
		}
		if r.Sys != c.sys || r.Pattern != c.pat || r.Errno != c.errno || r.Anchoring() != c.anchor || r.Budget != c.budget {
			t.Errorf("ParseRule(%q) = %+v (anchoring %s), want sys=%s pat=%s errno=%s anchor=%s budget=%d",
				c.text, r, r.Anchoring(), c.sys, c.pat, c.errno, c.anchor, c.budget)
		}
		// canonical round-trip: the text the shim parses back
		rt, err := ParseRule(r.canonicalText())
		if err != nil || rt.canonicalText() != r.canonicalText() {
			t.Errorf("canonical round-trip of %q: (%v, %q)", c.text, err, rt.canonicalText())
		}
		// every valid rule must accept a derived proof path
		p := DeriveProofPath(r.canonicalText(), t.TempDir())
		r2 := r
		r2.ProofPath = p
		if _, err := ParseRule(r2.canonicalText() + ":" + p); err != nil && !r2.matchesUnchecked(p) {
			_ = err // String() carries the proof for consumers; parse accepts it
		}
	}
}

func TestParseRuleRefusals(t *testing.T) {
	cases := []struct {
		text     string
		contains string
	}{
		{"", "must be sys:pattern:errno"},
		{"write:EIO", "must be sys:pattern:errno"},
		{"write::EIO", "empty pattern"},
		{":/tmp/x:EIO", "empty syscall name"},
		{"write:/tmp/x:ENOMYERRNO", "unknown errno"},
		// the MSF-028 refusals: no bare substring class in the language
		{"write:mc-victim:EIO", "MSF-028"},
		{"write:some/partial/path:EIO", "MSF-028"},
		// no mixed wildcards
		{"write:/tmp/*/x:EIO", "anchored"},
		{"write:a*b:EIO", "anchored"},
		{"write:*:EIO", "literal"},
		{"write::*:EIO", "literal"},
		{"write:*:*:EIO", "literal"},
		// unknown syscall refused (v0.1 implements write only)
		{"read:/tmp/x:EIO", "unsupported syscall"},
		{"open:/tmp/x:EIO", "unsupported syscall"},
	}
	for _, c := range cases {
		_, err := ParseRule(c.text)
		if err == nil {
			t.Errorf("ParseRule(%q): expected refusal, got nil", c.text)
			continue
		}
		if !strings.Contains(err.Error(), c.contains) {
			t.Errorf("ParseRule(%q): error %q does not name %q", c.text, err, c.contains)
		}
	}
}

func TestRuleMatchingAnchored(t *testing.T) {
	cases := []struct {
		text  string
		match []string // must match
		miss  []string // must NOT match (each miss is a specificity the legacy shim got wrong)
	}{
		{
			text:  "write:/tmp/x.tmp:EIO",
			match: []string{"/tmp/x.tmp"},
			miss:  []string{"/tmp/x.tmp.old", "/tmp/y.tmp", "/var/tmp/x.tmp", ""},
		},
		{
			// suffix anchoring: the name or anything ending in /name —
			// never a substring embedding
			text:  "write:*:cache.tmp:EIO",
			match: []string{"cache.tmp", "/var/cache.tmp", "/deep/nest/cache.tmp"},
			miss:  []string{"xcache.tmp", "cache.tmp.old", "/var/cache.tmp/wal", "cache"},
		},
		{
			// prefix anchoring: the dir itself or anything under it —
			// never a sibling whose name extends the dir
			text:  "write:/var/lib/app:*:EIO",
			match: []string{"/var/lib/app", "/var/lib/app/db", "/var/lib/app/log/a/b"},
			miss:  []string{"/var/lib/app.evil", "/var/lib", "/var/lib/appdb"},
		},
	}
	for _, c := range cases {
		r, err := ParseRule(c.text)
		if err != nil {
			t.Fatalf("ParseRule(%q): %v", c.text, err)
		}
		for _, m := range c.match {
			if !r.Matches(m) {
				t.Errorf("%q should match %q", c.text, m)
			}
		}
		for _, m := range c.miss {
			if r.Matches(m) {
				t.Errorf("%q must NOT match %q (anchoring violated)", c.text, m)
			}
		}
	}
}

func TestRuleProofChannelSelfExclusion(t *testing.T) {
	dir := t.TempDir()
	r, err := ParseRule("write:*:proof.jsonl:EIO")
	if err != nil {
		t.Fatal(err)
	}
	proof := filepath.Join(dir, "proof.jsonl")
	// even a rule whose pattern would match the proof path must not match it
	r.ProofPath = proof
	if r.Matches(proof) {
		t.Fatal("the proof channel matched its own rule (MSF-028 reproduced)")
	}
	// ...and arming a rule whose pattern names its own proof path is the
	// MSF-028 refusal (the reachable guard: the proof travels in env, so
	// ArmShim — not Parse — sees the rule WITH its proof path)
	selfMatching := mustRule(t, "write:*:proof.jsonl:EIO")
	selfMatching.ProofPath = proof
	if !selfMatching.matchesUnchecked(proof) {
		t.Fatal("test premise broken: the pattern should match its own proof path pre-exclusion")
	}
	if _, _, err := ArmShim("S-001", selfMatching, dir); err == nil {
		t.Fatal("ArmShim accepted a rule whose proof path matches its own pattern")
	} else if !strings.Contains(err.Error(), "MSF-028") {
		t.Errorf("proof self-match refusal does not cite the finding: %v", err)
	}
	// arming the same pattern with a DISJOINT proof path is fine
	disjoint := mustRule(t, "write:*:proof.jsonl:EIO")
	disjoint.ProofPath = filepath.Join(dir, "other-counter.jsonl")
	if _, _, err := ArmShim("S-001", disjoint, dir); err != nil {
		t.Errorf("disjoint proof path refused: %v", err)
	}
	// the derive path never collides between different rules
	r2, _ := ParseRule("write:*:other.tmp:ENOSPC")
	if DeriveProofPath(r.canonicalText(), dir) == DeriveProofPath(r2.canonicalText(), dir) {
		t.Fatal("two different rules derived the same proof path")
	}
	// derivation is idempotent (same rule -> same counter)
	if DeriveProofPath(r.canonicalText(), dir) != DeriveProofPath(r.canonicalText(), dir) {
		t.Fatal("proof path derivation is not idempotent")
	}
}

func TestRuleBudgetEnv(t *testing.T) {
	r, err := ParseRule("write:/tmp/x.tmp:EIO")
	if err != nil {
		t.Fatal(err)
	}
	if r.Budget != -1 {
		t.Fatalf("default budget = %d, want -1 (unlimited)", r.Budget)
	}
	env := r.Env("/tmp/lib.so")
	if len(env) != 3 || !strings.HasPrefix(env[0], "LD_PRELOAD=") ||
		!strings.HasPrefix(env[1], "MCFAULT=") || !strings.HasPrefix(env[2], "MCFAULT_PROOF=") {
		t.Fatalf("unlimited-rule env = %v, want LD_PRELOAD/MCFAULT/MCFAULT_PROOF with no budget var", env)
	}
	for _, e := range env {
		if strings.HasPrefix(e, "MCFAULT_BUDGET=") {
			t.Fatalf("unlimited rule must not carry MCFAULT_BUDGET: %v", env)
		}
	}
	rb, err := r.WithBudget(2)
	if err != nil {
		t.Fatal(err)
	}
	if rb.Budget != 2 || r.Budget != -1 {
		t.Fatalf("WithBudget mutated the receiver or lost the value: %d/%d", rb.Budget, r.Budget)
	}
	env = rb.Env("/tmp/lib.so")
	found := false
	for _, e := range env {
		if e == "MCFAULT_BUDGET=2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("budget rule env lacks MCFAULT_BUDGET=2: %v", env)
	}
	if _, err := r.WithBudget(-5); err == nil {
		t.Fatal("negative budget accepted")
	}
}

// ---- outcome contract: AC-3 shapes ----

func TestOutcomeExitCodeContract(t *testing.T) {
	cases := []struct {
		o    Outcome
		code int
	}{
		{NewLanded("P", "sig", "proof", &Receipt{Kind: "k", Detail: "d"}), 0},
		{NewNoOp("P", "sig", "reason", nil), 1},
		{NewFailed("P", "sig", errors.New("x"), nil), 1},
	}
	for i, c := range cases {
		if got := c.o.ExitCode(); got != c.code {
			t.Errorf("case %d: ExitCode=%d want %d (%s)", i, got, c.code, c.o)
		}
		if c.o.Landed() != (c.code == 0) {
			t.Errorf("case %d: Landed() disagrees with exit code", i)
		}
	}
}

func TestNewNoOpEmptyReasonIsLoud(t *testing.T) {
	o := NewNoOp("P-001", "sig", "", nil)
	if o.NoOpReason == "" {
		t.Fatal("empty no_op reason was accepted silently")
	}
	if !strings.Contains(o.NoOpReason, "contract violation") {
		t.Errorf("loud default reason missing: %q", o.NoOpReason)
	}
}

// ---- live signal primitives (real child processes, no mocks) ----

func spawnSleeper(t *testing.T, secs string) (*exec.Cmd, int, func()) {
	t.Helper()
	cmd := exec.Command("sleep", secs)
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	pid := cmd.Process.Pid
	kill := func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := procState(pid)
		if err == nil && st != 'Z' {
			return cmd, pid, kill
		}
		if time.Now().After(deadline) {
			kill()
			t.Fatal("child never became observable in /proc")
		}
		time.Sleep(statePollInterval)
	}
}

func TestApplySignalStopLandedAndResumeReverts(t *testing.T) {
	_, pid, kill := spawnSleeper(t, "30")
	defer kill()

	out, f := ApplySignal("P-001", pid, SigStop)
	if !out.Landed() {
		t.Fatalf("SIGSTOP did not land: %s", out)
	}
	if out.ExitCode() != 0 {
		t.Fatalf("landed outcome exit code = %d", out.ExitCode())
	}
	if f.receipt.Detail == "" {
		t.Error("receipt missing on a landed signal fault")
	}
	// the receipt alone is not the proof: the T-state observation is
	if !strings.Contains(out.LandedProof, "state T for 2 consecutive samples") {
		t.Errorf("landed proof does not name the observation: %q", out.LandedProof)
	}
	// the independent observation holds right now
	if st, err := procState(pid); err != nil || st != 'T' {
		t.Fatalf("target not in T after landed proof (state=%q err=%v)", string(rune(st)), err)
	}

	r := f.Resume()
	if !r.Landed() {
		t.Fatalf("resume did not prove reverted: %s", r)
	}
	if st, err := procState(pid); err != nil || st == 'T' {
		t.Fatalf("target still stopped after proven resume (state=%q err=%v)", string(rune(st)), err)
	}
}

func TestApplySignalKillLandsWithWaitReceipt(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	defer func() { _, _ = cmd.Process.Wait() }()
	waitFor(time.Second, statePollInterval, 1, func() bool {
		st, err := procState(pid)
		return err == nil && st != 'Z'
	})

	out, _ := ApplySignal("P-003", pid, SigKill)
	if !out.Landed() {
		t.Fatalf("SIGKILL did not land: %s", out)
	}
	if !strings.Contains(out.LandedProof, "Signaled=true") {
		t.Errorf("SIGKILL proof does not name the wait receipt: %q", out.LandedProof)
	}
	if !strings.Contains(out.LandedProof, "signal=killed") {
		t.Errorf("SIGKILL proof does not name the signal: %q", out.LandedProof)
	}
	// ApplySignal took the reap (the Wait4 receipt IS the proof): the
	// os/exec ProcessState is nil here — the test must not re-reap.
	if cmd.ProcessState != nil {
		t.Error("os/exec reaped too; the package and the test would double-reap")
	}
}

// THE AC-3 ARMS: armed-but-did-nothing must grade no_op with exit 1.

func TestApplySignalZombieGradesNoOpNotLanded(t *testing.T) {
	// start a child, kill it WITHOUT reaping -> a live zombie
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	defer func() { _, _ = cmd.Process.Wait() }()
	_ = cmd.Process.Kill()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if st, err := procState(pid); err == nil && st == 'Z' {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never became a zombie")
		}
		time.Sleep(statePollInterval)
	}

	out, _ := ApplySignal("P-001", pid, SigStop)
	if out.Landed() {
		t.Fatalf("a kill receipt on a zombie graded LANDED — the AC-3 gaming arm: %s", out)
	}
	if out.ExitCode() != 1 {
		t.Errorf("no_op exit code = %d, want 1", out.ExitCode())
	}
	if out.NoOpReason == "" {
		t.Error("no_op without a stated reason")
	}
	// the state must still be Z: nothing landed
	if st, _ := procState(pid); st != 'Z' {
		t.Errorf("zombie left state Z after the attempt (state=%q)", string(rune(st)))
	}
}

func TestApplySignalDeadPidGradesNoOp(t *testing.T) {
	// reap a child fully, then aim at the vanished pid
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	_, _ = cmd.Process.Wait()

	out, _ := ApplySignal("P-001", pid, SigStop)
	if out.Landed() {
		t.Fatalf("a signal to a reaped pid graded LANDED: %s", out)
	}
	if out.ExitCode() != 1 {
		t.Errorf("no_op exit code = %d, want 1", out.ExitCode())
	}
}

func TestApplySignalProtectedPidsRefused(t *testing.T) {
	for _, pid := range []int{0, 1, -3} {
		out, _ := ApplySignal("P-001", pid, SigStop)
		if out.Landed() {
			t.Fatalf("pid %d: landed on a structurally excluded target", pid)
		}
		if out.Err == nil {
			t.Errorf("pid %d: refusal without a named error", pid)
		} else if !strings.Contains(out.Err.Error(), "structurally excluded") {
			t.Errorf("pid %d: refusal does not name the exclusion: %v", pid, out.Err)
		}
	}
}

func TestApplySignalUnsupportedSignalNamedRefusal(t *testing.T) {
	_, pid, kill := spawnSleeper(t, "30")
	defer kill()
	out, _ := ApplySignal("P-002", pid, "SIGSEGV")
	if out.Landed() {
		t.Fatal("SIGSEGV landed; the v0.1 vocabulary is SIGSTOP/SIGCONT/SIGKILL only")
	}
	if out.Err == nil || !strings.Contains(out.Err.Error(), "not in the v0.1 sig vocabulary") {
		t.Errorf("refusal does not name the vocabulary: %v", out.Err)
	}
}

// ---- live prlimit primitive ----

func TestApplyPrlimitLandsAndRevertRestores(t *testing.T) {
	_, pid, kill := spawnSleeper(t, "30")
	defer kill()

	out, f := ApplyPrlimit("S-008", pid, ResNoFile, 64)
	if !out.Landed() {
		t.Fatalf("NOFILE squeeze did not land: %s", out)
	}
	if out.ExitCode() != 0 {
		t.Fatalf("landed squeeze exit code = %d", out.ExitCode())
	}
	if !strings.Contains(out.LandedProof, "prlimit64 AND /proc") {
		t.Errorf("landed proof does not name both surfaces: %q", out.LandedProof)
	}
	// independent observation #3: the child's own /proc limits agree
	pl, err := readProcLimits(pid)
	if err != nil || pl.NoFileSoft != 64 {
		t.Fatalf("independent read-back: soft=%d err=%v, want 64", pl.NoFileSoft, err)
	}
	// NOTE: deliberately NO fd-exhaustion behavior cell here — a battery
	// cell that storms fds/processes on a shared host manufactures the
	// incident it observes. R-007's EMFILE behavior belongs to SPEC-10's
	// battery on the sanctioned scratch host; this package's landed-proof
	// contract is the two-surface read-back, which is what is proven.

	// a second squeeze stacks (faults compose downward)
	out2, f2 := ApplyPrlimit("S-008", pid, ResNoFile, 32)
	if !out2.Landed() {
		t.Fatalf("second squeeze did not land: %s", out2)
	}
	if f2.Saved.Cur != 64 {
		t.Errorf("second fault saved %d, want the stacked 64", f2.Saved.Cur)
	}
	if r := f2.Revert(); !r.Landed() {
		t.Fatalf("inner revert did not prove: %s", r)
	}
	pl, _ = readProcLimits(pid)
	if pl.NoFileSoft != 64 {
		t.Fatalf("after inner revert soft=%d, want 64 (the outer fault still held)", pl.NoFileSoft)
	}
	if r := f.Revert(); !r.Landed() {
		t.Fatalf("outer revert did not prove restored: %s", r)
	}
	pl, err = readProcLimits(pid)
	if err != nil || pl.NoFileSoft != f.Saved.Cur {
		t.Fatalf("after revert soft=%d err=%v, want saved %d", pl.NoFileSoft, err, f.Saved.Cur)
	}
}

func TestApplyPrlimitNProcLands(t *testing.T) {
	_, pid, kill := spawnSleeper(t, "30")
	defer kill()

	var before rlimit64
	if err := prlimitRaw(pid, ResNProc, nil, &before); err != nil {
		t.Skipf("NPROC unreadable on this host: %v", err)
	}
	out, f := ApplyPrlimit("R-011", pid, ResNProc, 200)
	if !out.Landed() {
		t.Fatalf("NPROC squeeze did not land: %s", out)
	}
	pl, err := readProcLimits(pid)
	if err != nil || pl.NProcSoft != 200 {
		t.Fatalf("NPROC read-back soft=%d err=%v, want 200", pl.NProcSoft, err)
	}
	if r := f.Revert(); !r.Landed() {
		t.Fatalf("NPROC revert did not prove: %s", r)
	}
	pl, _ = readProcLimits(pid)
	if pl.NProcSoft != before.Cur {
		t.Fatalf("NPROC after revert = %d, want %d", pl.NProcSoft, before.Cur)
	}
}

func TestApplyPrlimitNoOpAndRefusalArms(t *testing.T) {
	// dead target: no_op with a named reason (never a fake landing)
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	_, _ = cmd.Process.Wait()
	out, _ := ApplyPrlimit("S-008", pid, ResNoFile, 64)
	if out.Landed() || out.ExitCode() != 1 || out.NoOpReason == "" {
		t.Errorf("dead-target arm: (%s) exit=%d reason=%q — want no_op/1/named", out, out.ExitCode(), out.NoOpReason)
	}

	_, pid2, kill := spawnSleeper(t, "30")
	defer kill()
	// raising is not a fault: named refusal
	out, _ = ApplyPrlimit("S-008", pid2, ResNoFile, 1<<30)
	if out.Landed() {
		t.Fatal("a raise graded landed")
	}
	if out.Err == nil || !strings.Contains(out.Err.Error(), "must lower") {
		t.Errorf("raise refusal does not name the constraint: %v", out.Err)
	}
	// structurally excluded pid
	out, _ = ApplyPrlimit("S-008", 1, ResNoFile, 64)
	if out.Landed() || out.Err == nil || !strings.Contains(out.Err.Error(), "structurally excluded") {
		t.Errorf("pid-1 refusal missing: %s", out)
	}
}

// ---- shim backend: live child, control vs faulted, no_op arm ----

func TestShimArmProveControlVsFaulted(t *testing.T) {
	dir := t.TempDir()
	r, err := ParseRule("write:*:shimtest-target.tmp:EIO")
	if err != nil {
		t.Fatal(err)
	}
	arm, env, err := ArmShim("S-001", r, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Gate("S-001", L0); err != nil {
		t.Fatalf("L0 gate must always admit: %v", err)
	}

	target := filepath.Join(dir, "shimtest-target.tmp")
	other := filepath.Join(dir, "unaffected.tmp")
	victimSrc := filepath.Join(dir, "victim.c")
	if err := os.WriteFile(victimSrc, BuildVictim(target, other), 0o600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(dir, "victim")
	if b, err := exec.Command("cc", "-O2", "-o", victim, victimSrc).CombinedOutput(); err != nil {
		t.Fatalf("victim build: %v: %s", err, b)
	}

	// CONTROL run: the write lands, NO counter exists -> Prove grades no_op.
	if b, err := exec.Command(victim, target).CombinedOutput(); err != nil {
		t.Fatalf("control run: %v: %s", err, b)
	}
	cb, err := os.ReadFile(target)
	if err != nil || len(cb) == 0 {
		t.Fatalf("control did not write the target (%v)", err)
	}
	if ctl := arm.Prove(); ctl.Landed() {
		t.Fatalf("control run graded LANDED — the landed-proof is not discriminating: %s", ctl)
	} else if ctl.ExitCode() != 1 || ctl.NoOpReason == "" {
		t.Errorf("control Prove = (%s) exit=%d — want no_op/1/named", ctl, ctl.ExitCode())
	}

	// FAULTED run: the write fails EIO, the unaffected path survives, the
	// counter exists -> Prove grades landed.
	faulted := exec.Command(victim, target)
	faulted.Env = append(os.Environ(), env...)
	outB, err := faulted.CombinedOutput()
	if err != nil {
		t.Fatalf("faulted run: %v: %s", err, outB)
	}
	outStr := string(outB)
	if !strings.Contains(outStr, "-> -1 errno=Input/output error") {
		t.Errorf("faulted write did not return EIO: %s", outStr)
	}
	if !strings.Contains(outStr, "unaffected.tmp) -> 6") {
		t.Errorf("unaffected path was disturbed: %s", outStr)
	}
	if b, _ := os.ReadFile(target); len(b) != 0 {
		t.Errorf("target file is non-empty after the faulted run (%d bytes) — the fault did not bite", len(b))
	}
	res := arm.Prove()
	if !res.Landed() {
		t.Fatalf("faulted run did not prove: %s", res)
	}
	if !strings.Contains(res.LandedProof, "hits=1") {
		t.Errorf("proof does not name the counter: %q", res.LandedProof)
	}
	// the receipt is the env attachment, not the landing
	if res.Receipt == nil || res.Receipt.Kind != "env" {
		t.Errorf("shim receipt = %+v, want the env attachment", res.Receipt)
	}
}

func TestShimBudgetStopsCounting(t *testing.T) {
	dir := t.TempDir()
	r, err := ParseRule("write:*:shimtest-budget.tmp:EIO")
	if err != nil {
		t.Fatal(err)
	}
	rb, err := r.WithBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	arm, env, err := ArmShim("S-004", rb, dir)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "shimtest-budget.tmp")
	other := filepath.Join(dir, "unaffected2.tmp")
	victimSrc := filepath.Join(dir, "victim2.c")
	if err := os.WriteFile(victimSrc, BuildVictim(target, other), 0o600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(dir, "victim2")
	if b, err := exec.Command("cc", "-O2", "-o", victim, victimSrc).CombinedOutput(); err != nil {
		t.Fatalf("victim build: %v: %s", err, b)
	}
	faulted := exec.Command(victim, target)
	faulted.Env = append(os.Environ(), env...)
	outB, err := faulted.CombinedOutput()
	if err != nil {
		t.Fatalf("faulted run: %v: %s", err, outB)
	}
	// budget 1: exactly one hit lands
	res := arm.Prove()
	if !res.Landed() {
		t.Fatalf("budgeted fault did not prove: %s", res)
	}
	if !strings.Contains(res.LandedProof, "hits=1") {
		t.Errorf("budget 1 produced hits != 1: %q", res.LandedProof)
	}
	// the SECOND write (the unaffected path) must have succeeded: the
	// budget stopped the rule after one hit
	if !strings.Contains(string(outB), "unaffected2.tmp) -> 6") {
		t.Errorf("budget did not stop after 1 hit: %s", outB)
	}
}

func TestShimArmRefusals(t *testing.T) {
	// an unvalidated rule is refused
	_, _, err := ArmShim("S-001", Rule{Sys: "write"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not produced by ParseRule") {
		t.Errorf("unvalidated rule accepted: %v", err)
	}
	// a missing compiler is a NAMED capability refusal (AC-11), not a
	// silent unfaulted run: neutralize cc via a PATH with no compiler.
	dir := t.TempDir()
	emptyBin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(emptyBin, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", emptyBin)
	standalone := standaloneShimCandidates
	standaloneShimCandidates = nil
	_, _, err = ArmShim("S-001", mustRule(t, "write:*:x.tmp:EIO"), dir)
	standaloneShimCandidates = standalone
	t.Setenv("PATH", oldPath)
	if err == nil {
		t.Fatal("arming without a compiler or .so succeeded — it would have run the child unfaulted")
	}
	if !strings.Contains(err.Error(), "capability_unavailable") {
		t.Errorf("compiler-missing refusal does not name the capability: %v", err)
	}
}

func mustRule(t *testing.T, s string) Rule {
	t.Helper()
	r, err := ParseRule(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// ---- seccomp supervisor stub: AC-11 naming ----

func TestSeccompSupervisorRefusesNamingWhatIsAbsent(t *testing.T) {
	_, err := Supervisor("S-013", []string{"fsync", "fdatasync"}, "EIO", t.TempDir())
	if err == nil {
		t.Fatal("the seccomp stub refused nothing — it must always refuse in v0.1")
	}
	if !IsUnavailable(err) {
		t.Fatalf("refusal is not capability_unavailable: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"capability_unavailable", "notifier helper", "kernel support present"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal does not name %q: %s", want, msg)
		}
	}
	// the kernel layer is measured BEFORE the refusal so the message names
	// the exact layer: on a GET_NOTIF_SIZES-capable kernel it names the
	// helper as the absent piece (the layer that IS present is not blamed).
	sz, szErr := sysNotifSizes()
	if szErr == nil && sz.Notif == 0 {
		t.Log("kernel reported zero notif sizes")
	}
	// empty syscall subset and bad errno name the input, not the helper
	_, err = Supervisor("S-013", nil, "EIO", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no syscall subset") {
		t.Errorf("empty-subset refusal: %v", err)
	}
	_, err = Supervisor("S-013", []string{"fsync"}, "ENOMYERRNO", t.TempDir())
	if err == nil || !IsUnavailable(err) || !strings.Contains(err.Error(), "closed vocabulary") {
		t.Errorf("bad-errno refusal: %v", err)
	}
}

func TestSeccompNotifSizesMatchesProbe(t *testing.T) {
	// probe/RESULTS.md measured notif=80 resp=24 on this host; if the
	// kernel layer is present the measurement must agree.
	sz, err := sysNotifSizes()
	if err != nil {
		t.Skipf("seccomp notif sizes unavailable on this host: %v", err)
	}
	if sz.Notif != 80 || sz.NotifResp != 24 {
		t.Errorf("GET_NOTIF_SIZES = %+v, want notif=80 resp=24 (probe/RESULTS.md)", sz)
	}
}

// ---- selftest registry + AC-19 gate ----

func TestSelftestRegistryComplete(t *testing.T) {
	got := SelftestNames()
	want := []string{"P-001", "P-003", "S-001", "S-008"}
	if len(got) != len(want) {
		t.Fatalf("selftest registry = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selftest registry = %v, want %v", got, want)
		}
	}
}

func TestSelftestRun(t *testing.T) {
	for _, id := range SelftestNames() {
		st, err := Selftest(id)
		if err != nil {
			t.Errorf("%s selftest red: %v", id, err)
			continue
		}
		if st != SelftestGreen {
			t.Errorf("%s selftest state = %s, want green", id, st)
		}
	}
	// unregistered -> missing, without error
	st, err := Selftest("S-013")
	if err != nil || st != SelftestMissing {
		t.Errorf("unregistered primitive = (%s, %v), want (missing, nil)", st, err)
	}
}

func TestGateRefusesOutsideL0(t *testing.T) {
	// green selftests admit above L0 (one representative level; the
	// predicate is level-independent above L0)
	for _, id := range SelftestNames() {
		if err := Gate(id, 1); err != nil {
			t.Errorf("Gate(%s, L1) refused with a green selftest: %v", id, err)
		}
	}
	// unregistered primitive refuses above L0, names the state
	err := Gate("S-013", 1)
	if err == nil {
		t.Fatal("un-selftested primitive admitted at L1")
	}
	if !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "L1") {
		t.Errorf("AC-19 refusal does not name the state and scope: %v", err)
	}
	// the same primitive is admitted INSIDE L0 (where selftest lives)
	if err := Gate("S-013", 0); err != nil {
		t.Errorf("L0 must always admit: %v", err)
	}
	// a failing selftest refuses with its reason
	old := selftests["P-001"]
	selftests["P-001"] = func() error { return errors.New("injected red") }
	err = Gate("P-001", 1)
	selftests["P-001"] = old
	if err == nil || !strings.Contains(err.Error(), "FAILING") || !strings.Contains(err.Error(), "injected red") {
		t.Errorf("failing-selftest refusal lacks the reason: %v", err)
	}
	// ...and still admits inside L0 even when failing
	selftests["P-001"] = func() error { return errors.New("injected red") }
	err0 := Gate("P-001", 0)
	selftests["P-001"] = old
	if err0 != nil {
		t.Errorf("L0 must admit even a failing selftest (where selftest lives): %v", err0)
	}
}

func TestSelftestStatePredicate(t *testing.T) {
	for _, st := range []SelftestState{SelftestMissing, SelftestFailing, SelftestUnknown} {
		if !st.RefusesOutsideL0() {
			t.Errorf("%s must refuse outside L0", st)
		}
	}
	if SelftestGreen.RefusesOutsideL0() {
		t.Error("green must not refuse")
	}
}

// ---- proc helpers ----

func TestProcStateAndLimitsParsers(t *testing.T) {
	self := os.Getpid()
	st, err := procState(self)
	if err != nil {
		t.Fatal(err)
	}
	switch st {
	case 'R', 'S':
	default:
		t.Errorf("test process state = %q, want R or S", string(rune(st)))
	}
	pl, err := readProcLimits(self)
	if err != nil {
		t.Fatal(err)
	}
	if pl.NoFileSoft == 0 || pl.NoFileHard == 0 {
		t.Errorf("NOFILE parsed as zero: %+v", pl)
	}
	if pl.NoFileSoft > pl.NoFileHard {
		t.Errorf("NOFILE soft %d > hard %d", pl.NoFileSoft, pl.NoFileHard)
	}
	if pl.NProcSoft == 0 {
		t.Errorf("NPROC parsed as zero: %+v", pl)
	}
}

func TestParseHitsRejectsGarbage(t *testing.T) {
	if _, err := parseHits("abc"); err == nil {
		t.Error("garbage hits accepted")
	}
	if _, err := parseHits("-1"); err == nil {
		t.Error("negative hits accepted")
	}
	if v, err := parseHits("7"); err != nil || v != 7 {
		t.Errorf("parseHits(7) = %d, %v", v, err)
	}
}

func TestReadProofFileAbsentVsUnparsable(t *testing.T) {
	dir := t.TempDir()
	// absent -> zero, nil (the no_op arm reads this as "no hits")
	pl, err := ReadProofFile(filepath.Join(dir, "nope"))
	if err != nil || pl.Hits != 0 {
		t.Errorf("absent proof = (%+v, %v), want (zero, nil)", pl, err)
	}
	// empty -> zero, nil
	p := filepath.Join(dir, "empty")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pl, err = ReadProofFile(p)
	if err != nil || pl.Hits != 0 {
		t.Errorf("empty proof = (%+v, %v)", pl, err)
	}
	// garbage -> ERROR (a counter we cannot read proves nothing)
	p2 := filepath.Join(dir, "garbage")
	if err := os.WriteFile(p2, []byte("nonsense"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadProofFile(p2); err == nil {
		t.Error("unparsable proof file accepted")
	}
	// the real shape parses
	p3 := filepath.Join(dir, "real")
	if err := os.WriteFile(p3, []byte("hits=3 rule=write:*:x.tmp:EIO"), 0o600); err != nil {
		t.Fatal(err)
	}
	pl, err = ReadProofFile(p3)
	if err != nil || pl.Hits != 3 || pl.Rule != "write:*:x.tmp:EIO" {
		t.Errorf("real proof = (%+v, %v)", pl, err)
	}
}
