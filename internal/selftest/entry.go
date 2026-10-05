package selftest

// entry.go — the verb-level entry points the CLI consumes.

import (
	"sort"
)

// RunOnePublic is the exported single-primitive run (the CLI's
// --primitive path).
func RunOnePublic(id string) Result { return newRunner().RunOne(id) }

// RunAllPublic is the exported all-primitives run (the CLI's --all path).
func RunAllPublic() []Result { return newRunner().RunAll() }

// Print renders the result lines (sorted by id).
func Print(res []Result) string {
	sorted := make([]Result, len(res))
	copy(sorted, res)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	out := ""
	for _, r := range sorted {
		out += r.String() + "\n"
	}
	return out
}
