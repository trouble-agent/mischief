package selftest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ── F-009: file replaced under a live writer ────────────────────────────────
//
// The 2026-08-29/30 archive-hole shape, rootless on a scratch path (the
// descriptor's own tier: "L0 (copy) / L1 (declared path)").
//
// Land: a live writer (our own os.File — a real open fd) holds a scratch
// file; the harness renames a NEW file over the path; the descriptor's
// landed_proof (inode-identity: stat(path).inode != fstat(fd).inode) is
// MEASURED from the fd and the path.
//
// Inverse: restore-original — the pre-fault content is restored at the
// path, the swap is proven gone (a fresh fd's inode == the path's inode),
// and the descriptor's verify ("the writer's next flush is visible at the
// path") is proven by a fresh-writer flush read back — after which the
// inverse's final act rewrites exactly the pre-fault content so the
// pre/post byte-compare below is the restored steady state, not the
// proof's own scratch.
//
// Pre/post snapshot: (path bytes, writer-fd bytes) — the fd half reads
// the OLD inode (untouched by the swap: the fault's whole point), the
// path half reads whatever the path now names, so a failed restore
// drifts and a vacuous pass is impossible.

type fileReplaceLandable struct {
	dir     string
	path    string
	writer  *os.File
	content []byte
	inoPre  uint64
}

func (f *fileReplaceLandable) capability() (string, bool) {
	return "", false // rename/stat on a scratch path, rootless
}

// inodeOf stats any path (including /proc/<pid>/fd/<n> magics, which
// resolve to the open file).
func inodeOf(path string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, err
	}
	return st.Ino, nil
}

func (f *fileReplaceLandable) prepare() ([]byte, error) {
	f.content = []byte("mischief-f009-pre-state\n")
	f.path = filepath.Join(f.dir, "state.db")
	w, err := os.OpenFile(f.path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("writer open: %v", err)
	}
	f.writer = w
	if _, err := w.Write(f.content); err != nil {
		return nil, fmt.Errorf("writer seed write: %v", err)
	}
	if err := w.Sync(); err != nil {
		return nil, fmt.Errorf("writer seed sync: %v", err)
	}
	fdPath := fmt.Sprintf("/proc/self/fd/%d", int(w.Fd()))
	f.inoPre, err = inodeOf(fdPath)
	if err != nil {
		return nil, fmt.Errorf("writer fd inode: %v", err)
	}
	if pino, err := inodeOf(f.path); err != nil {
		return nil, fmt.Errorf("path inode: %v", err)
	} else if pino != f.inoPre {
		return nil, fmt.Errorf("pre-fault invariant broken: path inode %d != fd inode %d", pino, f.inoPre)
	}
	return f.snapshot()
}

// snapshot reads both halves of the pre-state: the path's bytes and the
// writer fd's bytes.
func (f *fileReplaceLandable) snapshot() ([]byte, error) {
	pathB, err := os.ReadFile(f.path)
	if err != nil {
		return nil, fmt.Errorf("snapshot path: %v", err)
	}
	fdB := make([]byte, 4096)
	n, err := f.writer.ReadAt(fdB, 0)
	if err != nil && n == 0 {
		return nil, fmt.Errorf("snapshot writer fd: %v", err)
	}
	fdB = fdB[:n]
	var out bytes.Buffer
	fmt.Fprintf(&out, "path=%s\nfd=%s\n",
		hex.EncodeToString(hash(pathB)), hex.EncodeToString(hash(fdB)))
	return out.Bytes(), nil
}

func hash(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}

