package ops

import (
	"os"
	"strings"
	"testing"
)

// MISCHIEF-DF-34: a plan that aborts mid-way must report what ALREADY
// landed before the failing step — the half-state is otherwise invisible
// to the operator (the caller prints only the error). These tests pin the
// engine contract the cmd layer surfaces: the landed/removed report
// carries one entry per completed action in execution order, uses the
// verbs' greppable vocabulary (created/wrote for install, removed for
// removals), and is the DISK TRUTH (everything named exists, nothing
// unnamed does).

// TestExecuteReportsLandedOnFailure: the finding's install shape — the
// state prefix mkdir lands, the drop-in write is then refused (its parent
// dir was swapped for a plain file between planning and execution), and
// the manifest action never runs (plan order). The landed report names
// exactly the prefix dir — the answer to "what exists on disk right now
// because of this command?".
func TestExecuteReportsLandedOnFailure(t *testing.T) {
	tempRoot(t)
	// a planable host: the drop-in parent exists and the target is absent
	if err := os.MkdirAll(resolve("/etc/sudoers.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := PlanInstall(InstallOpts{Prefix: DefaultPrefix})
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	// sabotage AFTER planning: swap the drop-in PARENT for a plain file
	// so the write's implicit parent mkdir fails mid-plan (ENOTDIR) —
	// after action 1 (the state prefix) has already landed. The plan-time
	// foreign-file checks read the same paths, so the sabotage must come
	// between PlanInstall and Execute; that is exactly the window whose
	// half-state this task reports.
	if err := os.RemoveAll(resolve("/etc/sudoers.d")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolve("/etc/sudoers.d"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, landed, err := p.Execute()
	if err == nil {
		t.Fatal("the sabotaged drop-in write did not fail")
	}
	// the failing step: the write's implicit parent mkdir (a plain file
	// sits at /etc/sudoers.d) aborts BEFORE the write itself
	if !strings.Contains(err.Error(), "install: mkdir") || !strings.Contains(err.Error(), "/etc/sudoers.d") {
		t.Fatalf("error does not name the failing step: %v", err)
	}
	want := []string{"created " + DefaultPrefix}
	if len(landed) != len(want) {
		t.Fatalf("landed: %v, want %v", landed, want)
	}
	for i := range want {
		if landed[i] != want[i] {
			t.Fatalf("landed[%d] = %q, want %q (full: %v)", i, landed[i], want[i], landed)
		}
	}
	// the report is the disk truth: the prefix exists as a dir; the
	// drop-in's parent is still the sabotaged plain file (nothing landed
	// under it — the path is unreachable, ENOTDIR, never IsNotExist)
	if fi, err := os.Stat(resolve(DefaultPrefix)); err != nil || !fi.IsDir() {
		t.Fatalf("landed prefix is not on disk as a dir: %v (%v)", fi, err)
	}
	if fi, err := os.Lstat(resolve("/etc/sudoers.d")); err != nil || fi.Mode().IsRegular() == false {
		t.Fatalf("/etc/sudoers.d is no longer the sabotaged plain file: %v (%v)", fi, err)
	}
}

// TestExecuteUninstallRemovedIsThePreFailureState: the uninstall finding —
// the final prefix rmdir refuses on residue (fail closed) AFTER the
// drop-in and the manifest are already gone. The returned removed slice
// IS the removed-so-far report: in plan order, never naming the artefact
// whose action failed.
func TestExecuteUninstallRemovedIsThePreFailureState(t *testing.T) {
	tempRoot(t)
	opts := InstallOpts{Prefix: DefaultPrefix}
	p, err := PlanInstall(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Execute(); err != nil {
		t.Fatal(err)
	}
	// residue the install did not create: the rmdir refuses
	if err := os.WriteFile(resolve(DefaultPrefix+"/operator-data.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	up, err := PlanUninstall(opts, "", RetentionPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := up.ExecuteUninstall()
	if err == nil || !strings.Contains(err.Error(), "residue") {
		t.Fatalf("residue prefix not refused: %v", err)
	}
	want := []string{DefaultDropInPath, DefaultPrefix + "/helper-manifest.json"}
	if len(removed) != len(want) {
		t.Fatalf("removed: %v, want %v", removed, want)
	}
	for i := range want {
		if removed[i] != want[i] {
			t.Fatalf("removed[%d] = %q, want %q (full: %v)", i, removed[i], want[i], removed)
		}
	}
	// the failing artefact is never reported as removed
	if _, err := os.Stat(resolve(DefaultPrefix)); err != nil {
		t.Fatal("the refused prefix dir vanished — the residue guard broke")
	}
}

// TestExecuteLandedCoversParentsReplacementsAndStopsAtTheAbort: the landed
// report covers EVERYTHING a command put on disk or took off it — the
// implicit parent dir a write creates on a bare root, and a previous file
// a write replaced (the operator must know the old bytes are gone) — and
// stops cleanly at the abort (nothing after the failing action lands or
// reports).
func TestExecuteLandedCoversParentsReplacementsAndStopsAtTheAbort(t *testing.T) {
	tempRoot(t)
	// first command: a manifest under a nested prefix (does not exist yet)
	first := &Plan{Op: "install"}
	first.Actions = []Action{
		{Kind: OpWriteFile, Path: DefaultPrefix + "/nested/helper-manifest.json", Mode: "0644", Bytes: []byte("old\n"), Reason: "previous render"},
	}
	if _, _, err := first.Execute(); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// second command: refresh that manifest (replace), write the drop-in
	// (whose /etc parent does not exist on this bare root yet), then hit
	// a mkdir whose target path is a plain file — the abort.
	second := &Plan{Op: "install"}
	second.Actions = []Action{
		{Kind: OpWriteFile, Path: DefaultPrefix + "/nested/helper-manifest.json", Mode: "0644", Bytes: []byte("new\n"), Reason: "refresh"},
		{Kind: OpWriteFile, Path: DefaultDropInPath, Mode: "0440", Bytes: []byte("drop\n"), Reason: "drop-in"},
		{Kind: OpWriteDir, Path: "/tmp/blocked-target", Reason: "unreachable — aborts before"},
	}
	// the abort: a plain file sits at the mkdir's target (under the temp
	// root's tmp/), so MkdirAll refuses (ENOTDIR) and the run stops
	if err := os.MkdirAll(resolve("/tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolve("/tmp/blocked-target"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, landed, err := second.Execute()
	if err == nil {
		t.Fatal("the file-at-target-dir action did not fail")
	}
	want := []string{
		// the replace arm's parent dir necessarily already existed (the
		// previous render lived there) — no `created` line for it
		"removed " + DefaultPrefix + "/nested/helper-manifest.json", // the replaced previous render
		"wrote " + DefaultPrefix + "/nested/helper-manifest.json",
		"created /etc/sudoers.d", // parent the drop-in write made on this bare root
		"wrote " + DefaultDropInPath,
	}
	if len(landed) != len(want) {
		t.Fatalf("landed: %v, want %v", landed, want)
	}
	for i := range want {
		if landed[i] != want[i] {
			t.Fatalf("landed[%d] = %q, want %q (full: %v)", i, landed[i], want[i], landed)
		}
	}
	// the aborted action landed nothing
	if fi, err := os.Stat(resolve("/tmp/blocked-target")); err != nil || fi.IsDir() {
		t.Fatalf("the aborted mkdir state wrong: %v (%v)", fi, err)
	}
}
