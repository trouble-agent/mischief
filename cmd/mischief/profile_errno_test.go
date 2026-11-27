package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/trouble-agent/mischief/internal/sanction"
)

func TestProfileErrnoCLI(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses EACCES")
	}
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("cc unavailable")
	}
	t.Setenv(sanction.EnvSanction, "1")
	writeHealthyLoad(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "victim")
	cmd := exec.Command("cc", "-O2", "-DSWALLOW", "-o", bin, filepath.Join("..", "..", "internal", "profile", "testdata", "errno-victim.c"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cc: %v: %s", err, out)
	}
	code, stdout := captureStdout(t, func() int {
		return run([]string{"profile", "run", "linux/error-handling", "--target", bin, "--json", "--dir", filepath.Join(dir, "runs")})
	})
	if code != 1 {
		t.Fatalf("exit=%d stdout=%s", code, stdout)
	}
	var f struct {
		Schema  string            `json:"schema"`
		Profile string            `json:"profile"`
		Outcome string            `json:"outcome"`
		Cause   string            `json:"cause_class"`
		Steps   []json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal([]byte(stdout), &f); err != nil {
		t.Fatalf("not exactly one JSON document: %v", err)
	}
	if f.Schema != "mischief.finding/v1" || f.Profile != "linux/error-handling" || f.Outcome != "failed" || f.Cause != "error-handling:errno-swallowed" || len(f.Steps) != 5 {
		t.Fatalf("finding=%+v", f)
	}
	// The schema validator in the project's existing profile tests implements
	// the schema vocabulary; run that gate separately in the package suite.
	schema := filepath.Join("..", "..", "catalog", "profiles", "finding-schema.v1.json")
	validator := exec.Command("python3", "-c", `import json,sys; import jsonschema; jsonschema.Draft7Validator(json.load(open(sys.argv[1]))).validate(json.load(sys.stdin))`, schema)
	validator.Stdin = bytes.NewBufferString(stdout)
	if out, err := validator.CombinedOutput(); err != nil {
		t.Fatalf("schema validation: %v: %s", err, out)
	}
}
