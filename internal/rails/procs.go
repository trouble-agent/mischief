package rails

import "os"

// readProcFile reads a small virtual file wholesale. It is a package-level
// function so the `readFile` var seam above it can be pointed at a stub in
// tests; production callers get the real read.
func readProcFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) evidence=internal/rails/procs.go witness=none:no-live-host-run-in-worktree
