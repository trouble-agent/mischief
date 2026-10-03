package backend_signal

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// procState reads /proc/<pid>/stat and returns field 3 — the state letter
// (R running, S sleeping, D disk wait, T stopped, Z zombie, X dead). The
// comm field (field 2) may contain spaces and parentheses, so parsing skips
// everything up to the LAST ')'.
func procState(pid int) (byte, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 || i+2 >= len(s) {
		return 0, fmt.Errorf("proc: /proc/%d/stat: unparsable", pid)
	}
	return s[i+2], nil
}

// procLimits is the slice of /proc/<pid>/limits this package reads back.
type procLimits struct {
	NoFileSoft, NoFileHard uint64
	NProcSoft, NProcHard   uint64
}

// readProcLimits parses /proc/<pid>/limits: each row is
// "<name words...>  <soft>  <hard>  <unit>"; soft/hard are decimal or the
// token "unlimited" (RLIM_INFINITY, rendered as Unlimited). Unknown rows
// are skipped, not errors.
func readProcLimits(pid int) (procLimits, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/limits", pid))
	if err != nil {
		return procLimits{}, err
	}
	var out procLimits
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		soft, ok := parseLimit(f[len(f)-3])
		hard, ok2 := parseLimit(f[len(f)-2])
		if !ok || !ok2 {
			continue
		}
		name := strings.Join(f[:len(f)-3], " ")
		switch name {
		case "Max open files":
			out.NoFileSoft, out.NoFileHard = soft, hard
		case "Max processes":
			out.NProcSoft, out.NProcHard = soft, hard
		}
	}
	return out, nil
}

// limitsToRlimit picks one resource's pair out of a parsed limits file.
func limitsToRlimit(pl procLimits, res Resource) Rlimit {
	switch res {
	case ResNoFile:
		return Rlimit{Cur: pl.NoFileSoft, Max: pl.NoFileHard}
	case ResNProc:
		return Rlimit{Cur: pl.NProcSoft, Max: pl.NProcHard}
	}
	return Rlimit{}
}

func parseLimit(tok string) (uint64, bool) {
	if tok == "unlimited" {
		return Unlimited, true
	}
	v, err := strconv.ParseUint(tok, 10, 64)
	return v, err == nil
}

// parseHits parses the hits counter of a proof line.
func parseHits(s string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("hits counter %q is not an integer", s)
	}
	if v < 0 {
		return 0, fmt.Errorf("hits counter %q is negative", s)
	}
	return v, nil
}

// waitFor polls cond until it has held true for minHits consecutive samples
// or the window expires. It reports whether the condition held; a false
// return after a full window is the no_op arm.
func waitFor(window, interval time.Duration, minHits int, cond func() bool) bool {
	if minHits < 1 {
		minHits = 1
	}
	hits := 0
	for start := time.Now(); time.Since(start) < window; {
		if cond() {
			hits++
			if hits >= minHits {
				return true
			}
		} else {
			hits = 0
		}
		time.Sleep(interval)
	}
	return false
}

// Unlimited is RLIM_INFINITY as /proc/<pid>/limits renders it ("unlimited").
const Unlimited = math.MaxUint64
