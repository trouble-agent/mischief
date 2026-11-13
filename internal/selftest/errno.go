package selftest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/trouble-agent/mischief/internal/backend_signal"
)

// errnoLandable covers the five Linux CLI error shapes with a real child
// process. The parent is the external witness: it reads the child's measured
// output and the target filesystem after the child exits. Shim-backed shapes
// use the same generated LD_PRELOAD actuator as S-001; pipe and stdin shapes
// use the kernel's own pipe/read semantics.
type errnoLandable struct {
	id       string
	mode     string
	errno    string
	dir      string
	target   string
	victim   string
	env      []string
	arm      *backend_signal.ShimArm
	stdin    string
	baseline []byte
}

func (e *errnoLandable) capability() (string, bool) {
	if _, err := exec.LookPath("cc"); err != nil {
		return "no C compiler (cc) on PATH: errno child witness cannot be built", true
	}
	if e.id == "S-023" && os.Getuid() == 0 {
		return "root can bypass mode-based EACCES, so the unreadable-path witness is unavailable", true
	}
	return "", false
}

func (e *errnoLandable) prepare() ([]byte, error) {
	e.target = filepath.Join(e.dir, "errno-target.tmp")
	e.baseline = []byte("baseline\n")
	if err := os.WriteFile(e.target, e.baseline, 0o600); err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	pre, err := snapshotFiles(e.dir, filepath.Base(e.target))
	if err != nil {
		return nil, err
	}
	if e.id == "S-023" {
		if err := os.Chmod(e.target, 0); err != nil {
			return nil, fmt.Errorf("make target unreadable: %w", err)
		}
	}
	if e.errno != "" {
		rule, err := backend_signal.ParseRule("write:" + e.target + ":" + e.errno)
		if err != nil {
			return nil, fmt.Errorf("rule: %w", err)
		}
		if e.id == "S-020" {
			rule, err = rule.WithBudget(3)
			if err != nil {
				return nil, fmt.Errorf("rule budget: %w", err)
			}
		}
		e.arm, e.env, err = backend_signal.ArmShim(e.id, rule, e.dir)
		if err != nil {
			return nil, fmt.Errorf("arm: %w", err)
		}
	}
	src := filepath.Join(e.dir, "errno-victim.c")
	if err := os.WriteFile(src, errnoVictimSource(e.target), 0o600); err != nil {
		return nil, fmt.Errorf("victim source: %w", err)
	}
	e.victim = filepath.Join(e.dir, "errno-victim")
	if out, err := exec.Command("cc", "-O2", "-o", e.victim, src).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("victim build: %w: %s", err, trimOut(out))
	}
	return pre, nil
}

