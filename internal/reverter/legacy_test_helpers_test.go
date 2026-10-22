package reverter

import "os"

// writeFileString writes s to path (the tests' file-seeding helper).
func writeFileString(path, s string) error {
	return os.WriteFile(path, []byte(s), 0o644)
}

// appendJournalLine appends one raw line to the run dir's journal behind the
// store's back (the foreign/legacy-line tests' helper).
func appendJournalLine(dir, line string) error {
	f, err := osOpenAppend(dir, journalFileName)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}