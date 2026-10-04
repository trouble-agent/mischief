package plan

import (
	"errors"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/journal"
	"github.com/trouble-agent/mischief/internal/rails"
)

// parseExp adapts journal.Parse for the tests (the corpus-shape loader).
func parseExp(raw []byte) (*journal.Experiment, error) {
	return journal.Parse(raw)
}

func testSetupSpec() []byte { return []byte(testExperiment) }

func testScope() rails.Scope { return rails.Scope{Level: 0, Scratch: true} }

// allAvailable is the fully-capable host (every capability kind present),
// adapted to the catalog's registry seam.
var allAvailable = catalog.RegistryFunc(func(kind string) bool { return true })

// nothingAvailable is the bare host (every named capability absent).
var nothingAvailable = catalog.RegistryFunc(func(kind string) bool { return false })

// isRefusalErr reports whether err is (or wraps) a rails refusal shape.
func isRefusalErr(err error) bool {
	var re *rails.ResolveError
	if errors.As(err, &re) {
		return true
	}
	var ref *rails.Refusal
	return errors.As(err, &ref)
}

var _ = catalog.StatusReady // keep the catalog import for the helpers' docs
