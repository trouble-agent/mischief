package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yamlpkg "gopkg.in/yaml.v3"

	"github.com/trouble-agent/mischief/internal/journal"
)

// DefaultDir is the profiles tree, relative to the repo root. It is a SIBLING
// of catalog/faults (the primitives) and mirrors its shape: one directory per
// class, one file per profile.
const DefaultDir = "catalog/profiles"

// FindDefaultDir resolves catalog/profiles from the working directory upward,
// so the verb works from anywhere inside the repo (catalog.findDefaultDir
// precedent).
func FindDefaultDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", &LoadError{Reason: fmt.Sprintf("cwd: %v", err)}
	}
	for {
		cand := filepath.Join(wd, DefaultDir)
		if st, statErr := os.Stat(cand); statErr == nil && st.IsDir() {
			return cand, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			return "", &LoadError{Reason: fmt.Sprintf("%s not found from cwd upward", DefaultDir)}
		}
		wd = parent
	}
}

// ProfilePath resolves the file for <class>/<name> inside dir. The class is an
// EXPLICIT path segment, not a recursive walk: the catalog loader reads one
// flat directory by design, and a profile tree that recursed would let two
// files claim the same id.
func ProfilePath(dir, class, name string) string {
	return filepath.Join(dir, class, name+".yaml")
}

// SplitID splits "<class>/<name>" into its two segments, refusing anything that
// is not exactly two non-empty segments (the CLI's positional argument).
func SplitID(id string) (class, name string, err error) {
	parts := strings.Split(strings.TrimSpace(id), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("profile id must be <class>/<name>, got %q", id)
	}
	class = strings.TrimSpace(parts[0])
	name = strings.TrimSpace(parts[1])
	if strings.ContainsAny(class, `\`) || strings.ContainsAny(name, `\`) {
		return "", "", fmt.Errorf("profile id must be <class>/<name>, got %q (no backslashes)", id)
	}
	return class, name, nil
}

// Load reads and validates catalog/profiles/<class>/<name>.yaml. The returned
// profile's Class/Name are asserted against the path that resolved it, so a
// file cannot claim an id its location does not carry.
func Load(dir, class, name string) (*Profile, []byte, error) {
	path := ProfilePath(dir, class, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, &LoadError{File: path, ID: class + "/" + name,
				Reason: fmt.Sprintf("profile not found (want %s)", DefaultDir+"/<class>/<name>.yaml")}
		}
		return nil, nil, &LoadError{File: path, ID: class + "/" + name, Reason: err.Error()}
	}
	p, err := Parse(raw, path)
	if err != nil {
		return nil, raw, err
	}
	if p.Class != class || p.Name != name {
		return nil, raw, &LoadError{File: path, ID: class + "/" + name,
			Reason: fmt.Sprintf("declares %q but lives at %s (the path is the id)", p.ID(), ProfilePath(DefaultDir, class, name))}
	}
	return p, raw, nil
}

// Parse decodes and validates one profile document. file is used only to name
// refusals.
func Parse(raw []byte, file string) (*Profile, error) {
	p := &Profile{SourceFile: file}
	dec := yamlpkg.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true) // a misspelled key must refuse, not vanish
	if err := dec.Decode(p); err != nil {
		return nil, &LoadError{File: file, Reason: fmt.Sprintf("parse: %v", err)}
	}
	if err := p.validate(); err != nil {
		return nil, &LoadError{File: file, ID: p.ID(), Reason: err.Error()}
	}
	return p, nil
}

// List returns every profile id (<class>/<name>) under dir, sorted. It reads
// exactly two levels (class directory, then *.yaml) — the same flat-per-level
// shape the loader resolves.
func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, &LoadError{File: dir, Reason: err.Error()}
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		files, readErr := os.ReadDir(filepath.Join(dir, e.Name()))
		if readErr != nil {
			return nil, &LoadError{File: filepath.Join(dir, e.Name()), Reason: readErr.Error()}
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			ids = append(ids, e.Name()+"/"+strings.TrimSuffix(f.Name(), ".yaml"))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// RunID is the content-derived run id for a run of raw profile bytes at seed.
// It is the journal package's derivation (sha256 over "seed\n<N>\n" + bytes),
// so a CLI that names a run dir and the executor that writes the finding
// cannot disagree about which run it is.
func RunID(raw []byte, seed int64) string { return journal.RunID(raw, seed) }
