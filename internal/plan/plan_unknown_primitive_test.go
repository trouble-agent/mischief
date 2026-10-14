package plan

import (
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// DF-01: a fault whose primitive is absent from the loaded catalog must
// produce a TYPED refusal row (status "unknown_primitive" plus a refusal
// reason), never an empty-fields row. A scripted consumer branches on
// status; it must not have to parse notes lines to discover the gap.
func TestPlanUnknownPrimitiveIsTypedRefusal(t *testing.T) {
	cat, _ := testSetup(t)
	unknownExp := strings.Replace(testExperiment, "primitive: N-012", "primitive: F-999", 1)
	exp, err := parseExp([]byte(unknownExp))
	if err != nil {
		t.Fatalf("parse experiment: %v", err)
	}
	p, err := Build(exp, []byte(unknownExp), cat, allAvailable, testScope())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(p.Faults) != 1 {
		t.Fatalf("faults: %d, want 1", len(p.Faults))
	}
	f := p.Faults[0]
	if f.Status != catalog.StatusUnknownPrimitive {
		t.Fatalf("unknown primitive status = %q, want %q", f.Status, catalog.StatusUnknownPrimitive)
	}
	if len(p.Refusals) == 0 {
		t.Fatal("unknown primitive produced no refusal reason")
	}
	// The fields that used to come back empty must stay EMPTY-STRING-FREE
	// only where the descriptor defines them — here they are correctly
	// absent because there is no descriptor; the typed status is the
	// contract, so assert there is a notes line naming the id for humans.
	found := false
	for _, n := range p.Notes {
		if strings.Contains(n, "F-999") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no notes line names the unknown primitive: %v", p.Notes)
	}
}
