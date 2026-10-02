package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yamlpkg "gopkg.in/yaml.v3"
)

// yamlNode aliases the yaml package node type used by the custom unmarshalers.
type yamlNode = yamlpkg.Node

// sequenceNodeKind re-exports the yaml sequence kind for parseParamNode.
const sequenceNodeKind = yamlpkg.SequenceNode

// repairRule* name the mechanical pre-pass repairs. The catalog/faults corpus
// was authored with prose continuation lines inside plain scalars; YAML
// forbids two shapes that appear there (a "field: value" token inside a
// continuation, and an unindented "prose: more" line). The repairs exist so
// the read-only inputs load without editing them; each is named so its effect
// is auditable, and the upstream fix is one line per file.
const (
	// repairFieldish: inside a plain-scalar continuation under "what:", a
	// "breaks: ..." line is a scanner error; it is re-pointed as the mapping
	// entry it was meant to be.
	repairFieldish = "prose-continuation-fieldish"
	// repairUnindented: a coloned PROSE continuation ("(the TRBL-031 shape:
	// ...)") is merged into its key line as one quoted scalar.
	repairUnindented = "prose-continuation-coloned"
	// repairFlow: a flow-mapping plain value carrying a scanner-hostile
	// character — a bare colon ("peer: ip:port?") or a trailing "?"
	// ("retry_after: dur?"; "?" is libyaml's flow-context KEY indicator) —
	// is quoted whole.
	repairFlow = "flow-scalar-reserved-char"
)

// LoadError is a named refusal: it carries the file, the primitive id when
// known, and a reason naming the offending field or parse failure.
type LoadError struct {
	File   string
	ID     string
	Reason string
}

func (e *LoadError) Error() string {
	switch {
	case e.File != "" && e.ID != "":
		return fmt.Sprintf("%s (%s): %s", e.File, e.ID, e.Reason)
	case e.File != "":
		return fmt.Sprintf("%s: %s", e.File, e.Reason)
	default:
		return e.Reason
	}
}

// Catalog is the loaded primitive set plus its content-derived version.
type Catalog struct {
	// Version is content-derived (scheme content-v1): hex(sha256) over the
	// sorted "id<NUL>raw-file-bytes" pairs. Adding a primitive changes it;
	// the engine never needs to.
	Version string
	// VersionScheme names the derivation ("content-v1").
	VersionScheme string
	// Primitives is keyed by descriptor id.
	Primitives map[string]*Descriptor
	// IDs is the sorted id list (stable iteration).
	IDs []string
	// SourceDir is the directory the catalog was loaded from.
	SourceDir string
	// Warnings records every named repair the pre-pass applied (rule: file).
	Warnings []string
}

// CapabilityRegistry is the seam that decides availability (AC-11). The
// catalog never probes the host itself; an engine (or a test) supplies a
// registry and status derives from the descriptor's capability.
type CapabilityRegistry interface {
	// Available reports whether the named capability is present on this host.
	Available(kind string) bool
}

// RegistryFunc adapts a function to CapabilityRegistry.
type RegistryFunc func(kind string) bool

// Available implements CapabilityRegistry.
func (f RegistryFunc) Available(kind string) bool { return f(kind) }

// Status values are the derivable capability statuses of a primitive.
type Status string

const (
	// StatusReady: capability is none, or the registry reports it available.
	StatusReady Status = "ready"
	// StatusCapabilityUnavailable: the registry reports the capability
	// absent (AC-11); UnavailableNames carries what is absent.
	StatusCapabilityUnavailable Status = "capability_unavailable"
)

// StatusResult is the derived availability of one primitive.
type StatusResult struct {
	ID     string
	Status Status
	// UnavailableNames is the absent capability (empty when ready).
	UnavailableNames string
}

// Status derives AC-11 from the descriptor: "none" needs nothing; anything
// else is asked of the registry. The verb stays available either way — the
// primitive stays listed in the catalog and the status names what is absent.
func (d *Descriptor) Status(reg CapabilityRegistry) StatusResult {
	if d.Capability.Kind == "none" {
		return StatusResult{ID: d.ID, Status: StatusReady}
	}
	if reg != nil && reg.Available(d.Capability.Kind) {
		return StatusResult{ID: d.ID, Status: StatusReady}
	}
	return StatusResult{
		ID:               d.ID,
		Status:           StatusCapabilityUnavailable,
		UnavailableNames: d.Capability.Kind,
	}
}

// Get returns the descriptor with the given id, or nil.
func (c *Catalog) Get(id string) *Descriptor { return c.Primitives[id] }

// Len is the primitive count.
func (c *Catalog) Len() int { return len(c.Primitives) }

