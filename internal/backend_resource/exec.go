// Command execution (os/exec over the host seam). Kept in its own file so
// the primitive files read purely as fault logic. The exec helpers never
// set a working directory: the callers' contract (mischief's rails) is that
// scratch stays under the session bus and t.TempDir() scratch.
package backend_resource

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// execTimeout bounds every subprocess this package spawns. Long enough for
// systemd-run round trips; short enough that a hung helper cannot wedge a
// run.
const execTimeout = 30 * time.Second

// execLookPath wraps exec.LookPath for the host seam.
func execLookPath(name string) (string, error) { return exec.LookPath(name) }

// execRun runs argv and returns combined output. Errors carry the argv.
func execRun(argv []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 — fixed argv, no shell
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return out.String(), newHostErr(argv, err)
	}
	return out.String(), nil
}
