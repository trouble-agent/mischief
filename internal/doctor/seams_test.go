package doctor

import (
	"errors"
	"os"
	"testing"

	"github.com/trouble-agent/mischief/internal/rails"
)

// Test-only seams for the rails self-checks: pin the sanction and load
// seams, restoring the production values on cleanup. The host's real
// sanction state and real /proc numbers are never read by these tests;
// CheckLoad stays REAL on purpose — a pinned reading (healthy or breaching)
// is evaluated by the actual gate, so the breach arm exercises the true
// threshold logic, not a stub's.

// railsReading is the test's compact reading shape.
type railsReading struct {
	load1 float64
	load5 float64
	psi   float64
}

// errSanctionRefusal stands in for the sanction gate's refusal (the real
// Error renders host + missing halves verbatim; the seam carries the
// message so the naming assertion tests the report, not the gate).
var errSanctionRefusal = errors.New(`host "karaHermes" is not sanctioned for fault injection: sanction marker file "/etc/mischief/sanctioned" absent or unreadable; env MISCHIEF_SANCTION_HOST does not name this host (set it to the hostname to sanction) — refusing fail-closed (SPEC-13/MSF-020: mischief runs only on an ephemeral sanctioned host, never the fleet's main host)`)

// pinSanction pins the SanctionCheck seam to return err (nil = admit) and
// returns the restore func.
func pinSanction(t *testing.T, err error) func() {
	t.Helper()
	old := SanctionCheck
	SanctionCheck = func() error { return err }
	return func() { SanctionCheck = old }
}

// pinLoad pins the ReadLoad seam to the given reading and returns the
// restore func. CheckLoad is left real — the gate itself grades the pinned
// reading.
func pinLoad(t *testing.T, r railsReading) func() {
	t.Helper()
	old := ReadLoad
	ReadLoad = func() (rails.LoadReading, error) {
		return rails.LoadReading{Load1: r.load1, Load5: r.load5, PSIIoSomeAvg10: r.psi}, nil
	}
	return func() { ReadLoad = old }
}

// osWriteFile is the test file writer (named so the fixtures helper stays
// one indirection away from the ambient os use).
func osWriteFile(path string, body []byte) error {
	return os.WriteFile(path, body, 0o644)
}
