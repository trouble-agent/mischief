// Package catalog is SPEC-01: the fault model and the primitive catalog.
//
// The fault contract (docs/SPEC-PLAN.md build-order rule): every primitive
// descriptor MUST state landed_proof, inverse and capability — the loader
// refuses a primitive missing any of the three. A primitive that passes the
// loader needs no engine code change; the catalog is data.
//
// Glossary (names owned here, reused by later specs):
//
//   - Primitive: one injectable fault, described by a Descriptor.
//   - Descriptor: the machine-readable fault contract of one primitive.
//   - Catalog: the loaded primitive set plus a content-derived version.
//   - Capability: host support a primitive needs to land; availability is
//     derivable per descriptor via the CapabilityRegistry seam (AC-11).
//   - SelftestState: whether the primitive has a green selftest on this host;
//     the AC-19 refusal outside L0 is expressible from the schema alone.
//
// NOT-LIST (what SPEC-01 does not promise — see docs/SPEC-PLAN.md):
//
//   - The catalog does not promise a primitive can land on this host; it
//     promises the descriptor states what landing and reverting must prove
//     (landed_proof / inverse) and what the host must supply (capability).
//   - It does not promise every fault is revertible; it promises every
//     descriptor carries an inverse record, and the loader refuses a
//     primitive whose inverse cannot be expressed.
//   - It does not promise a green selftest: selftest state is expressed in
//     the schema (SelftestState, defaulting to unknown) and enforced
//     downstream (AC-19), not here.
//   - It does not grade or execute anything: no landing, no inverse
//     execution, no host probing — status() consults the registry seam.
//   - Param declarations are taken verbatim (Decl); a param whose declaration
//     names none of the known kinds reports Kinds [any] and Typed=false
//     rather than being guessed into a type.
//   - Version is content-derived: it changes when descriptor bytes change
//     and proves nothing beyond the bytes it hashes.
package catalog

import "fmt"

// Tier is the scope ladder L0..L5 (SPEC-03 owns the ladder; the catalog only
// expresses it). A descriptor's tier may name sub-levels for its param modes
// (e.g. "L0 (copy) / L1 (declared path)"), so the minimum level is parsed out.
type Tier struct {
	// Raw is the declared tier string exactly as written.
	Raw string
	// Min is the minimum L-level named in Raw (0..5); -1 when Raw names no
	// level (a loader validation failure, reported by name).
	Min int
}

// UnmarshalYAML parses the tier field: any L0..L5 token inside the string
// sets Min; the raw string is kept verbatim.
func (t *Tier) UnmarshalYAML(value *yamlNode) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("tier: must be a string naming an L0..L5 level")
	}
	*t = Tier{Raw: s, Min: -1}
	// the LOWEST level named wins (the tier states the minimum scope the
	// primitive can run at)
	for i := 0; i <= 5; i++ {
		if containsToken(s, tierToken(i)) {
			t.Min = i
			break
		}
	}
	return nil
}

func tierToken(i int) string { return fmt.Sprintf("L%d", i) }

// containsToken reports whether tok occurs in s un-glued from neighbours
// ("L1-only" contains L1; "FL1" and "L12" do not contain L1).
func containsToken(s, tok string) bool {
	n := len(tok)
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] != tok {
			continue
		}
		if i > 0 && isAlnum(s[i-1]) {
			continue
		}
		if i+n < len(s) && isAlnum(s[i+n]) {
			continue
		}
		return true
	}
	return false
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// SelftestState is the declared selftest state of a primitive on this host.
// Every value except green is an AC-19 refused state outside an L0 scope;
// "unknown" is the honest default for a fresh descriptor.
type SelftestState string

const (
	// SelftestGreen: a green selftest exists on this host.
	SelftestGreen SelftestState = "green"
	// SelftestMissing: no selftest exists.
	SelftestMissing SelftestState = "missing"
	// SelftestFailing: a selftest exists but is red on this host.
	SelftestFailing SelftestState = "failing"
	// SelftestUnknown: selftest state has not been established on this host.
	SelftestUnknown SelftestState = "unknown"
)

// parseSelftest validates a declared selftest string.
func parseSelftest(s string) (SelftestState, error) {
	switch st := SelftestState(s); st {
	case SelftestGreen, SelftestMissing, SelftestFailing, SelftestUnknown:
		return st, nil
	default:
		return "", fmt.Errorf("selftest: invalid value %q (must be green|missing|failing|unknown)", s)
	}
}

// RefusesOutsideL0 is the AC-19 refusal predicate: the primitive may not run
// against anything above an L0 scratch scope unless its selftest is green.
func (s SelftestState) RefusesOutsideL0() bool {
	return s != SelftestGreen
}

// ParamKind is a known param type token. Declarations may also carry example
// or constraint values ("64M", "EIO", "0..1"); those stay verbatim in Decl
// and Kinds falls back to any.
type ParamKind string

const (
	KindStr      ParamKind = "str"
	KindInt      ParamKind = "int"
	KindFloat    ParamKind = "float"
	KindBool     ParamKind = "bool"
	KindDur      ParamKind = "dur"
	KindIPPort   ParamKind = "ip:port"
	KindSelector ParamKind = "selector"
	KindList     ParamKind = "list"
	KindAny      ParamKind = "any"
)

