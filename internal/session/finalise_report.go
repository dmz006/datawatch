package session

import (
	"log"
	"time"
)

// FinaliseSummary is the outcome of one combined finalise run: the gone-session
// record pass followed by the saved-folder pass.
type FinaliseSummary struct {
	// At is the time the run was started for.
	At time.Time `json:"at"`
	// FinalisedRecords counts the records the daemon holds that were finalised.
	FinalisedRecords int `json:"finalised_records"`
	// FinalisedFolders counts the saved session folders that were finalised.
	FinalisedFolders int `json:"finalised_folders"`
	// Skipped counts the records and folders left as they were, over both
	// passes.
	Skipped int `json:"skipped"`
	// TerminalListOK is true only when the terminal listing succeeded for both
	// passes. When it is false at least one pass finalised nothing.
	TerminalListOK bool `json:"terminal_list_ok"`
}

// RunFinalise runs FinaliseGoneSessions and then FinaliseSessionFolders,
// stores the combined summary on the Manager and returns it. When anything was
// finalised it writes one line to the daemon journal that holds the counts and
// nothing else.
func (m *Manager) RunFinalise(now time.Time) FinaliseSummary {
	records := m.FinaliseGoneSessions(now)
	folders := m.FinaliseSessionFolders(now)

	sum := FinaliseSummary{
		At:               now,
		FinalisedRecords: len(records.Finalised),
		FinalisedFolders: len(folders.Finalised),
		Skipped:          records.Skipped + folders.Skipped,
		TerminalListOK:   records.TerminalListOK && folders.TerminalListOK,
	}

	m.finaliseMu.Lock()
	m.lastFinalise = sum
	m.finaliseRan = true
	m.finaliseMu.Unlock()

	if sum.FinalisedRecords > 0 || sum.FinalisedFolders > 0 {
		log.Printf("[finalise] finalised %d session records and %d session folders",
			sum.FinalisedRecords, sum.FinalisedFolders)
	}
	return sum
}

// LastFinalise returns the summary of the most recent RunFinalise. The second
// return value is false before the first run.
func (m *Manager) LastFinalise() (FinaliseSummary, bool) {
	m.finaliseMu.Lock()
	defer m.finaliseMu.Unlock()
	return m.lastFinalise, m.finaliseRan
}
