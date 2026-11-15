package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDocumentedOperationsCommandsRun verifies the commands copied from
// docs/OPERATIONS.md against the built CLI. Keeping extraction and execution
// shared means a future flag rename cannot leave either operator example stale.
func TestDocumentedOperationsCommandsRun(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(sourceFile), "..", "..")
	docsPath := filepath.Join(root, "docs", "OPERATIONS.md")
	docs, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatalf("read %s: %v", docsPath, err)
	}

	retentionCommand, err := documentedCommand(string(docs), "retention")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(retentionCommand, "--runs-dir <run dir>") {
		t.Fatalf("documented retention command does not use --runs-dir: %q", retentionCommand)
	}
	if strings.Contains(retentionCommand, "--dir") {
		t.Fatalf("documented retention command still uses the old --dir flag: %q", retentionCommand)
	}
	statusCommand, err := documentedCommand(string(docs), "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statusCommand, "status --dir <scratch>") {
		t.Fatalf("documented restore drill does not use status --dir: %q", statusCommand)
	}
	if strings.Contains(statusCommand, "--journal-dir") {
		t.Fatalf("documented restore drill still uses the plan-only --journal-dir flag: %q", statusCommand)
	}

	binary := filepath.Join(t.TempDir(), "mischief")
	build := exec.Command("go", "build", "-o", binary, "./cmd/mischief")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build documented CLI: %v\n%s", err, output)
	}

	t.Run("retention", func(t *testing.T) {
		runsDir := filepath.Join(t.TempDir(), "runs")
		if err := os.Mkdir(runsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		runDocumentedCommand(t, binary, retentionCommand, map[string]string{"<run dir>": runsDir})
	})

	t.Run("restore drill", func(t *testing.T) {
		source := filepath.Join(t.TempDir(), "source-run")
		if err := os.Mkdir(source, 0o755); err != nil {
			t.Fatal(err)
		}
		prepareDocumentedRestoreRun(t, binary, source)
		before := snapshotTree(t, source)

		scratch := filepath.Join(t.TempDir(), "scratch-run")
		if err := copyTree(source, scratch); err != nil {
			t.Fatalf("copy source run directory: %v", err)
		}
		output := runDocumentedCommand(t, binary, statusCommand, map[string]string{"<scratch>": scratch})
		for _, field := range []string{"run_dir:", "boot_id:", "holds:", "proofs:"} {
			if !strings.Contains(output, field) {
				t.Errorf("restore-drill status output missing %q:\n%s", field, output)
			}
		}
		if after := snapshotTree(t, source); !sameTree(before, after) {
			t.Fatalf("restore-drill status changed the source run directory")
		}
	})
}

func documentedCommand(docs, verb string) (string, error) {
	marker := "`mischief " + verb + " "
	var found string
	for _, line := range strings.Split(docs, "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		start := strings.Index(line, "`")
		end := strings.Index(line[start+1:], "`")
		if start < 0 || end < 0 {
			return "", fmt.Errorf("%s documentation line has an unterminated command: %q", verb, line)
		}
		command := line[start+1 : start+1+end]
		if found != "" {
			return "", fmt.Errorf("multiple %s command examples found", verb)
		}
		found = command
	}
	if found == "" {
		return "", fmt.Errorf("no %s command example found", verb)
	}
	return found, nil
}

func runDocumentedCommand(t *testing.T, binary, command string, replacements map[string]string) string {
	t.Helper()
	for placeholder, value := range replacements {
		command = strings.Replace(command, placeholder, value, 1)
	}
	args := strings.Fields(command)
	if len(args) < 2 || args[0] != "mischief" {
		t.Fatalf("unexpected documented command shape: %q", command)
	}
	result := exec.Command(binary, args[1:]...)
	output, err := result.CombinedOutput()
	if err != nil {
		t.Fatalf("documented command failed: %v\n%s\ncommand: %s", err, output, command)
	}
	return string(output)
}

func prepareDocumentedRestoreRun(t *testing.T, binary, source string) {
	t.Helper()
	fixture := t.TempDir()
	backup := filepath.Join(fixture, "backup")
	target := filepath.Join(fixture, "target")
	if err := os.WriteFile(backup, []byte("pristine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("faulted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(fixture, "sanction-marker")
	if err := os.WriteFile(marker, []byte("reason: documented operations test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "MISCHIEF_SANCTION_FILE="+marker, "MISCHIEF_SANCTION=")
	invoke := func(args ...string) string {
		cmd := exec.Command(binary, args...)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("prepare restore run command %q: %v\n%s", args, err, output)
		}
		return string(output)
	}
	arm := invoke("arm", "--fault", "S-001", "--target", "scratch:"+target, "--ttl", "10s",
		"--inverse-kind", "restore-file", "--inverse-params", "path="+target+",backup="+backup,
		"--check-kind", "file-matches-backup", "--check-params", "path="+target+",backup="+backup,
		"--dir", source)
	fields := strings.Fields(arm)
	if len(fields) < 2 || fields[0] != "armed" {
		t.Fatalf("arm output does not name a hold id: %q", arm)
	}
	holdID := fields[1]
	invoke("land", "--hold", holdID, "--dir", source)
	invoke("revert", "--hold", holdID, "--dir", source)
}

func copyTree(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

type treeSnapshot map[string][]byte

func snapshotTree(t *testing.T, root string) treeSnapshot {
	t.Helper()
	snapshot := treeSnapshot{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[rel] = data
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snapshot
}

func sameTree(left, right treeSnapshot) bool {
	if len(left) != len(right) {
		return false
	}
	for path, want := range left {
		if !bytes.Equal(want, right[path]) {
			return false
		}
	}
	return true
}
