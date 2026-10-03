package rails

import (
	"sort"
	"time"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// ArmedFault is one fault armed inside one run: the primitive, its params,
// and its TTL.
type ArmedFault struct {
	// Primitive is the primitive id ("P-001").
	Primitive string
	// Params are the fault's parameters for this run.
	Params map[string]string
	// TTL bounds how long the fault may be held; the reverter (SPEC-04)
	// owns enforcement — the armed set only records it.
	TTL time.Duration
}

// ArmedSet is the per-run armed set (PRD §5: "the armed set is per-run
// session state — mischief run arms exactly the primitives its experiment
// declares, and two concurrent runs must not see each other's faults. The
// catalog is constant; the armed set is per-run and mutable").
//
// It is an in-memory record of ONE run. It is created per run, never
// global, and its methods write nothing outside the value itself: arming
// and disarming are operations on run state, so the global config file is
// byte-identical before and after (AC-12). Durability belongs to the
// journal (SPEC-02); TTL ownership to the reverter (SPEC-04).
type ArmedSet struct {
	byPrimitive map[string]*ArmedFault
}

// NewArmedSet returns an empty per-run armed set.
func NewArmedSet() *ArmedSet {
	return &ArmedSet{byPrimitive: map[string]*ArmedFault{}}
}

// Arm records a fault into THIS run's set and returns the record. It takes
// no global lock, touches no config file, and proves no fault landed —
// landing is the actuator's job and the journal's evidence.
func (a *ArmedSet) Arm(f ArmedFault) (*ArmedFault, error) {
	if f.Primitive == "" {
		return nil, newRefusal(ReasonNoTarget, "", "arm: primitive unnamed")
	}
	rec := &ArmedFault{Primitive: f.Primitive, Params: cloneParams(f.Params), TTL: f.TTL}
	a.byPrimitive[f.Primitive] = rec
	return rec, nil
}

// Disarm removes a primitive from this run's set; it reports whether the
// primitive was armed (a disarm of a never-armed fault is reported, not
// swallowed — SPEC-04 grades reverts, and a claimed revert of nothing is a
// no_op shape, not a pass).
func (a *ArmedSet) Disarm(primitive string) bool {
	_, ok := a.byPrimitive[primitive]
	delete(a.byPrimitive, primitive)
	return ok
}

// Get returns the armed record for a primitive, or nil.
func (a *ArmedSet) Get(primitive string) *ArmedFault { return a.byPrimitive[primitive] }

// Len is the count of armed faults in this run.
func (a *ArmedSet) Len() int { return len(a.byPrimitive) }

// IDs returns the sorted primitive ids armed in this run — the shape
// status/verdict surfaces print, and the shape two concurrent runs compare
// (AC-12: each run's status shows only its own armed set).
func (a *ArmedSet) IDs() []string {
	out := make([]string, 0, len(a.byPrimitive))
	for id := range a.byPrimitive {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Contains reports whether the primitive is armed in this run.
func (a *ArmedSet) Contains(primitive string) bool { _, ok := a.byPrimitive[primitive]; return ok }

// PlanFrom builds the per-run armed set an experiment implies, checking
// every declared fault against the rails first (scope ladder, selftest, L5
// gate) so an experiment that fails a rail arms nothing. The scope is the
// one the run resolved to; each primitive is checked at that scope.
//
// This is the seam between the catalog (data) and a run (state): the
// catalog never mutates, the armed set is per-run, and the rails gate the
// edge between them.
func PlanFrom(cat *catalog.Catalog, declared []ArmedFault, s Scope) (*ArmedSet, error) {
	for _, f := range declared {
		d := cat.Get(f.Primitive)
		if d == nil {
			return nil, newRefusal(ReasonScope, f.Primitive, "not in the catalog: a fault that is not a primitive cannot be armed")
		}
		if err := CheckScope(d, s); err != nil {
			return nil, err
		}
	}
	set := NewArmedSet()
	for _, f := range declared {
		if _, err := set.Arm(f); err != nil {
			return nil, err
		}
	}
	return set, nil
}

func cloneParams(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) evidence=internal/rails/armed.go witness=none:no-live-host-run-in-worktree
