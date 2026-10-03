package backend_signal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// MatchKind is the anchoring class of a rule pattern (the MSF-028 fix: the
// language has no bare-substring class at all).
type MatchKind string

const (
	// MatchExact: the pattern is a literal path; byte equality only.
	MatchExact MatchKind = "exact"
	// MatchPrefix: pattern "dir:*" — the dir itself or anything under it
	// (path-boundary: "dir:*" never matches "dir.txt.evil").
	MatchPrefix MatchKind = "prefix"
	// MatchSuffix: pattern "*:name" — the name itself or anything ending
	// in "/name" (path-boundary: "*:cache.tmp" never matches "xcache.tmp").
	MatchSuffix MatchKind = "suffix"
)

// ErrnoNames is the closed errno vocabulary of the rule language. The legacy
// probe shim silently mapped unknown errno names to EIO; the v0.1 language
// refuses them at parse time instead (a silently-different fault is a
// green-but-meaningless run).
var ErrnoNames = []string{
	"EIO", "ENOSPC", "EACCES", "EDQUOT", "ENOMEM", "EINTR", "ETIMEDOUT", "EAGAIN", "EPIPE",
}

// ValidErrno reports whether name is in the closed errno vocabulary.
func ValidErrno(name string) bool {
	for _, n := range ErrnoNames {
		if n == name {
			return true
		}
	}
	return false
}

// Rule is one parsed MCFAULT rule: the fault language of the LD_PRELOAD
// shim. The text form (env MCFAULT) is sys:pattern:errno — exactly three
// slots, with the pattern parsed GREEDILY between the first and last
// colon. The greedy middle is what makes absolute paths possible
// ("write:/tmp/x.tmp:EIO") and what keeps the anchored spellings
// ("*:name", "dir:*") unambiguous — the legacy strtok-on-every-colon parse
// would read the suffix anchor's name as the errno slot. The optional
// budget and the proof channel travel in their own env vars
// (MCFAULT_BUDGET, MCFAULT_PROOF), not in the rule text: one encoding, one
// parsing rule, no slot-ambiguity by construction.
type Rule struct {
	// Sys is the intercepted syscall name (v0.1: "write").
	Sys string
	// Pattern is the target path pattern exactly as written.
	Pattern string
	// Errno is the errno name injected on a matching write.
	Errno string
	// Budget is how many matching writes may fail: -1 unlimited (the
	// default when MCFAULT_BUDGET is absent), 0 = the armed-but-never-fires
	// rule (the expressible no-op arm for AC-3 testing), N = the first N
	// matching writes fail.
	Budget int
	// ProofPath is the counter file the shim writes on every hit (the
	// landed-proof contract: probe/RESULTS.md "mc-fault-hits.<pid>").
	ProofPath string

	match  MatchKind
	anchor string
}

// ParseRule parses and validates one rule. Every failure names the offending
// field. Bare-substring patterns are refused here (the anchoring contract):
// the only wildcard forms are the anchored "prefix:*" and "*:suffix".
func ParseRule(s string) (Rule, error) {
	fields := strings.Split(s, ":")
	if len(fields) < 3 {
		return Rule{}, fmt.Errorf("rule %q: must be sys:pattern:errno (pattern may contain colons; errno is the last slot)", s)
	}
	r := Rule{Sys: fields[0], Errno: fields[len(fields)-1], Budget: -1}
	r.Pattern = strings.Join(fields[1:len(fields)-1], ":")
	if r.Sys == "" {
		return Rule{}, fmt.Errorf("rule %q: empty syscall name", s)
	}
	if r.Pattern == "" {
		return Rule{}, fmt.Errorf("rule %q: empty pattern", s)
	}
	if !ValidErrno(r.Errno) {
		return Rule{}, fmt.Errorf("rule %q: unknown errno %q (supported: %s)",
			s, r.Errno, strings.Join(ErrnoNames, ","))
	}
	mk, anchor, err := parsePattern(s, r.Pattern)
	if err != nil {
		return Rule{}, err
	}
	// v0.1 implements exactly one intercepted syscall; anything else is a
	// named refusal (S-005/S-008 classes ride the seccomp backend, not the
	// shim).
	if r.Sys != "write" {
		return Rule{}, fmt.Errorf("rule %q: unsupported syscall %q (v0.1 implements write only; open/fsync/connect classes belong to other backends)", s, r.Sys)
	}
	r.match, r.anchor = mk, anchor
	// NOTE: the proof channel is not part of the MCFAULT text (it travels
	// in MCFAULT_PROOF), so a parse-time proof self-match guard would be
	// unreachable here; the reachable guard lives in ArmShim, which sees
	// the rule WITH its proof path.
	return r, nil
}

