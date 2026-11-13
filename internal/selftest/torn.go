package selftest

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

const defaultTornSectorSize int64 = 4096

const tornRecordHeaderSize = 8
const tornRecordChecksumSize = sha256.Size

type tornWriteLandable struct {
	dir    string
	path   string
	sector int64
	pre    []byte
	tornAt int64
}

func (t *tornWriteLandable) capability() (string, bool) { return "", false }

func (t *tornWriteLandable) prepare() ([]byte, error) {
	if t.sector <= 0 {
		t.sector = defaultTornSectorSize
	}
	t.path = filepath.Join(t.dir, "torn-write.db")
	payload := []byte("mischief-s011-pre-state")
	t.pre = encodeTornRecord(payload)
	if err := os.WriteFile(t.path, t.pre, 0o600); err != nil {
		return nil, fmt.Errorf("write pre-state: %w", err)
	}
	return append([]byte(nil), t.pre...), nil
}

func (t *tornWriteLandable) land() landOutcome {
	if t.sector <= 0 {
		return bad("land: invalid sector size %d", t.sector)
	}
	payload := bytes.Repeat([]byte{'T'}, int(t.sector*2))
	candidate := encodeTornRecord(payload)
	// The first sector is durable; the next sector is only half present. This
	// is the torn-write shape: the file ends inside a sector and its declared
	// checksum record is incomplete.
	t.tornAt = t.sector + t.sector/2
	if t.tornAt <= tornRecordHeaderSize || t.tornAt >= int64(len(candidate)) {
		return bad("land: invalid torn boundary %d for candidate length %d", t.tornAt, len(candidate))
	}
	f, err := os.OpenFile(t.path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return bad("open target: %v", err)
	}
	if _, err := f.Write(candidate); err != nil {
		_ = f.Close()
		return bad("write candidate: %v", err)
	}
	if err := f.Truncate(t.tornAt); err != nil {
		_ = f.Close()
		return bad("truncate at sector boundary: %v", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return bad("sync torn write: %v", err)
	}
	if err := f.Close(); err != nil {
		return bad("close torn write: %v", err)
	}

	st, err := os.Stat(t.path)
	if err != nil {
		return bad("external file-length witness: %v", err)
	}
	if st.Size() != t.tornAt || st.Size()%t.sector == 0 {
		return bad("external file-length witness: size=%d, want %d and non-sector-aligned", st.Size(), t.tornAt)
	}
	unchecked, uncheckedErr := readTornRecord(t.path, false)
	if uncheckedErr != nil || len(unchecked) == 0 {
		return bad("unchecked reader did not serve the partial record: bytes=%d err=%v", len(unchecked), uncheckedErr)
	}
	if _, err := readTornRecord(t.path, true); err == nil {
		return bad("checksum reader served the torn record")
	}
	return ok(fmt.Sprintf("external file-length witness: %s size=%d (sector=%d, remainder=%d); unchecked reader served %d bytes; checksum reader rejected the torn record", t.path, st.Size(), t.sector, st.Size()%t.sector, len(unchecked)))
}

func (t *tornWriteLandable) inverse() landOutcome {
	if len(t.pre) == 0 {
		return bad("inverse: no pre-state")
	}
	if err := os.WriteFile(t.path, t.pre, 0o600); err != nil {
		return bad("restore pre-state: %v", err)
	}
	if _, err := readTornRecord(t.path, true); err != nil {
		return bad("checksum reader rejected restored state: %v", err)
	}
	return ok(fmt.Sprintf("restore-pre-state: checksum reader accepted %d bytes", len(t.pre)))
}

func (t *tornWriteLandable) post() ([]byte, error) { return os.ReadFile(t.path) }
func (t *tornWriteLandable) cleanup()              {}

func encodeTornRecord(payload []byte) []byte {
	out := make([]byte, tornRecordHeaderSize+len(payload)+tornRecordChecksumSize)
	copy(out[:4], []byte("S011"))
	binary.BigEndian.PutUint32(out[4:8], uint32(len(payload)))
	copy(out[tornRecordHeaderSize:], payload)
	sum := sha256.Sum256(payload)
	copy(out[tornRecordHeaderSize+len(payload):], sum[:])
	return out
}

func readTornRecord(path string, verifyChecksum bool) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < tornRecordHeaderSize {
		return nil, fmt.Errorf("record header truncated: %d bytes", len(data))
	}
	if !bytes.Equal(data[:4], []byte("S011")) {
		return nil, fmt.Errorf("record magic mismatch")
	}
	declared := int(binary.BigEndian.Uint32(data[4:8]))
	available := data[tornRecordHeaderSize:]
	if !verifyChecksum {
		return append([]byte(nil), available...), nil
	}
	if len(available) < declared+tornRecordChecksumSize {
		return nil, fmt.Errorf("record truncated: payload=%d available=%d", declared, len(available))
	}
	payload := available[:declared]
	want := available[declared : declared+tornRecordChecksumSize]
	got := sha256.Sum256(payload)
	if !bytes.Equal(want, got[:]) {
		return nil, fmt.Errorf("checksum mismatch")
	}
	return append([]byte(nil), payload...), nil
}
