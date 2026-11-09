package mergeshadow

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// Derived is what the shadow merge computes from an Automaton's change record.
// With no change record every field is empty or false: nothing is invented.
type Derived struct {
	// ReadmeLine is the README "Current release" line for the release.
	ReadmeLine string
	// RegistryRow is the row for the registry table in docs/plans/README.md.
	RegistryRow string
	// ParityIssueNeeded is true when the record lists changed files under
	// internal/server/web/.
	ParityIssueNeeded bool
}

// Derive computes the README line, the registry row and parity_issue_needed.
// version and date (YYYY-MM-DD) fill the README line; rowNumber fills the
// registry row (empty prints NNN). A nil rec yields the zero Derived.
func Derive(rec *changerecord.ChangeRecord, version, date, rowNumber string) (Derived, error) {
	if rec == nil {
		return Derived{}, nil
	}
	row, _, err := genartifacts.Render(genartifacts.RegistryRow, rec, genartifacts.Options{RowNumber: rowNumber})
	if err != nil {
		return Derived{}, fmt.Errorf("mergeshadow: registry row: %w", err)
	}
	summary := fmt.Sprintf("Change from Automaton %s: %d files in %d packages.", rec.AutomatonID, len(rec.Files), len(rec.Packages))
	return Derived{
		ReadmeLine:        fmt.Sprintf("**Current release: %s (%s).** %s", version, date, summary),
		RegistryRow:       strings.TrimSpace(row),
		ParityIssueNeeded: genartifacts.PWAChanged(rec),
	}, nil
}