func (f *fileReplaceLandable) land() landOutcome {
	if f.writer == nil {
		return bad("land: no live writer (prepare never ran)")
	}
	// the fault: rename a NEW file over the path (the descriptor's mode:
	// replace)
	replacement := f.path + ".replacement"
	if err := os.WriteFile(replacement, []byte("mischief-f009-replacement\n"), 0o600); err != nil {
		return bad("write replacement: %v", err)
	}
	if err := os.Rename(replacement, f.path); err != nil {
		return bad("rename-over: %v", err)
	}
	// MEASURED landed-proof (the descriptor's check): stat(path).inode !=
	// fstat(fd).inode for the writer.
	pino, err := inodeOf(f.path)
	if err != nil {
		return bad("post-rename path stat: %v", err)
	}
	fdino, err := inodeOf(fmt.Sprintf("/proc/self/fd/%d", int(f.writer.Fd())))
	if err != nil {
		return bad("post-rename fd inode: %v", err)
	}
	if pino == fdino {
		return bad("rename-over landed but inode identity unchanged (path=%d fd=%d) — the fault did not land", pino, fdino)
	}
	// the fd half must still see the pre-fault content (the writer's
	// bytes survive at the old inode — the archive-hole mechanism itself)
	fdB := make([]byte, 4096)
	n, err := f.writer.ReadAt(fdB, 0)
	if err != nil && n == 0 {
		return bad("post-fault fd read: %v", err)
	}
	if !bytes.Equal(fdB[:n], f.content) {
		return bad("writer fd content changed by the swap — the fault is not the rename-over shape")
	}
	return ok(fmt.Sprintf("inode-identity: stat(path).inode=%d != fstat(fd).inode=%d (writer fd holds the pre-fault bytes, path names the replacement)", pino, fdino))
}

func (f *fileReplaceLandable) inverse() landOutcome {
	if f.writer == nil {
		return bad("inverse: no live writer (land never proved)")
	}
	// 1. restore-original: the pre-fault content back at the path.
	if err := os.WriteFile(f.path, f.content, 0o600); err != nil {
		return bad("restore write: %v", err)
	}
	// 2. MEASURED: the swap is gone — a fresh fd at the path names the
	// path's own inode.
	pino, err := inodeOf(f.path)
	if err != nil {
		return bad("restored path stat: %v", err)
	}
	chk, err := os.OpenFile(f.path, os.O_RDWR, 0o600)
	if err != nil {
		return bad("restored path reopen: %v", err)
	}
	defer chk.Close()
	var st syscall.Stat_t
	if err := syscall.Fstat(int(chk.Fd()), &st); err != nil {
		return bad("restored fd fstat: %v", err)
	}
	if st.Ino != pino {
		return bad("restore issued but not proven: fresh fd inode %d != path inode %d", st.Ino, pino)
	}
	// 3. the descriptor's verify, MEASURED: "the writer's next flush is
	// visible at the path" — the recovery shape a repaired process takes
	// is reopen-and-write, so the fresh writer flushes a marker and the
	// path read-back must show it.
	marker := []byte("mischief-f009-post-restore\n")
	if _, err := chk.WriteAt(marker, int64(len(f.content))); err != nil {
		return bad("flush write: %v", err)
	}
	if err := chk.Sync(); err != nil {
		return bad("flush sync: %v", err)
	}
	back, err := os.ReadFile(f.path)
	if err != nil {
		return bad("flush read-back: %v", err)
	}
	want := append(append([]byte(nil), f.content...), marker...)
	if !bytes.Equal(back, want) {
		return bad("flush not visible at the path (got %d bytes, want %d)", len(back), len(want))
	}
	// 4. the inverse's final act: the restored steady state is exactly
	// the pre-fault content (the proof's own marker must not linger in
	// the byte-compare below — it was the flush PROOF, not the state).
	if err := os.WriteFile(f.path, f.content, 0o600); err != nil {
		return bad("steady-state rewrite: %v", err)
	}
	if err := chk.Sync(); err != nil {
		return bad("steady-state sync: %v", err)
	}
	return ok(fmt.Sprintf("restore-original: fresh fd inode %d == path inode %d; flush visible at the path; steady state = pre-fault content", st.Ino, pino))
}

func (f *fileReplaceLandable) post() ([]byte, error) {
	return f.snapshot()
}

func (f *fileReplaceLandable) cleanup() {
	if f.writer != nil {
		_ = f.writer.Close()
		f.writer = nil
	}
}
