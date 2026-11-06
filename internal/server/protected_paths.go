// Protected-paths REST surface:
//
//   GET /api/autonomous/protected-paths[?automaton=<id>] — the protected-paths
//   action and rules source in effect, the effective protected list, every task
//   held because its commit changed protected paths and every Automaton that
//   authorises some in advance.
//
// It also carries the strict write checks for the three config keys that
// PUT /api/config and PUT /api/autonomous/config apply to
// automation.protected_paths, automation.protected_paths_action and
// automation.rules_review_source: an absent key keeps the current value, but a
// present one must hold an allowed word (action: hold|warn|block|off, source:
// base_commit|worktree) or a clean list of repository-relative entries.
//
// POST /api/autonomous/prds/{id}/set_authorized_paths lives in the action
// switch in autonomous.go and uses authorizedPathsSetter below.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	protectedPathsKey       = "autonomous.automation.protected_paths"
	protectedPathsActionKey = "autonomous.automation.protected_paths_action"
	rulesReviewSourceKey    = "autonomous.automation.rules_review_source"

	// maxProtectedPathEntries caps the entries of the protected list.
	maxProtectedPathEntries = 100
	// maxProtectedPathEntryBytes caps the length of one entry.
	maxProtectedPathEntryBytes = 256
)

// protectedPathsReader is the narrow Manager surface the protected-paths
// route needs; asserted like uncommittedWorkReader so minimal test doubles
// keep compiling.
type protectedPathsReader interface {
	ProtectedPathsStatus(automatonID string) autonomous.ProtectedPathsStatus
}

// authorizedPathsSetter is the narrow Manager surface behind
// POST /api/autonomous/prds/{id}/set_authorized_paths.
type authorizedPathsSetter interface {
	SetAuthorizedProtectedPaths(prdID string, paths []string, actor string) (any, error)
}

// handleProtectedPaths serves GET /api/autonomous/protected-paths: the action
// and rules source in effect, the effective protected list, the held list (one
// entry per held task) and the authorised list (one entry per Automaton). The
// optional automaton query narrows the held and authorised lists to one
// Automaton; an id that matches nothing reads empty lists. Read-only: only GET
// is served, the Automata must be enabled, a federated peer needs
// autonomous:read, and the Manager must expose the status. Any other method is
// a 405.
func (s *Server) handleProtectedPaths(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	pr, ok := s.autonomousMgr.(protectedPathsReader)
	if !ok {
		http.Error(w, "Automaton protected-paths status unavailable", http.StatusServiceUnavailable)
		return
	}
	st := pr.ProtectedPathsStatus(r.URL.Query().Get("automaton"))
	if st.Paths == nil {
		st.Paths = []string{}
	}
	if st.Held == nil {
		st.Held = []autonomous.ProtectedPathsHeldEntry{}
	}
	for i := range st.Held {
		if st.Held[i].Paths == nil {
			st.Held[i].Paths = []string{}
		}
	}
	if st.Authorized == nil {
		st.Authorized = []autonomous.ProtectedPathsAuthorizedEntry{}
	}
	for i := range st.Authorized {
		if st.Authorized[i].Paths == nil {
			st.Authorized[i].Paths = []string{}
		}
	}
	writeJSONOK(w, st)
}

// validateProtectedPathsAction checks automation.protected_paths_action: it
// must be one of hold, warn, block or off. Empty and unknown words are
// rejected on write, so a read back always shows the word that was written.
func validateProtectedPathsAction(v string) error {
	switch v {
	case config.ProtectedPathsActionHold, config.ProtectedPathsActionWarn,
		config.ProtectedPathsActionBlock, config.ProtectedPathsActionOff:
		return nil
	}
	return fmt.Errorf("%s: must be %q, %q, %q or %q", protectedPathsActionKey,
		config.ProtectedPathsActionHold, config.ProtectedPathsActionWarn,
		config.ProtectedPathsActionBlock, config.ProtectedPathsActionOff)
}

// validateRulesReviewSource checks automation.rules_review_source: it must be
// base_commit or worktree.
func validateRulesReviewSource(v string) error {
	switch v {
	case config.RulesReviewSourceBaseCommit, config.RulesReviewSourceWorktree:
		return nil
	}
	return fmt.Errorf("%s: must be %q or %q", rulesReviewSourceKey,
		config.RulesReviewSourceBaseCommit, config.RulesReviewSourceWorktree)
}

// protectedPathsWordAllowed names the allowed words of the two word keys for
// the type-mismatch message.
func protectedPathsWordAllowed(key string) string {
	if key == rulesReviewSourceKey {
		return config.RulesReviewSourceBaseCommit + " or " + config.RulesReviewSourceWorktree
	}
	return config.ProtectedPathsActionHold + ", " + config.ProtectedPathsActionWarn + ", " +
		config.ProtectedPathsActionBlock + " or " + config.ProtectedPathsActionOff
}

