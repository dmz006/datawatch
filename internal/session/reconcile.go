package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ReconcileResult summarises a single ReconcileSessions pass.
type ReconcileResult struct {
	// Imported is the list of session full IDs that were just added to
	// the registry from on-disk session.json files. Always empty when
	// AutoImport is false.
	Imported []string `json:"imported"`
	// Orphaned is the list of session full IDs found on disk but
	// missing from the registry. When AutoImport=true these become
	// Imported instead of Orphaned (one or the other, never both).
	Orphaned []string `json:"orphaned"`
	// Errors collects per-directory failures so a single bad
	// session.json does not abort the whole pass.
	Errors []string `json:"errors,omitempty"`
}

// ReconcileSessions walks <dataDir>/sessions/<id>/ and reports
// session directories that have a session.json but no entry in the
// in-memory registry. When autoImport is true the registry is updated
// in place (and Store.Save flushes synchronously per BL92).
//
// This is the BL93 startup hook. It is also exposed via REST/MCP/CLI
// (BL94) so an operator can trigger a reconcile on demand without a
// daemon restart.
func (m *Manager) ReconcileSessions(autoImport bool) (*ReconcileResult, error) {
	root := filepath.Join(m.dataDir, "sessions")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return &ReconcileResult{}, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	known := make(map[string]bool)
	for _, sess := range m.store.List() {
		known[sess.FullID] = true
	}

	res := &ReconcileResult{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fullID := e.Name()
		if known[fullID] {
			continue
		}
		dir := filepath.Join(root, fullID)
		sess, err := readSessionFile(dir)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", fullID, err))
			continue
		}
		if sess == nil {
			// no session.json — not an importable session dir
			continue
		}
		if autoImport {
			if err := m.store.Save(sess); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: save: %v", fullID, err))
				continue
			}
			res.Imported = append(res.Imported, sess.FullID)
		} else {
			res.Orphaned = append(res.Orphaned, fullID)
		}
	}

	sort.Strings(res.Imported)
	sort.Strings(res.Orphaned)
	return res, nil
}

// ImportSessionDir reads <dir>/session.json and adds the session to
// the registry. Returns the imported session. If a session with the
// same FullID already exists in the registry the call is a no-op and
// the existing session is returned. This is the BL94 single-dir entry
// point used by `datawatch session import`.
func (m *Manager) ImportSessionDir(dir string) (*Session, bool, error) {
	sess, err := readSessionFile(dir)
	if err != nil {
		return nil, false, err
	}
	if sess == nil {
		return nil, false, fmt.Errorf("no session.json in %s", dir)
	}
	if existing, ok := m.store.Get(sess.FullID); ok {
		return existing, false, nil
	}
	if err := m.store.Save(sess); err != nil {
		return nil, false, fmt.Errorf("save: %w", err)
	}
	return sess, true, nil
}

// readSessionFile loads session.json from a session tracking dir.
// Returns (nil, nil) when the file does not exist (the dir is not an
// importable session dir — e.g. a stray subdirectory).
func readSessionFile(dir string) (*Session, error) {
	path := filepath.Join(dir, "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if sess.FullID == "" {
		return nil, fmt.Errorf("session.json in %s is missing full_id", dir)
	}
	return &sess, nil
}
