package profile

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/trouble-agent/mischief/internal/backend_signal"
)

// Errno exercises a cooperative child executable supplied as a scratch target.
// The child protocol is: executable <scenario-file> <mode>; stdout reports
// fault=1 and handled=0|1. The parent checks the resulting file independently.
type Errno struct{ ID string }

func (e *Errno) Apply(ctx context.Context, req LandRequest) LandResult {
	if req.Target.Kind != "scratch" || req.Target.Path == "" {
		return LandResult{Reason: "errno requires a scratch selector naming a controlled executable"}
	}
	resolved, err := filepath.EvalSymlinks(req.Target.Path)
	if err != nil || !underScratchBase(resolved) {
		return LandResult{Reason: "controlled executable must resolve within scratch"}
	}
	req.Target.Path = resolved
	if _, err := exec.LookPath("cc"); err != nil {
		return LandResult{Reason: "no C compiler (cc) on PATH: errno shim cannot be built"}
	}
	if e.ID == "S-023" && os.Geteuid() == 0 {
		return LandResult{Reason: "root can bypass mode-based EACCES"}
	}
	dir := filepath.Join(req.Dir, "errno-"+e.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return LandResult{Reason: err.Error()}
	}
	path := filepath.Join(dir, "target")
	if err := os.WriteFile(path, []byte("baseline"), 0600); err != nil {
		return LandResult{Reason: err.Error()}
	}
	modes := map[string]string{"S-020": "eintr", "S-021": "epipe", "S-022": "enospc", "S-023": "eacces", "S-024": "partialstdin"}
	mode, ok := modes[e.ID]
	if !ok {
		return LandResult{Reason: "unknown errno primitive"}
	}
	var env []string
	if e.ID == "S-020" || e.ID == "S-022" {
		errno := "EINTR"
		if e.ID == "S-022" {
			errno = "ENOSPC"
		}
		rule, err := backend_signal.ParseRule("write:" + path + ":" + errno)
		if err != nil {
			return LandResult{Reason: err.Error()}
		}
		if e.ID == "S-020" {
			rule, err = rule.WithBudget(3)
			if err != nil {
				return LandResult{Reason: err.Error()}
			}
		}
		_, env, err = backend_signal.ArmShim(e.ID, rule, dir)
		if err != nil {
			return LandResult{Reason: err.Error()}
		}
	}
	if e.ID == "S-023" {
		if err := os.Chmod(path, 0); err != nil {
			return LandResult{Reason: err.Error()}
		}
		defer os.Chmod(path, 0600)
	}
	cmd := exec.CommandContext(ctx, req.Target.Path, path, mode)
	cmd.Env = append(os.Environ(), env...)
	if e.ID == "S-024" {
		cmd.Stdin = strings.NewReader("partial")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return LandResult{Reason: fmt.Sprintf("controlled child: %v: %s", err, out)}
	}
	fields := map[string]string{}
	for _, f := range strings.Fields(string(out)) {
		k, v, ok := strings.Cut(f, "=")
		if ok {
			fields[k] = v
		}
	}
	if fields["fault"] != "1" || (fields["handled"] != "0" && fields["handled"] != "1") {
		return LandResult{Reason: "child did not report the expected fault and handling status"}
	}
	b, err := os.ReadFile(path)
	if e.ID == "S-023" {
		if chmodErr := os.Chmod(path, 0600); chmodErr != nil {
			return LandResult{Reason: chmodErr.Error()}
		}
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return LandResult{Reason: err.Error()}
	}
	// Cross-check the syscall observation with state measured outside the child.
	switch e.ID {
	case "S-020":
		attempts, err := strconv.Atoi(fields["attempts"])
		if err != nil || (fields["handled"] == "1" && (attempts < 4 || string(b) != "abcdef")) || (fields["handled"] == "0" && (attempts != 1 || len(b) != 0)) {
			return LandResult{Reason: "EINTR child report contradicts attempts or target contents"}
		}
	case "S-022":
		if len(b) != 0 {
			return LandResult{Reason: "ENOSPC target was not empty"}
		}
	case "S-023":
		if string(b) != "baseline" {
			return LandResult{Reason: "EACCES target changed"}
		}
	case "S-024":
		if fields["bytes"] != "7" || fields["eof"] != "1" || (fields["handled"] == "1" && string(b) != "partial") || (fields["handled"] == "0" && len(b) != 0) {
			return LandResult{Reason: "short stdin report contradicts the post-exit file"}
		}
	}
	evidence := fmt.Sprintf("child: fault=1 handled=%s; parent read target bytes=%d", fields["handled"], len(b))
	return LandResult{Landed: true, Witness: &Witness{Kind: "external", Source: "child syscall report and parent post-exit read", Evidence: evidence}, Obs: Observation{ErrnoObserved: true, ErrnoHandled: fields["handled"] == "1", Note: evidence}}
}

func (e *Errno) Detach(ctx context.Context, req LandRequest) DetachResult {
	dir := filepath.Join(req.Dir, "errno-"+e.ID)
	path := filepath.Join(dir, "inverse")
	cmd := exec.CommandContext(ctx, req.Target.Path, path, "control")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return DetachResult{Reason: fmt.Sprintf("control child: %v: %s", err, out)}
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "abcdef" {
		return DetachResult{Reason: fmt.Sprintf("control file did not contain expected bytes: %v", err)}
	}
	return DetachResult{Witness: &Witness{Kind: "external", Source: "parent post-exit control read", Evidence: "unarmed control wrote all 6 bytes"}}
}