// LoadDir loads catalog/faults-style YAML descriptors from dir. It validates
// every descriptor against the fault contract and refuses malformed ones with
// a named reason (which file, which primitive, which field). Duplicate ids are
// refused. It never touches the engine.
func LoadDir(dir string) (*Catalog, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, &LoadError{Reason: fmt.Sprintf("catalog dir: %v", err)}
	}
	var files []string
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, &LoadError{Reason: fmt.Sprintf("no .yaml files in %s", dir)}
	}

	cat := &Catalog{
		Primitives:    map[string]*Descriptor{},
		SourceDir:     dir,
		VersionScheme: "content-v1",
	}
	var vin versionInputs

	for _, name := range files {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, &LoadError{File: name, Reason: fmt.Sprintf("read: %v", err)}
		}
		text, applied := repairPlainScalars(string(raw))
		for _, rule := range applied {
			cat.Warnings = append(cat.Warnings, fmt.Sprintf("%s: %s", rule, name))
		}
		var d Descriptor
		if err := yamlpkg.Unmarshal([]byte(text), &d); err != nil {
			return nil, &LoadError{File: name, Reason: fmt.Sprintf("yaml: %v", err)}
		}
		if err := d.validate(); err != nil {
			var le *LoadError
			if errors.As(err, &le) {
				le.File = name
				return nil, le
			}
			return nil, &LoadError{File: name, ID: d.ID, Reason: err.Error()}
		}
		if _, dup := cat.Primitives[d.ID]; dup {
			return nil, &LoadError{File: name, ID: d.ID, Reason: "duplicate id"}
		}
		cat.Primitives[d.ID] = &d
		cat.IDs = append(cat.IDs, d.ID)
		vin = append(vin, versionInput{id: d.ID, raw: raw})
	}
	sort.Strings(cat.IDs)
	cat.Version = vin.compute()
	return cat, nil
}

// DefaultDir is the in-repo catalog directory relative to the repo root.
const DefaultDir = "catalog/faults"

// LoadDefault loads the in-repo catalog (catalog/faults), resolving the
// directory from the process working directory upward to the repo root (the
// directory holding go.mod), so a future cmd/ binary can call it from any cwd.
func LoadDefault() (*Catalog, error) {
	dir, err := findDefaultDir()
	if err != nil {
		return nil, err
	}
	return LoadDir(dir)
}

func findDefaultDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", &LoadError{Reason: fmt.Sprintf("cwd: %v", err)}
	}
	for {
		cand := filepath.Join(wd, DefaultDir)
		if st, statErr := os.Stat(cand); statErr == nil && st.IsDir() {
			return cand, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			return "", &LoadError{Reason: fmt.Sprintf("%s not found from cwd upward", DefaultDir)}
		}
		wd = parent
	}
}

// versionInput is one file's contribution to the content version.
type versionInput struct {
	id  string
	raw []byte
}

type versionInputs []versionInput

