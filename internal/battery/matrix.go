// Package battery is SPEC-10: the battery/matrix runner — the fleet-facing
// product surface (docs/SPEC-PLAN.md SPEC-10; PRD §6.1 `battery`, §12 S-5).
//
// A battery is a MATRIX (faults × targets) executed cell by cell: each cell
// lands its primitive on an L0 scratch target the run owns, MEASURES the
// landed proof and the revert (the same harness `mischief selftest` uses —
// no second actuator), grades the cell through the SPEC-05 verdict engine
// (verdict.Grade consumes measured evidence; nothing here string-maps a
// verdict), and records the outcome as journal records in the
// internal/journal format. The battery then emits the detection-matrix
// artefact — fault × detection × recovery, as JSON + a markdown table —
// under the run dir, and can convert failed verdicts into board rows in
// the mischief board's own vocabulary (AC-18).
//
// --quick parity (S-5 / M4): the quick matrix is the reduced matrix whose
// cells correspond ONE TO ONE to the five legacy bash chaos cells in
// ~/.hermes/scripts/bunker-qa.sh (chaos-disconnect, chaos-shutdown,
// chaos-corruption, chaos-resource, chaos-errorpath). Every quick cell
// names the legacy cell it replaces and the catalog primitive that replaces
// it; the parity property is pinned by test (a primitive set that stops
// covering a named legacy cell fails the suite). The legacy cells are NOT
// deleted by this build — deletion happens in a later milestone, after
// parity is proven live (the artefact is the evidence that will justify it).
//
// Detection (AC-14): each cell carries an optional Detector. The real
// trouble-ledger integration is FUTURE WORK — the seam exists (an
// interface the runner queries per cell), and a cell without a detector
// records a reason-bearing "not-wired" detection entry, never a bare
// false. Nothing in this package fakes a live ledger read.
//
// NOT-LIST (what SPEC-10 does not promise at M1):
//
//   - No live trouble ledger reads: the Detector seam is the AC-14
//     assertion point; wiring it to observe.trouble is tracked as future
//     work and nothing here pretends otherwise.
//   - No non-scratch targets: M1 executes on L0 scratch targets the run
//     owns (the selftest precedent); a target that declares Scratch=false
//     refuses (rails shape, exit 2) rather than landing on a shared host.
//   - No matrix file parser yet (`-f matrix.yaml` of PRD §6.1): the M1
//     matrix is derived from the catalog (all primitives) or the quick
//     parity set; the file format arrives with the fleet rollout.
//   - Findings are EMITTED as board-vocabulary JSONL (validated
//     in-package, the `boardctl validate`-style self-check); writing them
//     onto a live board is the operator/foreman merge step, not this
//     verb's side effect.
package battery

import (
	"fmt"
	"sort"
	"strings"
)

// LegacyCell is one legacy bash chaos cell from ~/.hermes/scripts/bunker-qa.sh
// and the catalog primitive that replaces it. The pairing is the parity
// contract of --quick (S-5): one cell, one primitive, one named replacement
// reason.
type LegacyCell struct {
	// Cell is the legacy cell name exactly as bunker-qa.sh names it
	// ("chaos-disconnect") — the artefact carries it verbatim so the
	// later deletion diff can cite this file.
	Cell string
	// Primitive is the replacing catalog primitive id ("N-012").
	Primitive string
	// Replaces is the one-line mapping rationale (what the cell did, and
	// which primitive shape now covers it).
	Replaces string
}

// legacyCells is THE parity table. The five cells are read from
// ~/.hermes/scripts/bunker-qa.sh (cells 6-10 in that file: network cut,
// compose stop/kill, state-file truncate+restart, 3G memory-capped suite,
// missing-config errorpath). Each maps to the catalog primitive whose fault
// class the cell approximated:
//
//	chaos-disconnect — HTTP(S)_PROXY pointed at a dead port; the suite must
//	                   fail fast or hang → N-012 http-error-injection (the
//	                   userspace proxy fault class; a dead proxy is the
//	                   rate=1.0 total-failure end of the same axis).
//	chaos-shutdown   — compose stop (SIGTERM) then kill (SIGKILL) then
//	                   restart must recover → I-002 dependency-cold-return
//	                   (service dies and must come back on the same
//	                   address; mode stop/kill/fresh-empty). Tier L3: its
//	                   actuator needs the docker API, so on an
//	                   unprivileged L0 harness the cell records the named
//	                   skip (AC-11 shape) — the replacement EXISTS and is
//	                   named; the live actuator arrives with the container
//	                   backend. That is the honest state and the artefact
//	                   says so.
//	chaos-corruption — truncate a state file, restart, observe, restore →
//	                   F-009 file-replaced-under-live-writer (the
//	                   inode-identity fault class — the exact class of the
//	                   2026-08-29/30 archive hole that motivated the
//	                   cell), proven by inode identity instead of a
//	                   vacuumed rc.
//	chaos-resource   — the suite under a 3G memory cap (ulimit -v) →
//	                   R-001 memory-max-squeeze (a real cgroup cap on a
//	                   scope, with cgroup-counter proof, instead of a
//	                   shell ulimit the child could dodge).
//	chaos-errorpath  — run the binary with its config missing; clean usage
//	                   or panic → F-009 again, the unlink-then-create
//	                   mode: the file a boot path needs is GONE from under
//	                   the process. Two cells share one primitive because
//	                   the v0.1 catalog genuinely covers both cell shapes
//	                   with that one fault class (absent/replaced file
//	                   under a process); the artefact records the shared
//	                   mapping rather than inventing a fifth primitive.
var legacyCells = []LegacyCell{
	{
		Cell:      "chaos-disconnect",
		Primitive: "N-012",
		Replaces:  "dead-proxy network cut (HTTP(S)_PROXY→127.0.0.1:9) = the rate=1.0 end of N-012's userspace-proxy fault axis; the primitive adds the proxy-counter landed proof the cell never had",
	},
	{
		Cell:      "chaos-shutdown",
		Primitive: "I-002",
		Replaces:  "compose stop + kill + restart = I-002's stop/kill/fresh-empty dependency lifecycle, with the external-probe + state-diff proof; tier L3 (docker API) — on an unprivileged L0 harness this cell records the named skip until the container backend lands",
	},
	{
		Cell:      "chaos-corruption",
		Primitive: "F-009",
		Replaces:  "state-file truncate + restart + restore = F-009's replace-under-live-writer class (the 08-29/30 archive-hole shape), proven by inode identity instead of an unobserved rc",
	},
	{
		Cell:      "chaos-resource",
		Primitive: "R-001",
		Replaces:  "suite under a 3G ulimit -v cap = R-001's memory-max scope squeeze with cgroup-counter proof (the cap cannot be dodged by a child re-exec)",
	},
	{
		Cell:      "chaos-errorpath",
		Primitive: "F-009",
		Replaces:  "missing-config boot errorpath = F-009's unlink-then-create mode (the file a boot path needs is gone); shares the primitive with chaos-corruption because one fault class covers both cell shapes — recorded as the shared mapping, not a fifth primitive",
	},
}

