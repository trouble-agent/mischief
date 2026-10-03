package reverter

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain is the child-process intercept for the AC-5 battery: the kill -9
// test spawns THIS binary as the "CLI" (argv[1]=__cli), and the CLI's Detach
// spawns THIS binary again as the detached owner (argv[1]=__owner). A test
// binary would otherwise try to run tests, so both forms are intercepted
// here and EXIT without returning to the test runner.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 {
		switch os.Args[1] {
		case "__owner":
			if err := RunOwner(os.Args[2:]); err != nil {
				os.Stderr.WriteString("owner: " + err.Error() + "\n")
				os.Exit(1)
			}
			os.Exit(0)
		case "__cli":
			cliChildMain(os.Args[2:])
			os.Exit(0) // unreachable: cliChildMain blocks until killed
		}
	}
	os.Exit(m.Run())
}

// cliChildMain is the simulated CLI process of the AC-5 test: arm, land
// (corrupt the target file), detach the owner, then block forever — the
// parent kill -9s it mid-hold. It writes its pid to <dir>/cli.pid only
// AFTER the owner spawn succeeded, so the parent never kills before the
// owner exists.
func cliChildMain(args []string) {
	var dir, file, backup string
	var ttlMs int
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-dir":
			i++
			dir = args[i]
		case "-file":
			i++
			file = args[i]
		case "-backup":
			i++
			backup = args[i]
		case "-ttl-ms":
			i++
			fmt.Sscanf(args[i], "%d", &ttlMs)
		}
	}
	st, err := Open(dir)
	if err != nil {
		os.Stderr.WriteString("cli: open: " + err.Error() + "\n")
		os.Exit(2)
	}
	defer st.Close()
	lf := New(st)
	hold := Hold{
		FaultID: "P-CLI-HOLD",
		Target:  "file:" + file,
		Inverse: Decl{Kind: "restore-file", Params: map[string]string{"path": file, "backup": backup}},
		Check:   Decl{Kind: "file-matches-backup", Params: map[string]string{"path": file, "backup": backup}},
		TTL:     time.Duration(ttlMs) * time.Millisecond,
	}
	id, err := lf.Arm(hold)
	if err != nil {
		os.Stderr.WriteString("cli: arm: " + err.Error() + "\n")
		os.Exit(2)
	}
	// Land the fault: corrupt the target file AFTER the write-ahead arm.
	if err := os.WriteFile(file, []byte("FAULTED-CONTENT\n"), 0o644); err != nil {
		os.Stderr.WriteString("cli: land: " + err.Error() + "\n")
		os.Exit(2)
	}
	if err := lf.Land(id); err != nil {
		os.Stderr.WriteString("cli: land-refused: " + err.Error() + "\n")
		os.Exit(2)
	}
	if _, err := lf.Detach(id, hold.TTL); err != nil {
		os.Stderr.WriteString("cli: detach: " + err.Error() + "\n")
		os.Exit(3)
	}
	// Owner is out. Publish the pid and hold the line until SIGKILL.
	if err := os.WriteFile(filepath.Join(dir, "cli.pid"), []byte(fmt.Sprintf("%d", os.Getpid())), 0o644); err != nil {
		os.Stderr.WriteString("cli: pidfile: " + err.Error() + "\n")
		os.Exit(2)
	}
	select {} // killed -9 by the parent mid-hold
}

// journalLines reads the journal file back as raw lines (the evidence the
// byte-level tests assert on). A not-yet-created journal reads as empty —
// poll loops run before the first writer creates the file; the strict
// count assertions fail naturally on empty.
func journalLines(t *testing.T, dir string) []string {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, journalFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("open journal: %v", err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan journal: %v", err)
	}
	return lines
}

// recordTypeOf extracts the "type" value from one raw journal line (string
// surgery, not the struct — the byte-level tests must see the BYTES).
func recordTypeOf(t *testing.T, line string) string {
	t.Helper()
	const key = `"type":"`
	i := strings.Index(line, key)
	if i < 0 {
		t.Fatalf("line has no type field: %s", line)
	}
	rest := line[i+len(key):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatalf("line type field unterminated: %s", line)
	}
	return rest[:j]
}

// scratchHold builds a hold over a real scratch file pair (backup B with
// good content, live file F corrupted by "landing") — no network, no root,
// the measured check is a real cmp of real bytes.
func scratchHold(t *testing.T, faultID string) (Hold, string, string) {
	t.Helper()
	dir := localTempDir(t)
	backup := filepath.Join(dir, faultID+".backup")
	file := filepath.Join(dir, faultID+".live")
	if err := os.WriteFile(backup, []byte("GOOD-CONTENT\n"), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	if err := os.WriteFile(file, []byte("FAULTED-CONTENT\n"), 0o644); err != nil {
		t.Fatalf("write live: %v", err)
	}
	return Hold{
		FaultID: faultID,
		Target:  "file:" + file,
		Inverse: Decl{Kind: "restore-file", Params: map[string]string{"path": file, "backup": backup}},
		Check:   Decl{Kind: "file-matches-backup", Params: map[string]string{"path": file, "backup": backup}},
		TTL:     30 * time.Second,
	}, file, backup
}

// localTempDir is the tests' scratch root: LOCAL-disk /tmp, not t.TempDir().
// Reason: t.TempDir() roots at go test's -test.tmpdir flag default, which on
// this host resolves to a bulk/network mount whose fsync measured ~1.1s per
// call — every timing contract in this suite (the AC-6 2s kill-switch wall,
// the measured-revert durations, the child CLI's arm-land-detach sequence)
// would measure the MOUNT, not the reverter. Cleanup is registered.
func localTempDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "mischief-reverter-test-")
	if err != nil {
		t.Fatalf("local temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// newTestStore opens a store over a fresh local-disk temp dir (isolated
// state, no ambient writes — the default dir is never touched by tests).
func newTestStore(t *testing.T) (*Store, *Lifecycle) {
	t.Helper()
	st, err := Open(localTempDir(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, New(st)
}