// compute derives the content version: sha256 over sorted "id<NUL>raw bytes"
// pairs, hex-encoded. Raw bytes (not the repaired text) so the version tracks
// the on-disk inputs exactly.
func (v versionInputs) compute() string {
	sorted := make(versionInputs, len(v))
	copy(sorted, v)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].id < sorted[j].id })
	h := sha256.New()
	for _, in := range sorted {
		h.Write([]byte(in.id))
		h.Write([]byte{0})
		h.Write(in.raw)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// repairPlainScalars rewrites the named plain-scalar malformations and
// returns the repaired text plus the applied rule names.
//
// Grounding (probed against yaml.v3 directly): a plain multi-line scalar may
// continue on indented lines as long as no line carries a ": " (or a trailing
// colon) — a colon there is a scanner error ("mapping values are not allowed
// in this context"). The corpus has exactly two shapes of that error, both
// under a scalar-opening top-level key ("what: <prose>" with continuations):
//
// Rule prose-continuation-fieldish: the continuation is a "breaks: ..." line
// — a bare-word key with ": " that was meant to be the descriptor's next
// field. Repair: de-indent it to column 0, making it the mapping entry it
// was meant to be (semantics preserved: Breaks becomes its own field).
//
// Rule unindented-prose-continuation: the continuation is prose with an
// embedded ": " (I-002's "(the TRBL-031 shape: ...)"). Quoting the line is
// not enough (a plain scalar cannot continue into a quoted line); the repair
// merges the key line and its whole prose continuation segment into one
// double-quoted scalar.
//
// Gating: only continuations under a scalar-OPENING key ("key: value") are
// repaired. Children of a block opener ("landed_proof:" with no inline
// value — its "kind:"/"check:" lines) are never touched. A colon not
// followed by space ("ip:port") is not an error and is left alone. Both
// rules fire only where the strict parser would reject; everything the
// pre-pass leaves is still parsed by strict yaml.v3.
func repairPlainScalars(text string) (string, []string) {
	lines := strings.Split(text, "\n")
	applied := map[string]bool{}
	out := make([]string, 0, len(lines))

	// enclosing top-level key context
	key := ""
	opens := false

	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if ln == "" {
			out = append(out, ln)
			continue
		}
		if !isIndented(ln) {
			if fixed, changed := fixFlowColons(ln); changed {
				ln = fixed
				applied[repairFlow] = true
			}
			if k, rest, ok := splitKey(ln); ok {
				key, opens = k, opensScalarValue(rest)
			} else {
				key, opens = "", false
			}
			out = append(out, ln)
			continue
		}
		// indented line: a continuation only under a scalar-opening key
		if !opens {
			out = append(out, ln) // block child (landed_proof: kind/check) or stray
			continue
		}
		if !hasColonError(ln) {
			out = append(out, ln)
			continue
		}
		if fieldishColon(ln) >= 0 {
			// repair 1: the field-shaped line becomes the mapping entry it
			// was meant to be
			out = append(out, ln[indentWidth(ln):])
			applied[repairFieldish] = true
			key, opens = continuationFieldName(ln), false
			continue
		}
		// repair 2: merge key line + prose segment into one quoted scalar.
		// Walk back over the continuation lines already emitted to find the
		// key line, then rewrite them as one line.
		start := len(out) - 1
		for start > 0 && isIndented(lines[start]) && start >= 0 {
			start--
		}
		// out[start] is the key line (or block child); guard it
		if k, _, ok := splitKey(out[start]); !ok || k != key {
			out = append(out, ln) // cannot attribute; leave for strict parse
			continue
		}
		merged := []string{out[start]}
		for len(out)-1 > start {
			merged = append(merged, trimIndent(out[len(out)-1]))
			out = out[:len(out)-1]
		}
		merged = append(merged, trimIndent(ln))
		out[start] = key + ": " + quoteYAML(strings.Join(merged, " "))
		applied[repairUnindented] = true
	}
	var rules []string
	for _, r := range []string{repairFieldish, repairUnindented, repairFlow} {
		if applied[r] {
			rules = append(rules, r)
		}
	}
	return strings.Join(out, "\n"), rules
}

// fixFlowColons rewrites a line's flow mappings so that no plain-scalar flow
// value carries a bare colon ("peer: ip:port?" -> "peer: \"ip:port?\"").
// Values already quoted, and flow sequences (lists of scalar tokens), are
// left untouched. Depth-guarded: a colon inside a nested flow collection is
// structural ({"a": [b: c]}) and is left for the strict parser.
func fixFlowColons(ln string) (string, bool) {
	if !strings.Contains(ln, "{") {
		return ln, false
	}
	changed := false
	var b strings.Builder
	depth := 0 // { } nesting
	inFlowSeq := 0
	flowStart := -1 // line index of the innermost '{'
	i := 0
	for i < len(ln) {
		c := ln[i]
		switch c {
		case '{':
			if depth == 0 {
				flowStart = i
			}
			depth++
			i++
		case '}':
			depth--
			if depth == 0 && flowStart >= 0 {
				body := ln[flowStart+1 : i]
				fixed := fixFlowMapBody(body, &changed)
				b.WriteString("{")
				b.WriteString(fixed)
				b.WriteString("}")
				flowStart = -1
			}
			i++
		case '[', ']':
			if c == '[' {
				inFlowSeq++
			} else {
				inFlowSeq--
			}
			if depth == 0 {
				b.WriteByte(c)
			}
			i++
		case '"', '\'':
			if depth == 0 {
				// copy the quoted span verbatim
				q := c
				b.WriteByte(c)
				i++
				for i < len(ln) {
					b.WriteByte(ln[i])
					if ln[i] == q {
						i++
						break
					}
					i++
				}
			} else {
				i++
			}
		default:
			if depth == 0 {
				b.WriteByte(c)
			}
			i++
		}
	}
	if changed {
		return b.String(), true
	}
	return ln, false
}