func knownKind(k ParamKind) bool {
	switch k {
	case KindStr, KindInt, KindFloat, KindBool, KindDur, KindIPPort, KindSelector, KindList, KindAny:
		return true
	}
	return false
}

// ParamSpec is one declared param.
type ParamSpec struct {
	// Name is the param name.
	Name string
	// Decl is the declared type/constraint string verbatim.
	Decl string
	// Optional is true when the declaration carries a trailing "?".
	Optional bool
	// Typed reports whether the declaration named at least one known kind.
	Typed bool
	// Kinds are the known kind tokens in the declaration (alternation kept
	// in declared order); [any] when the declaration names none.
	Kinds []ParamKind
	// Enum carries pipe-alternated value tokens when the declaration is an
	// enumeration ("replace|unlink-then-create|restore-snapshot").
	Enum []string
	// Values carries the declared value list when the param is a sequence
	// ("syscalls: [clock_gettime, gettimeofday]").
	Values []string
}

// Params is the param schema of a primitive, keyed by param name.
type Params map[string]*ParamSpec

// UnmarshalYAML accepts a mapping of name -> declaration. A scalar
// declaration is parsed as kinds/enum/optional; a sequence declaration is a
// value list.
func (p *Params) UnmarshalYAML(value *yamlNode) error {
	var m map[string]yamlNode
	if err := value.Decode(&m); err != nil {
		return fmt.Errorf("params: must be a mapping of name->type")
	}
	out := Params{}
	for name, node := range m {
		ps, err := parseParamNode(name, &node)
		if err != nil {
			return err
		}
		out[name] = ps
	}
	*p = out
	return nil
}

func parseParamNode(name string, node *yamlNode) (*ParamSpec, error) {
	if node.Kind == sequenceNodeKind {
		ps := &ParamSpec{Name: name, Decl: node.Value, Typed: true, Kinds: []ParamKind{KindList}}
		for i := range node.Content {
			var v string
			if err := node.Content[i].Decode(&v); err != nil {
				return nil, fmt.Errorf("params.%s: sequence values must be strings", name)
			}
			ps.Values = append(ps.Values, v)
		}
		return ps, nil
	}
	var decl string
	if err := node.Decode(&decl); err != nil {
		return nil, fmt.Errorf("params.%s: declaration must be a scalar or a list", name)
	}
	return parseParamDecl(name, decl)
}

// parseParamDecl parses a scalar declaration: optional "?", pipe tokens, each
// either a known kind or an enum value.
func parseParamDecl(name, decl string) (*ParamSpec, error) {
	d := decl
	optional := false
	if len(d) > 0 && d[len(d)-1] == '?' {
		optional = true
		d = d[:len(d)-1]
	}
	if trimSpace(d) == "" {
		return nil, fmt.Errorf("params.%s: empty type", name)
	}
	ps := &ParamSpec{Name: name, Decl: decl, Optional: optional}
	for _, tok := range splitPipe(d) {
		tok = trimSpace(tok)
		if tok == "" {
			return nil, fmt.Errorf("params.%s: empty alternation token in %q", name, decl)
		}
		if k := ParamKind(tok); knownKind(k) {
			ps.Kinds = append(ps.Kinds, k)
			continue
		}
		ps.Enum = append(ps.Enum, tok)
	}
	if len(ps.Kinds) == 0 {
		switch {
		case len(ps.Enum) > 1:
			// an enumeration of values implies a string param
			ps.Kinds = []ParamKind{KindStr}
		case len(ps.Enum) == 1:
			// a single value token is an example/constraint ("64M", "EIO"),
			// not an enumeration: keep verbatim, type any
			ps.Kinds = []ParamKind{KindAny}
		default:
			ps.Kinds = []ParamKind{KindAny}
		}
	}
	ps.Typed = !(len(ps.Kinds) == 1 && ps.Kinds[0] == KindAny)
	return ps, nil
}