// LegacyCells returns the five legacy bash cells and their replacing
// primitives, in bunker-qa.sh cell order (the artefact and the parity test
// consume this; the order is part of the parity contract).
func LegacyCells() []LegacyCell {
	out := make([]LegacyCell, len(legacyCells))
	copy(out, legacyCells)
	return out
}

// LegacyCellNames returns the five legacy cell names in order.
func LegacyCellNames() []string {
	out := make([]string, 0, len(legacyCells))
	for _, lc := range legacyCells {
		out = append(out, lc.Cell)
	}
	return out
}

// Target is one battery target row. At M1 every target is an L0 scratch
// target the run owns (the selftest harness's own scratch discipline); the
// Project field names the owning project for findings filing (AC-18's
// "owning project's board").
type Target struct {
	// Project is the owning project label ("scratch" for the default
	// single-project run; a fleet project name when --project names one).
	Project string
	// Name is the target label inside the project.
	Name string
	// Selector is the declared selector ("host:scratch/battery/<cell>");
	// resolved through the rails shape at run time.
	Selector string
	// Scratch must be true at M1; a non-scratch target refuses (NOT-LIST).
	Scratch bool
}

// Cell is one matrix cell: primitive × target, with the legacy parity
// annotation when the cell belongs to the quick matrix.
type Cell struct {
	// ID is the stable cell id ("q1".."q5" in the quick matrix;
	// "<project>/<primitive>" elsewhere).
	ID string
	// Primitive is the catalog primitive id.
	Primitive string
	// Target is the cell's target.
	Target Target
	// LegacyCell is the bunker-qa.sh cell this cell replaces ("" when the
	// cell is not part of the parity set).
	LegacyCell string
	// Replaces carries the mapping rationale when LegacyCell != "".
	Replaces string
}

// Matrix is the battery definition: the fault × target cross product.
type Matrix struct {
	// Quick is true when the matrix is the parity set (--quick).
	Quick bool
	// Cells is the ordered cell list (quick: bunker-qa.sh cell order;
	// otherwise sorted project, then primitive).
	Cells []Cell
}

// QuickMatrix returns the reduced parity matrix: exactly the five legacy
// bash cells, each named alongside the primitive that replaces it, each on
// the run-owned scratch target. This is what `mischief battery --quick`
// executes.
func QuickMatrix() *Matrix {
	m := &Matrix{Quick: true}
	for i, lc := range legacyCells {
		m.Cells = append(m.Cells, Cell{
			ID:         fmt.Sprintf("q%d", i+1),
			Primitive:  lc.Primitive,
			Target:     scratchTarget("scratch", lc.Cell),
			LegacyCell: lc.Cell,
			Replaces:   lc.Replaces,
		})
	}
	return m
}

// DefaultMatrix returns the full matrix: every named primitive across every
// named project (the cartesian product, sorted by project then primitive).
// primitives are catalog ids; an empty list yields an empty matrix (the
// caller validates against the catalog).
func DefaultMatrix(projects, primitives []string) *Matrix {
	projs := dedupeSorted(projects)
	prim := dedupeSorted(primitives)
	m := &Matrix{}
	for _, p := range projs {
		for _, id := range prim {
			m.Cells = append(m.Cells, Cell{
				ID:        p + "/" + id,
				Primitive: id,
				Target:    scratchTarget(p, id),
			})
		}
	}
	return m
}

// scratchTarget builds the M1 scratch target for a cell (the selftest
// harness owns the real scratch dir; this is the declared address).
func scratchTarget(project, label string) Target {
	name := "scratch-" + sanitizeLabel(label)
	return Target{
		Project:  project,
		Name:     name,
		Selector: "host:scratch/battery/" + name,
		Scratch:  true,
	}
}

// sanitizeLabel keeps a label filesystem/selector-safe.
func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "cell"
	}
	return b.String()
}

func dedupeSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Primitives returns the matrix's primitive ids in cell order (no
// duplicates).
func (m *Matrix) Primitives() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range m.Cells {
		if !seen[c.Primitive] {
			seen[c.Primitive] = true
			out = append(out, c.Primitive)
		}
	}
	return out
}
