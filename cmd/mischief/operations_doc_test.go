package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDocumentedRetentionCommandRuns verifies the command copied from
// docs/OPERATIONS.md against the built CLI. Keeping the extraction here means
// a future flag rename cannot leave the operator example silently stale.
func TestDocumentedRetentionCommandRuns(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	docsPath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "docs", "OPERATIONS.md")
	docs, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatalf("read %s: %v", docsPath, err)
	}

	command, err := documentedRetentionCommand(string(docs))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(command, "--runs-dir <run dir>") {
		t.Fatalf("documented retention command does not use --runs-dir: %q", command)
	}
	if strings.Contains(command, "--dir") {
		t.Fatalf("documented retention command still uses the old --dir flag: %q", command)
	}

	binary := filepath.Join(t.TempDir(), "mischief")
	build := exec.Command("go", "build", "-o", binary, "./cmd/mischief")
	build.Dir = filepath.Join(filepath.Dir(sourceFile), "..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build documented CLI: %v\n%s", err, output)
	}

	runsDir := filepath.Join(t.TempDir(), "runs")
	if err := os.Mkdir(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	command = strings.Replace(command, "<run dir>", runsDir, 1)
	args := strings.Fields(command)
	if len(args) < 3 || args[0] != "mischief" {
		t.Fatalf("unexpected documented command shape: %q", command)
	}
	result := exec.Command(binary, args[1:]...)
	output, err := result.CombinedOutput()
	if err != nil {
		t.Fatalf("documented retention command failed: %v\n%s\ncommand: %s", err, output, command)
	}

	oldFlag := exec.Command(binary, "retention", "--dir", runsDir)
	if output, err := oldFlag.CombinedOutput(); err == nil {
		t.Fatalf("old --dir form unexpectedly succeeded:\n%s", output)
	}
}

func documentedRetentionCommand(docs string) (string, error) {
	const marker = "`mischief retention "
	var found string
	for _, line := range strings.Split(docs, "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		start := strings.Index(line, "`")
		end := strings.Index(line[start+1:], "`")
		if start < 0 || end < 0 {
			return "", fmt.Errorf("retention documentation line has an unterminated command: %q", line)
		}
		command := line[start+1 : start+1+end]
		if found != "" {
			return "", fmt.Errorf("multiple retention command examples found")
		}
		found = command
	}
	if found == "" {
		return "", fmt.Errorf("no retention command example found")
	}
	return found, nil
}
