package reverter

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
)

// scratchExec is the inverse/check executor: the declarative kinds are
// implemented NATIVELY (direct file operations, no shell fork). A shell
// layer would make every measured revert a fork+exec pair — seconds under
// host load, fatal to the 2s kill-switch budget (AC-6) — and would parse
// caller paths as shell text (a scratch dir with parentheses is a syntax
// error). Native kinds are deterministic, load-immune, and injection-free;
// an UNKNOWN kind still fails closed.
type scratchExec struct{}

// execAction executes one inverse action kind. It returns the measured
// action detail and the error.
func (scratchExec) execAction(kind string, d Decl) (string, error) {
	switch kind {
	case "restore-file":
		b, err := os.ReadFile(d.Params["backup"])
		if err != nil {
			return "", fmt.Errorf("read backup: %w", err)
		}
		if err := os.WriteFile(d.Params["path"], b, 0o644); err != nil {
			return "", fmt.Errorf("write path: %w", err)
		}
		return "restored", nil
	case "remove-file":
		if err := os.Remove(d.Params["path"]); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("remove: %w", err)
		}
		return "removed", nil
	default:
		// fail closed: an unknown action must not masquerade as success.
		return "", fmt.Errorf("inverse kind %q: unknown (refusing to execute)", kind)
	}
}

// evalCheck evaluates one measured check kind and returns the measured
// detail and the check's pass. An unknown kind FAILS (fail closed: a revert
// that cannot be measured is not a revert, AC-4).
func (scratchExec) evalCheck(kind string, d Decl) (string, bool, error) {
	switch kind {
	case "file-matches-backup":
		b1, err1 := os.ReadFile(d.Params["path"])
		b2, err2 := os.ReadFile(d.Params["backup"])
		if err1 != nil || err2 != nil {
			// a check that cannot read both sides MEASURES "unreadable" —
			// that is a failed measurement, not a pass.
			return "unreadable", false, nil
		}
		if bytes.Equal(b1, b2) {
			return "same", true, nil
		}
		return "differ", false, nil
	case "file-absent":
		_, err := os.Stat(d.Params["path"])
		switch {
		case err == nil:
			return "present", false, nil
		case os.IsNotExist(err):
			return "absent", true, nil
		default:
			return "stat error", false, nil
		}
	default:
		return "", false, fmt.Errorf("check kind %q: unknown (failing closed)", kind)
	}
}

// homeDir reports the reverter's home (HOME, falling back to the uid's
// passwd entry) — the default journal dir lives under it.
func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return "/"
}