// WithBudget sets how many matching writes the rule may fail (immutable
// style: returns the modified copy). 0 = armed but never fires.
func (r Rule) WithBudget(n int) (Rule, error) {
	if n < 0 {
		return r, fmt.Errorf("budget %d must be >= 0 (use the default -1 for unlimited)", n)
	}
	r.Budget = n
	return r, nil
}

// parsePattern classifies the anchoring of one pattern: exact, the
// path-boundary prefix arm "X:*", or the path-boundary suffix arm "*:X".
// Anything else — including a bare substring and a mixed wildcard like
// "a*b" — is a named refusal.
func parsePattern(rule, pat string) (MatchKind, string, error) {
	switch {
	case strings.HasSuffix(pat, ":*"):
		a := strings.TrimSuffix(pat, ":*")
		if a == "" || strings.ContainsRune(a, '*') {
			return "", "", fmt.Errorf("rule %q: prefix anchor must be a literal path, got %q", rule, pat)
		}
		return MatchPrefix, a, nil
	case strings.HasPrefix(pat, "*:"):
		a := strings.TrimPrefix(pat, "*:")
		if a == "" || strings.ContainsRune(a, '*') {
			return "", "", fmt.Errorf("rule %q: suffix anchor must be a literal name, got %q", rule, pat)
		}
		return MatchSuffix, a, nil
	}
	if strings.ContainsRune(pat, '*') {
		return "", "", fmt.Errorf(
			"rule %q: pattern %q mixes a wildcard into an otherwise literal path — the only anchored forms are \"prefix:*\" and \"*:suffix\"",
			rule, pat)
	}
	// MSF-028: an fd target (a /proc/<pid>/fd readlink result) is always
	// an absolute path, so a non-absolute literal pattern could never
	// match anything EXCEPT as the legacy bare-substring idiom — which is
	// the flaw this language exists to refuse.
	if !strings.HasPrefix(pat, "/") {
		return "", "", fmt.Errorf(
			"rule %q: pattern %q is not an absolute path — a relative fragment is the legacy bare-substring idiom (MSF-028) and is refused; use an absolute exact path, \"dir:*\" or \"*:name\"",
			rule, pat)
	}
	return MatchExact, pat, nil
}

// Matches reports whether an fd target path (a /proc/<pid>/fd readlink
// result) is selected by the rule. The proof channel is always self-excluded:
// a write to the proof path never matches, whatever the pattern says.
func (r Rule) Matches(target string) bool {
	if r.ProofPath != "" && target == r.ProofPath {
		return false
	}
	return r.matchesUnchecked(target)
}

func (r Rule) matchesUnchecked(target string) bool {
	switch r.match {
	case MatchExact:
		return target == r.anchor
	case MatchPrefix:
		return target == r.anchor ||
			(len(target) > len(r.anchor) &&
				strings.HasPrefix(target, r.anchor) &&
				target[len(r.anchor)] == '/')
	case MatchSuffix:
		return target == r.anchor ||
			(len(target) > len(r.anchor) &&
				strings.HasSuffix(target, r.anchor) &&
				target[len(target)-len(r.anchor)-1] == '/')
	}
	return false
}

// Anchoring reports the rule's anchoring class.
func (r Rule) Anchoring() MatchKind { return r.match }

// canonicalText renders the canonical rule text (sys:pattern:errno);
// ParseRule(canonicalText) round-trips.
func (r Rule) canonicalText() string {
	return r.Sys + ":" + r.Pattern + ":" + r.Errno
}

// String renders the canonical rule text including an explicit proof path
// when one is set; ParseRule(String()) round-trips.
func (r Rule) String() string {
	if r.ProofPath == "" {
		return r.canonicalText()
	}
	return r.canonicalText() + ":" + r.ProofPath
}

// Env returns the environment additions that arm a child for this rule:
// LD_PRELOAD carries the shim, MCFAULT the rule text, MCFAULT_BUDGET the
// hit budget (omitted when unlimited), MCFAULT_PROOF the counter path. The
// budget and the proof path travel separately so the shim's parsing rule
// stays exactly three slots and its self-exclusion never depends on
// parsing the rule.
func (r Rule) Env(libPath string) []string {
	env := []string{
		"LD_PRELOAD=" + libPath,
		"MCFAULT=" + r.canonicalText(),
	}
	if r.Budget >= 0 {
		env = append(env, "MCFAULT_BUDGET="+strconv.Itoa(r.Budget))
	}
	env = append(env, "MCFAULT_PROOF="+r.ProofPath)
	return env
}

// DeriveProofPath derives the counter file path for one rule inside dir.
// The content-derived name (sha256 of the canonical rule) means two
// different rules never share a counter and the same rule is idempotent.
func DeriveProofPath(ruleText, dir string) string {
	sum := sha256.Sum256([]byte(ruleText))
	return filepath.Join(dir, "mc-proof-"+hex.EncodeToString(sum[:])[:16]+".jsonl")
}