func (e *errnoLandable) land() landOutcome {
	args := []string{e.target, e.mode}
	cmd := exec.Command(e.victim, args...)
	if e.id == "S-024" {
		e.stdin = "partial!"
		cmd.Stdin = strings.NewReader(e.stdin)
	}
	if e.errno != "" {
		cmd.Env = append(os.Environ(), e.env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return bad("child: %v: %s", err, trimOut(out))
	}
	text := string(out)
	switch e.id {
	case "S-020":
		if !strings.Contains(text, "delivered=6") {
			return bad("external child witness missing delivered-return count: %s", trimOut(out))
		}
		b, readErr := os.ReadFile(e.target)
		if readErr != nil || string(b) != "abcdef" {
			return bad("external target witness: got %q, err %v", b, readErr)
		}
		return ok("child witness: delivered=6 after EINTR retries; target contains 6 bytes")
	case "S-021":
		if !strings.Contains(text, "epipe=1") {
			return bad("external pipe witness missing EPIPE: %s", trimOut(out))
		}
		return ok("external pipe witness: reader closed and writer observed EPIPE")
	case "S-022":
		if !strings.Contains(text, "errno=No space left on device") {
			return bad("external child witness missing ENOSPC: %s", trimOut(out))
		}
		b, readErr := os.ReadFile(e.target)
		var st syscall.Statfs_t
		statErr := syscall.Statfs(e.dir, &st)
		if readErr != nil || len(b) != 0 || statErr != nil || st.Bavail == 0 {
			return bad("external ENOSPC witness: target=%d bytes, free blocks=%d, read=%v, statfs=%v", len(b), st.Bavail, readErr, statErr)
		}
		return ok("external ENOSPC witness: write returned ENOSPC; target empty; statfs reports free blocks")
	case "S-023":
		if !strings.Contains(text, "errno=Permission denied") {
			return bad("external child witness missing EACCES: %s", trimOut(out))
		}
		st, statErr := os.Stat(e.target)
		if statErr != nil || st.Mode().Perm() != 0 {
			return bad("external mode witness: mode=%o, err=%v, want 000", st.Mode().Perm(), statErr)
		}
		return ok("external EACCES witness: child rejected write while stat mode was 000")
	case "S-024":
		if !strings.Contains(text, "partial_read=8") {
			return bad("external stdin witness missing short read: %s", trimOut(out))
		}
		return ok("external stdin witness: delivered 8 bytes, then EOF")
	default:
		return bad("unknown errno mode %q", e.mode)
	}
}

func (e *errnoLandable) inverse() landOutcome {
	if e.id == "S-023" {
		if err := os.Chmod(e.target, 0o600); err != nil {
			return bad("restore target mode: %v", err)
		}
	}
	mode := e.mode
	input := ""
	switch e.id {
	case "S-021":
		mode = "epipe-control"
	case "S-024":
		mode = "partialstdin-control"
		input = "complete-input!"
	}
	cmd := exec.Command(e.victim, e.target, mode)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return bad("control child: %v: %s", err, trimOut(out))
	}
	text := string(out)
	if e.id == "S-021" && !strings.Contains(text, "epipe_control=1") {
		return bad("control pipe witness missing successful write: %s", trimOut(out))
	}
	if e.id == "S-024" && !strings.Contains(text, "full_read=15") {
		return bad("control stdin witness missing complete read: %s", trimOut(out))
	}
	if e.id != "S-021" && e.id != "S-024" {
		b, readErr := os.ReadFile(e.target)
		if readErr != nil || len(b) == 0 {
			return bad("control write did not restore target: bytes=%d err=%v", len(b), readErr)
		}
		if err := os.WriteFile(e.target, e.baseline, 0o600); err != nil {
			return bad("restore pre-state: %v", err)
		}
	}
	return ok("inverse witness: fault removed; control path completed and pre-state restored")
}

func (e *errnoLandable) post() ([]byte, error) {
	return snapshotFiles(e.dir, filepath.Base(e.target))
}

func (e *errnoLandable) cleanup() {}

func errnoVictimSource(target string) []byte {
	s := `#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char **argv) {
  const char *path = argc > 1 ? argv[1] : "TARGET";
  const char *mode = argc > 2 ? argv[2] : "";
  if (!strcmp(mode, "epipe") || !strcmp(mode, "epipe-control")) {
    int p[2];
    if (pipe(p) != 0) return 2;
    signal(SIGPIPE, SIG_IGN);
    if (!strcmp(mode, "epipe")) {
      close(p[0]);
      ssize_t n = write(p[1], "x", 1);
      printf("epipe=%d errno=%s\n", n < 0 && errno == EPIPE, strerror(errno));
    } else {
      ssize_t n = write(p[1], "x", 1);
      printf("epipe_control=%d\n", n == 1);
      close(p[0]);
    }
    close(p[1]);
    return 0;
  }
  if (!strcmp(mode, "partialstdin") || !strcmp(mode, "partialstdin-control")) {
    char buf[64];
    ssize_t n = read(STDIN_FILENO, buf, sizeof(buf));
    if (n < 0) return 3;
    if (!strcmp(mode, "partialstdin")) printf("partial_read=%zd expected=14\n", n);
    else printf("full_read=%zd\n", n);
    return 0;
  }
  int f = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
  if (f < 0) { printf("open errno=%s\n", strerror(errno)); return 0; }
  const char *msg = !strcmp(mode, "eintr") ? "abcdef" : "faulted";
  size_t want = strlen(msg);
  size_t done = 0;
  int attempts = 0;
  while (done < want) {
    ssize_t n = write(f, msg + done, want - done);
    attempts++;
    if (n < 0 && errno == EINTR) continue;
    if (n < 0) { printf("errno=%s\n", strerror(errno)); close(f); return 0; }
    done += (size_t)n;
  }
  if (!strcmp(mode, "eintr")) printf("delivered=%zu attempts=%d\n", done, attempts);
  else printf("errno=none bytes=%zu\n", done);
  close(f);
  return 0;
}
`
	return []byte(strings.ReplaceAll(s, "TARGET", target))
}
