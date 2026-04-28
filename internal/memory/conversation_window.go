// v5.26.70 — Mempalace QW#3 port: conversation-window stitching.
//
// When session output is chunked into multiple `output_chunk` rows
// for embedding, semantic search returns one chunk at a time — but
// the operator usually wants the surrounding context too (the
// sentence that came before the match, the response that followed,
// etc.). StitchSessionWindow returns N chunks before + N chunks
// after a hit, sorted by created_at, so the caller can render a
// continuous excerpt instead of an isolated fragment.
//
// Pure Go port of mempalace's window-stitching pattern (BL99
// closets/drawers companion). Mempalace performs this at the
// retriever layer; we keep it on the Store so any backend (REST,
// MCP tool, channel command) can ask for a window without going
// through Retriever.

package memory

import "fmt"

// SessionWindow is the stitched result: the hit chunk plus its
// neighbouring chunks from the same session_id, ordered oldest
// first. The Hit field marks which entry was the original match
// so callers can highlight it in a UI.
type SessionWindow struct {
	HitID  int64    `json:"hit_id"`
	Chunks []Memory `json:"chunks"`
}

// StitchSessionWindow returns up to `before` chunks created before
// hitID + up to `after` chunks created after hitID, all from the
// same session. Returns the hit alone (no neighbours) when the
// session_id is empty or no other chunks exist.
//
// Idempotent — repeated calls with the same args return the same
// chunk set in the same order.
func (s *Store) StitchSessionWindow(hitID int64, before, after int) (*SessionWindow, error) {
	if hitID <= 0 {
		return nil, fmt.Errorf("StitchSessionWindow: positive hit_id required")
	}
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}

	var (
		hitSession string
		hitCreated string
	)
	if err := s.db.QueryRow(
		`SELECT session_id, created_at FROM memories WHERE id = ?`, hitID,
	).Scan(&hitSession, &hitCreated); err != nil {
		return nil, fmt.Errorf("look up hit: %w", err)
	}

	hit, err := s.fetchByID(hitID)
	if err != nil {
		return nil, err
	}

	out := &SessionWindow{HitID: hitID, Chunks: []Memory{*hit}}
	if hitSession == "" {
		// Standalone row — nothing to stitch.
		return out, nil
	}

	if before > 0 {
		// v6.0.0 — id < hitID covers the same-second tie case where
		// created_at < hitCreated is unable to disambiguate. SQLite's
		// CURRENT_TIMESTAMP has 1-second resolution; rapid back-to-back
		// chunk saves all share a timestamp.
		rows, err := s.db.Query(`
			SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at
			FROM memories
			WHERE session_id = ? AND id < ? AND id != ?
			ORDER BY id DESC LIMIT ?
		`, hitSession, hitID, hitID, before)
		if err == nil {
			var prev []Memory
			for rows.Next() {
				var m Memory
				if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary,
					&m.Role, &m.Wing, &m.Room, &m.Hall, &m.CreatedAt); err == nil {
					s.decryptMemory(&m)
					prev = append(prev, m)
				}
			}
			rows.Close()
			// Reverse so oldest-first.
			for i := len(prev) - 1; i >= 0; i-- {
				out.Chunks = append([]Memory{prev[i]}, out.Chunks...)
			}
		}
	}
	if after > 0 {
		rows, err := s.db.Query(`
			SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at
			FROM memories
			WHERE session_id = ? AND id > ? AND id != ?
			ORDER BY id ASC LIMIT ?
		`, hitSession, hitID, hitID, after)
		if err == nil {
			for rows.Next() {
				var m Memory
				if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary,
					&m.Role, &m.Wing, &m.Room, &m.Hall, &m.CreatedAt); err == nil {
					s.decryptMemory(&m)
					out.Chunks = append(out.Chunks, m)
				}
			}
			rows.Close()
		}
	}

	return out, nil
}

// fetchByID is the small helper used by StitchSessionWindow to get
// the hit chunk back as a Memory. Kept private — callers outside
// the conversation-window flow should use existing list/search APIs.
func (s *Store) fetchByID(id int64) (*Memory, error) {
	rows, err := s.db.Query(`
		SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at
		FROM memories WHERE id = ?
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("memory %d not found", id)
	}
	var m Memory
	if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary,
		&m.Role, &m.Wing, &m.Room, &m.Hall, &m.CreatedAt); err != nil {
		return nil, err
	}
	s.decryptMemory(&m)
	return &m, nil
}