// fixFlowMapBody rewrites one flow mapping's body ("netns: str, peer:
// ip:port?, ...") so every plain value carrying a bare colon is quoted
// whole ("peer: \"ip:port?\""). Values already quoted pass through; quoted
// KEYS are respected (the comma scan honours quotes).
func fixFlowMapBody(body string, changed *bool) string {
	var pairs []string
	var cur strings.Builder
	inQ := byte(0)
	for i := 0; i < len(body); i++ {
		c := body[i]
		if inQ != 0 {
			cur.WriteByte(c)
			if c == inQ {
				inQ = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			inQ = c
			cur.WriteByte(c)
		case ',':
			pairs = append(pairs, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	pairs = append(pairs, cur.String())

	var out strings.Builder
	for _, p := range pairs {
		if out.Len() > 0 {
			out.WriteString(", ")
		}
		p = strings.TrimSpace(p)
		ci := indexFlowColon(p)
		if ci < 0 {
			out.WriteString(p)
			continue
		}
		key := trimSpace(p[:ci])
		val := trimSpace(p[ci+1:])
		if val == "" || val[0] == '"' || val[0] == '\'' {
			out.WriteString(p)
			continue
		}
		// nested flow collections are structural, not scalars: pass through
		// unexamined (a hostile scalar inside a nested collection would fail
		// the strict parse loudly rather than be silently mangled)
		if val[0] == '{' || val[0] == '[' {
			out.WriteString(p)
			continue
		}
		// a value carrying a scanner-hostile char of its own — a colon
		// ("ip:port?") or a "?" (libyaml's flow KEY indicator) — is quoted
		// whole. A clean value is a structural "key: value" pair and
		// passes through.
		if strings.ContainsAny(val, ":?") {
			out.WriteString(key + `: "` + val + `"`)
			*changed = true
			continue
		}
		out.WriteString(p)
	}
	return out.String()
}

// indexFlowColon finds the key/value separating colon in a flow pair: the
// first ": " (space after), or a colon with a non-space follower (bare). It
// returns -1 when the pair has no colon outside quotes.
func indexFlowColon(p string) int {
	inQ := byte(0)
	for i := 0; i < len(p); i++ {
		c := p[i]
		if inQ != 0 {
			if c == inQ {
				inQ = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQ = c
			continue
		}
		if c == ':' {
			return i
		}
	}
	return -1
}

// isIndented reports whether the line starts with whitespace.
func isIndented(ln string) bool { return ln != "" && (ln[0] == ' ' || ln[0] == '\t') }

// splitKey splits a column-0 "key: rest" line.
func splitKey(ln string) (key, rest string, ok bool) {
	i := strings.Index(ln, ":")
	if i <= 0 {
		return "", "", false
	}
	return ln[:i], ln[i+1:], true
}

// opensScalarValue reports whether a key's inline value opens a plain scalar
// (non-empty, not a block indicator).
func opensScalarValue(rest string) bool {
	v := trimSpace(rest)
	if v == "" || strings.HasPrefix(v, "#") {
		return false
	}
	switch v {
	case "|", ">", "|-", ">-", "|+", ">+":
		return false
	}
	return true
}

// hasColonError reports whether a continuation line carries the scanner
// error: a ": " sequence or a trailing colon. A colon glued to the next byte
// ("ip:port") is not an error.
func hasColonError(ln string) bool {
	return strings.Contains(ln, ": ") || strings.HasSuffix(trimSpace(ln), ":")
}

// continuationFieldName extracts the bare-word key of a fieldish line.
func continuationFieldName(ln string) string {
	t := trimIndent(ln)
	if i := strings.Index(t, ":"); i > 0 {
		return t[:i]
	}
	return ""
}

// quoteYAML renders s as a double-quoted YAML scalar.
func quoteYAML(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "	", `	`)
	return `"` + r.Replace(s) + `"`
}

func trimIndent(ln string) string { return ln[indentWidth(ln):] }

// indentWidth counts leading spaces (tabs count as one).
func indentWidth(ln string) int {
	n := 0
	for n < len(ln) && (ln[n] == ' ' || ln[n] == '\t') {
		n++
	}
	return n
}

// fieldishColon locates a "word: rest" colon in a continuation line. The key
// must be a BARE WORD (letters/digits/underscore/hyphen, starting with a
// letter or underscore) — so "(the TRBL-031 shape: ...)" prose and
// "{a: b}" flow lines are not fieldish.
func fieldishColon(ln string) int {
	t := ln[indentWidth(ln):]
	j := strings.Index(t, ": ")
	if j < 0 && strings.HasSuffix(t, ":") {
		j = len(t) - 1
	}
	if j <= 0 {
		return -1
	}
	key := t[:j]
	if !isBareWord(key) {
		return -1
	}
	return j
}

// isBareWord reports whether s is a plain identifier-shaped token.
func isBareWord(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9', c == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// isTopLevelKeyLine reports whether the line is a genuine descriptor key
// line ("breaks:", "params:", ...) at column 0 — left untouched by the
// unindented rule.
func isTopLevelKeyLine(ln string) bool {
	i := strings.Index(ln, ":")
	if i <= 0 {
		return false
	}
	switch ln[:i] {
	case "id", "name", "what", "breaks", "params", "landed_proof", "inverse",
		"capability", "tier", "backend", "maturity", "selftest", "expect_when_healthy":
		return true
	}
	return false
}
