package profile

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/trouble-agent/mischief/internal/catalog"
)

func errnoVictim(t *testing.T, swallow bool) string {
	t.Helper()
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("cc unavailable")
	}
	bin := filepath.Join(t.TempDir(), "victim")
	args := []string{"-O2", "-o", bin, filepath.Join("testdata", "errno-victim.c")}
	if swallow {
		args = append(args, "-DSWALLOW")
	}
	if out, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("cc: %v: %s", err, out)
	}
	return bin
}

func TestErrnoLandsExternal(t *testing.T) {
	bin := errnoVictim(t, false)
	for _, id := range []string{"S-020", "S-021", "S-022", "S-023", "S-024"} {
		t.Run(id, func(t *testing.T) {
			if id == "S-023" && os.Geteuid() == 0 {
				t.Skip("root bypasses EACCES")
			}
			req := LandRequest{Target: Target{Kind: "scratch", Path: bin}, Dir: t.TempDir()}
			act := &Errno{ID: id}
			r := act.Apply(context.Background(), req)
			if !r.Landed || r.Witness == nil || r.Witness.Kind != "external" || !r.Obs.ErrnoHandled {
				t.Fatalf("result: %+v", r)
			}
			if d := act.Detach(context.Background(), req); d.Witness == nil {
				t.Fatalf("detach: %+v", d)
			}
		})
	}
}

func TestErrnoExecutorAndDeterminism(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("EACCES cannot land as root")
	}
	p, raw, err := Load(filepath.Join("..", "..", "catalog", "profiles"), "linux", "error-handling")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.LoadDir(filepath.Join("..", "..", "catalog", "faults"))
	if err != nil {
		t.Fatal(err)
	}
	for _, swallow := range []bool{false, true} {
		t.Run(map[bool]string{false: "handles", true: "swallows"}[swallow], func(t *testing.T) {
			bin := errnoVictim(t, swallow)
			var journal []byte
			for i := 0; i < 2; i++ {
				dir := t.TempDir()
				f, err := Run(context.Background(), Options{Profile: p, Raw: raw, Seed: 7, Target: Target{Kind: "scratch", Path: bin, Selector: "scratch:" + bin}, Catalog: cat, RunDir: dir})
				if err != nil {
					t.Fatal(err)
				}
				want := OutcomePassed
				if swallow {
					want = OutcomeFailed
				}
				if f.Outcome != want {
					t.Fatalf("outcome=%s steps=%+v", f.Outcome, f.Steps)
				}
				if swallow && f.CauseClass != "error-handling:errno-swallowed" {
					t.Fatalf("cause=%q", f.CauseClass)
				}
				b, err := os.ReadFile(filepath.Join(dir, "profile.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				if i == 1 && !bytes.Equal(b, journal) {
					t.Fatalf("journals differ\n%s\n%s", journal, b)
				}
				journal = b
			}
		})
	}
}

// TestUnderScratchBaseAcceptsGoTmpDir pins the scratch rail's env-redirect arm:
// when GOTMPDIR is set, the go tool redirects the test process's TMPDIR there,
// so a go-test-built scratch target resolves under the go temp root and must
// still be admitted (the judge runs the suite with the ambient GOTMPDIR).
func TestUnderScratchBaseAcceptsGoTmpDir(t *testing.T) {
	goTmp := filepath.Join(t.TempDir(), "go-tmp")
	if err := os.Mkdir(goTmp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOTMPDIR", goTmp)
	if !underScratchBase(filepath.Join(goTmp, "victim")) {
		t.Fatalf("target under GOTMPDIR=%s refused", goTmp)
	}
	// Both arms refuse a path outside every accepted base: probe a path under
	// the package's own testdata (the repo checkout is never under os.TempDir,
	// GOTMPDIR or /tmp on any runner, including CI where TMPDIR is /tmp).
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if underScratchBase(filepath.Join(wd, "testdata", "outside-every-base")) {
		t.Fatal("path outside every scratch base was admitted")
	}
}