// validateProtectedPathsWord is the PUT /api/config form of the two word
// checks above: v is the raw JSON value of one dotted key, which must be a
// string holding an allowed word.
func validateProtectedPathsWord(key string, v interface{}) error {
	str, ok := v.(string)
	if !ok {
		return fmt.Errorf("%s: must be a string (%s)", key, protectedPathsWordAllowed(key))
	}
	if key == rulesReviewSourceKey {
		return validateRulesReviewSource(str)
	}
	return validateProtectedPathsAction(str)
}

// checkProtectedPathEntries validates a list of protected-path entries: each
// must be repository-relative (not absolute), have no .. segment and be at
// most 256 bytes, and the list holds at most 100 entries. Blank entries are
// ignored. The error names the offending entry. It returns the trimmed,
// non-empty entries in order.
func checkProtectedPathEntries(key string, entries []string) ([]string, error) {
	list := make([]string, 0, len(entries))
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		switch {
		case len(e) > maxProtectedPathEntryBytes:
			return nil, fmt.Errorf("%s: entry %q is longer than %d bytes", key, truncateProtectedEntry(e), maxProtectedPathEntryBytes)
		case strings.HasPrefix(e, "/"):
			return nil, fmt.Errorf("%s: entry %q is absolute; use a repository-relative path", key, e)
		case protectedEntryEscapes(e):
			return nil, fmt.Errorf("%s: entry %q contains a .. segment", key, e)
		}
		list = append(list, e)
	}
	if len(list) > maxProtectedPathEntries {
		return nil, fmt.Errorf("%s: too many entries: %d (at most %d)", key, len(list), maxProtectedPathEntries)
	}
	return list, nil
}

func protectedEntryEscapes(e string) bool {
	for _, seg := range strings.Split(strings.ReplaceAll(e, "\\", "/"), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func truncateProtectedEntry(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// parseProtectedPathsValue reads the value of automation.protected_paths as
// PUT /api/config sends it: a JSON array of strings or one comma separated
// string. It returns the checked entries in their normalized form (nil for an
// empty list, which clears the key back to the default list).
func parseProtectedPathsValue(v interface{}) ([]string, error) {
	var entries []string
	switch val := v.(type) {
	case nil:
		return nil, nil
	case string:
		entries = strings.Split(val, ",")
	case []string:
		entries = val
	case []interface{}:
		for _, item := range val {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s: every entry must be a string", protectedPathsKey)
			}
			entries = append(entries, s)
		}
	default:
		return nil, fmt.Errorf("%s: must be a list of strings or a comma separated string", protectedPathsKey)
	}
	list, err := checkProtectedPathEntries(protectedPathsKey, entries)
	if err != nil {
		return nil, err
	}
	// Store the cleaned form (no leading ./, directory form for a trailing
	// /**, de-duplicated) so config.yaml, the live manager and the read back
	// all show the same list.
	list = config.NormalizeProtectedPaths(list)
	if len(list) == 0 {
		return nil, nil
	}
	return list, nil
}

// validateProtectedPathsRaw checks the protected_paths value in the body of
// PUT /api/autonomous/config, where the manager merges it as a JSON list.
func validateProtectedPathsRaw(raw json.RawMessage) error {
	if string(raw) == "null" {
		return nil
	}
	var entries []string
	if json.Unmarshal(raw, &entries) != nil {
		return fmt.Errorf("%s: must be a list of strings", protectedPathsKey)
	}
	_, err := checkProtectedPathEntries(protectedPathsKey, entries)
	return err
}

// protectedPathsEffectiveList is the effective protected list GET /api/config
// shows: the explicit list when it has a usable entry, else the default list
// built on the effective rules file.
func protectedPathsEffectiveList(cfg *config.Config) []string {
	list := cfg.Autonomous.Automation.EffectiveProtectedPaths(cfg.Autonomous.RulesFile)
	if list == nil {
		return []string{}
	}
	return list
}

// withProtectedPathsList makes the automation JSON carry the protected list
// explicitly. The field is omitempty, so a PUT that clears the list back to
// empty (the default list) was dropped from the pushed block and the live
// manager kept serving the stale list until a restart. Writing the current
// value, [] when empty, lets the live merge apply the clear. A block that
// cannot be decoded is returned unchanged.
func withProtectedPathsList(automation json.RawMessage, list []string) json.RawMessage {
	var auto map[string]json.RawMessage
	if json.Unmarshal(automation, &auto) != nil {
		return automation
	}
	if list == nil {
		list = []string{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return automation
	}
	auto["protected_paths"] = b
	out, err := json.Marshal(auto)
	if err != nil {
		return automation
	}
	return out
}
