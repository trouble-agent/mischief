package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// revertResultSeries matches one exported mischiefd_revert_total series line
// and captures its result label. Anchored at line start so the HELP/TYPE
// comments and the histogram families can never contribute a label.
var revertResultSeries = regexp.MustCompile(`(?m)^mischiefd_revert_total\{result="([^"]+)"\} `)

// backtickedSpan captures one `...` span inside the documentation table row.
var backtickedSpan = regexp.MustCompile("`([^`]+)`")

// revertOutcomeToken is the shape a documented result label must have to
// count as vocabulary: a bare lowercase identifier. It deliberately rejects
// the metric token in the row's first cell (`mischiefd_revert_total{result}`
// carries braces) and the multi-word prose spans, so the vocabulary set is
// the labels and nothing else.
var revertOutcomeToken = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// minExportedOutcomes is the floor under the EXPOSITION side: knownOutcomes
// always renders no_hold plus the reverter's declared vocabulary, so fewer
// than four series means the family is missing or the scrape is broken — a
// hard failure, never an empty-vs-empty equality.
const minExportedOutcomes = 4

// minDocumentedTokens is the anti-no-op floor under the DOCUMENTED side,
// deliberately LOWER than today's vocabulary size: a row that loses one token
// must still reach the set comparison and fail there, naming the drifted
// label, instead of being reported as a parser failure. A wholesale
// extraction collapse (no backticked labels, a reworded row, a renamed
// metric) lands here and prints the row it read.
const minDocumentedTokens = 2

// TestMetricsDoc_RevertResultVocabularyMatchesExposition is the DOC-28
// census: the mischiefd_revert_total result vocabulary documented in
// docs/OPERATIONS.md must be EXACTLY the label set the binary exports.
//
// Why this test exists: the family table documents a CLOSED result
// vocabulary, and closed sets drift in one direction only — code grows a new
// outcome (not_landed landed there before the table did), the table keeps the
// old tokens, and an operator alerting on `result="not_landed"` sees an empty
// series forever while the exposition emits series the table never named. A
// hardcoded expected list would not catch that: it would just be a third copy
// of the vocabulary to forget. So this test reads BOTH sides from their live
// sources — the labels out of the real exposition render path, the documented
// tokens out of the doc row itself — and asserts set equality in both
// directions.
//
// The exposition side is deliberately the REAL render path
// (daemon.New(dir) -> Metrics.Handler() -> render()), not a hand-built
// counter map: a table that agrees with a test fixture but not with
// `curl /metrics` is exactly the drift this census is meant to forbid.
func TestMetricsDoc_RevertResultVocabularyMatchesExposition(t *testing.T) {
	exported := exportedRevertOutcomes(t, scrapedExposition(t))
	documented := documentedRevertOutcomes(t, string(readRepoFile(t, filepath.Join("docs", "OPERATIONS.md"))))

	if missingFromDoc := difference(exported, documented); len(missingFromDoc) > 0 {
		t.Errorf("docs/OPERATIONS.md does not document exported mischiefd_revert_total label(s) %v; exported=%v documented=%v",
			missingFromDoc, exported, documented)
	}
	if missingFromExposition := difference(documented, exported); len(missingFromExposition) > 0 {
		t.Errorf("docs/OPERATIONS.md documents mischiefd_revert_total label(s) %v the binary never exports; exported=%v documented=%v",
			missingFromExposition, exported, documented)
	}
}

// scrapedExposition renders the metrics of a freshly built daemon over an
// empty run dir — no hold is armed, so the body is the zero-observation
// exposition a monitor sees on its first scrape of a live mischiefd.
func scrapedExposition(t *testing.T) string {
	t.Helper()
	d, err := New(localTempDir(t))
	if err != nil {
		t.Fatalf("new daemon: %v", err)
	}
	t.Cleanup(func() { _ = d.store.Close() })

	body, code := scrape(t, d.Metrics.Handler(), "/metrics")
	if code != http.StatusOK {
		t.Fatalf("scrape status: got %d want %d", code, http.StatusOK)
	}
	return body
}

// exportedRevertOutcomes extracts the result labels of mischiefd_revert_total
// from a scraped exposition body, sorted and de-duplicated.
func exportedRevertOutcomes(t *testing.T, body string) []string {
	t.Helper()
	labels := map[string]struct{}{}
	for _, match := range revertResultSeries.FindAllStringSubmatch(body, -1) {
		labels[match[1]] = struct{}{}
	}
	out := sortedSet(labels)
	if len(out) < minExportedOutcomes {
		t.Fatalf("exposition carries %d mischiefd_revert_total result label(s) %v, want at least %d — the series is missing or the scrape is broken:\n%s",
			len(out), out, minExportedOutcomes, body)
	}
	return out
}

// documentedRevertOutcomes extracts the backticked result labels from the
// docs/OPERATIONS.md table row whose metric is mischiefd_revert_total, sorted
// and de-duplicated. The row is located by content (a table row naming the
// metric), never by line number, so the table can grow above it.
func documentedRevertOutcomes(t *testing.T, docs string) []string {
	t.Helper()
	const metric = "mischiefd_revert_total"

	var row string
	for i, line := range strings.Split(docs, "\n") {
		trimmed := strings.TrimSpace(line)
		// A table row starts with the pipe; the promql example further down
		// the document also names the metric, so the shape check is what
		// keeps the census off it.
		if !strings.HasPrefix(trimmed, "|") || !strings.Contains(trimmed, metric) {
			continue
		}
		if row != "" {
			t.Fatalf("docs/OPERATIONS.md line %d: more than one table row documents %s; the census cannot tell which vocabulary is authoritative", i+1, metric)
		}
		row = trimmed
	}
	if row == "" {
		t.Fatalf("docs/OPERATIONS.md: no table row documents %s — the family table was reworded or the metric renamed", metric)
	}

	labels := map[string]struct{}{}
	for _, match := range backtickedSpan.FindAllStringSubmatch(row, -1) {
		if revertOutcomeToken.MatchString(match[1]) {
			labels[match[1]] = struct{}{}
		}
	}
	out := sortedSet(labels)
	if len(out) < minDocumentedTokens {
		t.Fatalf("docs/OPERATIONS.md %s row yielded %d backticked result label(s) %v, want at least %d — the row's vocabulary is no longer a machine-readable backticked list: %s",
			metric, len(out), out, minDocumentedTokens, row)
	}
	return out
}

// readRepoFile reads rel from the repository root, resolved by walking up
// from the test's working directory until go.mod is found — the suite must
// not depend on an absolute checkout path.
func readRepoFile(t *testing.T, rel string) []byte {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			path := filepath.Join(dir, rel)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read repo file %s: %v", path, err)
			}
			return data
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above the test working directory; cannot resolve repo-relative %q", rel)
		}
		dir = parent
	}
}

// sortedSet returns the set as a sorted slice, so failure messages and
// comparisons are stable scrape to scrape.
func sortedSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// difference returns the members of left that are absent from right.
func difference(left, right []string) []string {
	inRight := make(map[string]struct{}, len(right))
	for _, value := range right {
		inRight[value] = struct{}{}
	}
	var out []string
	for _, value := range left {
		if _, ok := inRight[value]; !ok {
			out = append(out, value)
		}
	}
	return out
}
