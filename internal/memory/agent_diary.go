// BL97 — agent diaries (mempalace per-agent wing).
//
// Mempalace gives each agent a "wing" with diary-style entries that
// outlive the agent's lifetime: decisions made, files touched,
// surprises encountered. Datawatch already has the wing/room/hall
// columns from S6.1 — BL97 adds the helpers that use them with a
// per-agent convention so an agent's diary is uniformly queryable
// after termination.
//
// Convention:
//   wing  = "agent-<agent_id>"            (one wing per spawned agent)
//   room  = operator-supplied topic        (e.g. "decisions", "edits")
//   hall  = facts | events | discoveries | preferences | advice

package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DiaryEntry is one chronological line in an agent's diary.
type DiaryEntry struct {
	AgentID   string
	Room      string
	Hall      string
	Content   string
	CreatedAt time.Time
}

// AgentWing returns the canonical wing name for an agent's diary.
// Empty agentID returns "" (caller can branch on that).
func AgentWing(agentID string) string {
	if agentID == "" {
		return ""
	}
	return "agent-" + agentID
}

// AppendDiary writes one diary entry for the named agent. Requires
// a NamespacedBackend with metadata support — i.e. the SQLite Store.
// Returns the new memory ID, or an error when the backend doesn't
// support spatial metadata or the entry is empty.
//
// Persisted via SaveWithMeta so dedup + WAL + KG hooks all fire as
// for any other memory write — the only thing different is the
// canonical wing convention.
func AppendDiary(store Backend, agentID, projectDir, room, hall, content string) (int64, error) {
	if agentID == "" {
		return 0, fmt.Errorf("AppendDiary: agent_id required")
	}
	if strings.TrimSpace(content) == "" {
		return 0, fmt.Errorf("AppendDiary: content required")
	}
	if hall == "" {
		hall = "events"
	}
	wing := AgentWing(agentID)
	return store.SaveWithMeta(projectDir, content, "" /* summary */, "diary",
		"diary-"+agentID, wing, room, hall, nil /* embedding */)
}

// ListDiary returns the diary entries for one agent, oldest-first.
// Requires the store to surface ListFiltered (every Backend impl
// must — it's part of the interface contract).
func ListDiary(store Backend, agentID string, limit int) ([]DiaryEntry, error) {
	if agentID == "" {
		return nil, fmt.Errorf("ListDiary: agent_id required")
	}
	wing := AgentWing(agentID)
	memories, err := store.ListFiltered("" /* projectDir */, "diary", "" /* since */,
		clampDiaryLimit(limit))
	if err != nil {
		return nil, err
	}
	out := make([]DiaryEntry, 0, len(memories))
	for _, m := range memories {
		if m.Wing != wing {
			continue
		}
		out = append(out, DiaryEntry{
			AgentID:   agentID,
			Room:      m.Room,
			Hall:      m.Hall,
			Content:   m.Content,
			CreatedAt: m.CreatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func clampDiaryLimit(n int) int {
	if n <= 0 {
		return 200
	}
	if n > 5000 {
		return 5000
	}
	return n
}
