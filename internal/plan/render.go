package plan

import (
	"fmt"
	"strings"
)

// Render produces the plan's deterministic text form. BYTE-IDENTICAL
// GUARANTEE (AC-1's plan half): the same Plan renders to the same bytes —
// every map-derived list is sorted, every iteration is over sorted keys,
// no timestamp, pid, or hostname enters the text. The plan is the run dir's
// plan.txt: two plans of the same experiment+catalog+capability state diff
// empty.
//
// The refusal exit contract lives in the caller (cmd): a plan carrying
// refusals exits 2 per rails.MapExit; the RENDER is still complete (the
// operator sees everything that was resolved before the refusal).
func Render(p *Plan) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "plan: %s\n", p.ExperimentID)
	fmt.Fprintf(&b, "run_id: %s\n", p.RunID)
	fmt.Fprintf(&b, "seed: %d\n", p.Seed)
	fmt.Fprintf(&b, "catalog_version: %s\n", p.CatalogVersion)
	fmt.Fprintf(&b, "target:\n")
	fmt.Fprintf(&b, "  kind: %s\n", p.Target.Kind)
	fmt.Fprintf(&b, "  selector: %s\n", p.Target.Selector)
	if p.Target.IsProtected() {
		fmt.Fprintf(&b, "  protected: %s (operator override recorded)\n", p.Target.Protected)
	}
	fmt.Fprintf(&b, "faults:\n")
	for _, f := range p.Faults {
		fmt.Fprintf(&b, "  - primitive: %s\n", f.Primitive)
		fmt.Fprintf(&b, "    params: %s\n", f.Params)
		fmt.Fprintf(&b, "    ttl: %s\n", f.TTLRaw)
		fmt.Fprintf(&b, "    tier: %s\n", f.Tier)
		fmt.Fprintf(&b, "    capability: %s\n", f.Capability)
		fmt.Fprintf(&b, "    status: %s\n", f.Status)
		if f.UnavailableNames != "" {
			fmt.Fprintf(&b, "    unavailable: %s\n", f.UnavailableNames)
		}
		fmt.Fprintf(&b, "    selftest: %s\n", f.Selftest)
		fmt.Fprintf(&b, "    landed_proof:\n")
		fmt.Fprintf(&b, "      kind: %s\n", f.LandedProof.Kind)
		fmt.Fprintf(&b, "      check: %s\n", f.LandedProof.Check)
		fmt.Fprintf(&b, "    inverse:\n")
		fmt.Fprintf(&b, "      action: %s\n", f.Inverse.Action)
		if len(f.Inverse.Params) > 0 {
			keys := sortedKeys(f.Inverse.Params)
			fmt.Fprintf(&b, "      params:\n")
			for _, k := range keys {
				fmt.Fprintf(&b, "        %s: %s\n", k, f.Inverse.Params[k])
			}
		}
		if f.Inverse.Verify != "" {
			fmt.Fprintf(&b, "      verify: %s\n", f.Inverse.Verify)
		}
	}
	fmt.Fprintf(&b, "probes:\n")
	for _, pr := range p.Probes {
		fmt.Fprintf(&b, "  - name: %s\n", pr.Name)
		fmt.Fprintf(&b, "    cmd: %s\n", pr.Cmd)
		fmt.Fprintf(&b, "    expect: %s\n", pr.Expect)
	}
	fmt.Fprintf(&b, "budget:\n")
	fmt.Fprintf(&b, "  recover_within: %s\n", p.Budget.RecoverWithin)
	fmt.Fprintf(&b, "  max_error_rate: %g\n", p.Budget.MaxErrorRate)
	if len(p.Refusals) > 0 {
		fmt.Fprintf(&b, "refusals:\n")
		for _, r := range p.Refusals {
			fmt.Fprintf(&b, "  - %s\n", r)
		}
	}
	if len(p.Notes) > 0 {
		fmt.Fprintf(&b, "notes:\n")
		for _, n := range p.Notes {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	}
	return []byte(b.String())
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ { // insertion sort; small maps only
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
