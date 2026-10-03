package rails

// Verdict vocabulary values the rails speak (PRD §6.4). The full engine
// vocabulary belongs to SPEC-05; the rails only ever grade one value, and
// they name it here so the refusal type does not carry a bare string
// literal.
const (
	// VerdictAborted is the verdict a rail refusal grades ("rails/TTL/blast
	// refusal").
	VerdictAborted = "aborted"
)

// readFile is the procfs read seam: package-level so tests can neutralize
// the filesystem without a build tag (the ambient-filesystem discipline the
// fleet applies to tests: a test must never depend on, or perturb, the real
// /proc numbers of the box it runs on).
var readFile = readProcFile

// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) evidence=internal/rails/verdict.go witness=none:no-live-host-run-in-worktree