func splitPipe(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '|' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// LandedProof is the observable condition proving the fault landed,
// independent of the fault's own exit code (AC-13 shape; SPEC-05 grades).
type LandedProof struct {
	Kind  string `yaml:"kind"`
	Check string `yaml:"check"`
}

// Inverse is the recorded reverter contract: how to undo the fault and how to
// verify the undo (SPEC-04 consumes this; the catalog requires it stated).
type Inverse struct {
	// Action is a primitive id ("P-001": re-run with different params) or a
	// named action ("restore-original", "delete-qdisc", ...).
	Action string
	// Params carries the action's parameters when the action is a primitive id.
	Params map[string]string
	// Verify is the observable condition proving the undo landed.
	Verify string
}

// UnmarshalYAML normalises the inverse forms: a bare action string,
// {action, params?, verify?}, or {primitive, params?, verify?} (the
// self-inverse form P-001 uses).
func (iv *Inverse) UnmarshalYAML(value *yamlNode) error {
	var s string
	if err := value.Decode(&s); err == nil {
		if trimSpace(s) == "" {
			return fmt.Errorf("inverse: empty action")
		}
		*iv = Inverse{Action: s}
		return nil
	}
	var m struct {
		Action    *string           `yaml:"action"`
		Primitive *string           `yaml:"primitive"`
		Params    map[string]string `yaml:"params"`
		Verify    *string           `yaml:"verify"`
	}
	if err := value.Decode(&m); err != nil {
		return fmt.Errorf("inverse: must be an action string or a mapping with action/primitive")
	}
	act := ""
	if m.Action != nil {
		act = *m.Action
	}
	if m.Primitive != nil {
		if act != "" {
			return fmt.Errorf("inverse: both action and primitive set")
		}
		act = *m.Primitive
	}
	if trimSpace(act) == "" {
		return fmt.Errorf("inverse: missing action")
	}
	verify := ""
	if m.Verify != nil {
		verify = *m.Verify
	}
	*iv = Inverse{Action: act, Params: m.Params, Verify: verify}
	return nil
}

// Capability states the host support a primitive needs. "none" means nothing
// beyond the backend machinery itself. Availability is derivable from the
// descriptor via the registry seam (status), so AC-11 stays a schema question.
type Capability struct {
	// Kind is the requirement token: "none" or a named capability
	// ("NET_ADMIN", "cgroup2", "docker", ...).
	Kind string
	// Detail is the declared prose after the token, kept verbatim
	// ("cgroup2 + user scope" -> "user scope").
	Detail string
}

// UnmarshalYAML splits the capability string into a leading token and prose
// detail: "none (shim); ..." -> Kind "none"; "cgroup2 + user scope" -> Kind
// "cgroup2", Detail "user scope"; "NET_ADMIN" -> Kind "NET_ADMIN".
func (c *Capability) UnmarshalYAML(value *yamlNode) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("capability: must be a string")
	}
	s = trimSpace(s)
	if s == "" {
		return fmt.Errorf("capability: empty")
	}
	kind, detail := s, ""
	if i := indexAnyByte(s, " \t("); i >= 0 {
		kind, detail = s[:i], trimSpace(s[i:])
	}
	*c = Capability{Kind: kind, Detail: detail}
	return nil
}

func indexAnyByte(s, set string) int {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(set); j++ {
			if s[i] == set[j] {
				return i
			}
		}
	}
	return -1
}

// trimSpace is a byte-set trim; avoids importing strings for two callers.
func trimSpace(s string) string {
	start := 0
	for start < len(s) && isSpaceByte(s[start]) {
		start++
	}
	end := len(s)
	for end > start && isSpaceByte(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// Descriptor is the primitive descriptor: the machine-readable fault
// contract of one primitive.
type Descriptor struct {
	ID          string        `yaml:"id"`
	Name        string        `yaml:"name"`
	What        string        `yaml:"what"`
	Breaks      string        `yaml:"breaks"`
	Params      Params        `yaml:"params"`
	LandedProof LandedProof   `yaml:"landed_proof"`
	Inverse     Inverse       `yaml:"inverse"`
	Capability  Capability    `yaml:"capability"`
	Tier        *Tier         `yaml:"tier"`
	Backend     string        `yaml:"backend"`
	Maturity    string        `yaml:"maturity"`
	Selftest    SelftestState `yaml:"selftest"`
}

// validate enforces the fault contract and field rules, naming the offending
// field on refusal. Selftest is optional (absent -> unknown, which refuses
// outside L0); the three contract fields are not.
func (d *Descriptor) validate() error {
	if d.ID == "" {
		return fmt.Errorf("id: missing")
	}
	if d.Name == "" {
		return fmt.Errorf("%s: name: missing", d.ID)
	}
	if d.What == "" {
		return fmt.Errorf("%s: what: missing", d.ID)
	}
	if len(d.Params) == 0 {
		return fmt.Errorf("%s: params: missing", d.ID)
	}
	if d.LandedProof.Kind == "" || d.LandedProof.Check == "" {
		return fmt.Errorf("%s: landed_proof: missing (kind and check are required)", d.ID)
	}
	if d.Inverse.Action == "" {
		// the fault contract: no inverse, no primitive
		return fmt.Errorf("%s: inverse: missing (the fault contract: a primitive MUST state inverse + landed_proof + capability)", d.ID)
	}
	if d.Capability.Kind == "" {
		return fmt.Errorf("%s: capability: missing", d.ID)
	}
	if d.Tier == nil || d.Tier.Min < 0 {
		return fmt.Errorf("%s: tier: missing (must name an L0..L5 level)", d.ID)
	}
	if d.Backend == "" {
		return fmt.Errorf("%s: backend: missing", d.ID)
	}
	if d.Maturity == "" {
		return fmt.Errorf("%s: maturity: missing", d.ID)
	}
	if d.Selftest == "" {
		// absent selftest is honestly "unknown" (which refuses outside L0)
		d.Selftest = SelftestUnknown
	}
	if _, err := parseSelftest(string(d.Selftest)); err != nil {
		return fmt.Errorf("%s: %v", d.ID, err)
	}
	return nil
}
