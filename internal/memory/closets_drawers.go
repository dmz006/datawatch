// BL99 — closets/drawers (mempalace verbatim → summary chain).
//
// Mempalace's 6-level palace separates closets (summaries pointing to
// originals) from drawers (verbatim originals). Datawatch already
// adopts wing/room/hall (3 of 6 levels). With multi-agent F10 writes
// scaling memory volume, the two-tier chain becomes valuable: queries
// hit small/fast summary embeddings first; only drill into the large
// verbatim when the operator needs the full text.
//
// Schema addition (in store.go migration):
//   memories.drawer_id INTEGER DEFAULT 0   -- 0 = standalone row;
//                                              non-zero = closet
//                                              pointing at drawer
//
// SaveClosetWithDrawer writes the verbatim first (no embedding),
// then the closet (with embedding from the summary text), linking
// the closet's drawer_id at the verbatim's row. The closet is what
// search hits; the drawer is what an operator drills into.

package memory

import (
	"context"
	"fmt"
	"strings"
)

// ClosetWithDrawerResult bundles the two row IDs the chain creates.
type ClosetWithDrawerResult struct {
	DrawerID int64
	ClosetID int64
}

// SaveClosetWithDrawer writes a verbatim drawer + a summary closet
// linked to it. Both rows live in the same memory namespace; only
// the summary gets an embedding so search costs scale with summary
// volume, not verbatim size.
//
// embedder is optional — when nil, both rows are stored without an
// embedding (search-by-namespace still works; vector search does not).
func SaveClosetWithDrawer(ctx context.Context, store *Store, embedder Embedder,
	projectDir, verbatim, summary, role, sessionID string,
) (*ClosetWithDrawerResult, error) {
	if strings.TrimSpace(verbatim) == "" {
		return nil, fmt.Errorf("SaveClosetWithDrawer: verbatim required")
	}
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("SaveClosetWithDrawer: summary required")
	}
	if role == "" {
		role = "session"
	}

	// Drawer: full content, no embedding (saves disk + embedder calls).
	drawerID, err := store.Save(projectDir, verbatim, "", role+"-verbatim",
		sessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("save drawer: %w", err)
	}

	// Closet: summary content (also stored as content for search to
	// match against), embedding from the summary text, drawer_id
	// pointing at the verbatim row.
	var summaryVec []float32
	if embedder != nil {
		v, embErr := embedder.Embed(ctx, summary)
		if embErr == nil {
			summaryVec = v
		}
	}
	closetID, err := store.SaveCloset(projectDir, summary, role,
		sessionID, summaryVec, drawerID)
	if err != nil {
		return nil, fmt.Errorf("save closet: %w", err)
	}
	return &ClosetWithDrawerResult{DrawerID: drawerID, ClosetID: closetID}, nil
}

// Drawer returns the verbatim memory linked to the supplied closet
// ID. Returns nil + nil error when the closet has no drawer (a
// standalone non-chain row).
func (s *Store) Drawer(closetID int64) (*Memory, error) {
	if closetID <= 0 {
		return nil, fmt.Errorf("Drawer: positive closet_id required")
	}
	var drawerID int64
	if err := s.db.QueryRow(`SELECT drawer_id FROM memories WHERE id = ?`, closetID).Scan(&drawerID); err != nil {
		return nil, fmt.Errorf("look up closet: %w", err)
	}
	if drawerID == 0 {
		return nil, nil // standalone closet — no chain
	}
	rows, err := s.db.Query(`
		SELECT id, session_id, project_dir, content, summary, role,
		       wing, room, hall, namespace, created_at, embedding
		FROM memories WHERE id = ?
	`, drawerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var m Memory
	var emb []byte
	if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content,
		&m.Summary, &m.Role, &m.Wing, &m.Room, &m.Hall, &m.Namespace,
		&m.CreatedAt, &emb); err != nil {
		return nil, err
	}
	return &m, nil
}

// SaveCloset is a low-level write that records a closet row with its
// drawer_id link. Operators normally use SaveClosetWithDrawer; this
// is exported for tests + the rare case where a caller already wrote
// the drawer separately and just wants to link a fresh closet to it.
//
// Goes through the normal Save path first (so dedup + WAL + the
// normal indexing happens) then patches the new row with drawer_id.
// drawer_id <= 0 stores a standalone summary (no chain).
func (s *Store) SaveCloset(projectDir, summary, role, sessionID string,
	embedding []float32, drawerID int64) (int64, error) {
	closetRole := role + "-summary"
	id, err := s.Save(projectDir, summary, summary, closetRole, sessionID, embedding)
	if err != nil {
		return 0, err
	}
	if drawerID > 0 {
		if _, err := s.db.Exec(`UPDATE memories SET drawer_id = ? WHERE id = ?`,
			drawerID, id); err != nil {
			return id, fmt.Errorf("link drawer: %w", err)
		}
	}
	return id, nil
}
