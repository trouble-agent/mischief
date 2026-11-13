package selftest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTornWriteExternalFileLengthWitness(t *testing.T) {
	dir := t.TempDir()
	l := &tornWriteLandable{dir: dir, sector: 4096}
	pre, err := l.prepare()
	if err != nil {
		t.Fatal(err)
	}
	if len(pre) == 0 {
		t.Fatal("prepare returned an empty pre-state")
	}
	out := l.land()
	if !out.landed {
		t.Fatalf("torn write did not land: %s", out.err)
	}
	st, err := os.Stat(filepath.Join(dir, "torn-write.db"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 6144 || st.Size()%4096 == 0 {
		t.Fatalf("external file length = %d, want 6144 and a non-zero sector remainder", st.Size())
	}
	if !strings.Contains(out.proof, "external file-length witness") {
		t.Fatalf("proof omits the external file-length witness: %q", out.proof)
	}
	l.cleanup()
}

func TestTornWriteChecksumReaderRejectsUncheckedReaderData(t *testing.T) {
	dir := t.TempDir()
	l := &tornWriteLandable{dir: dir, sector: 4096}
	if _, err := l.prepare(); err != nil {
		t.Fatal(err)
	}
	if out := l.land(); !out.landed {
		t.Fatalf("torn write did not land: %s", out.err)
	}
	unchecked, err := readTornRecord(l.path, false)
	if err != nil || len(unchecked) == 0 {
		t.Fatalf("unchecked reader did not serve torn bytes: bytes=%d err=%v", len(unchecked), err)
	}
	if _, err := readTornRecord(l.path, true); err == nil {
		t.Fatal("checksum reader served a torn record")
	} else if !strings.Contains(err.Error(), "truncated") && !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("checksum reader gave an unrelated error: %v", err)
	}
	if out := l.inverse(); !out.landed {
		t.Fatalf("inverse did not restore a checksum-valid record: %s", out.err)
	}
	if _, err := l.post(); err != nil {
		t.Fatal(err)
	}
	l.cleanup()
}
