package session

import (
	"sort"
	"strings"
	"time"
)

// TagInventoryEntry is one tag in use, the axis:value split of it, and the
// number of sessions carrying it.
type TagInventoryEntry struct {
	Tag   string `json:"tag"`
	Axis  string `json:"axis"`
	Value string `json:"value"`
	Count int    `json:"count"`
}

// tagAxisValue splits "axis:value" into its parts; Axis is empty when the tag
// carries no axis prefix and Value is the whole tag.
func tagAxisValue(tag string) (axis, value string) {
	if i := strings.IndexByte(tag, ':'); i > 0 {
		return tag[:i], tag[i+1:]
	}
	return "", tag
}

// TagInventory returns the tags in use across the live and archived sessions,
// each split into its axis and value and counted over the sessions that carry
// it. A tag counts once per session even when it appears on more than one of
// the session's tag lists (its own, system and inherited tags all count).
// The result is sorted by axis (tags carrying no axis last), then count
// (descending), then tag, so the axis groups come out grouped and ordered
// and untagged tags read at the end.
func (m *Manager) TagInventory() []TagInventoryEntry {
	counts := make(map[string]int)
	mark := func(sess *Session) {
		for _, id := range m.sessionEffectiveTagIDs(sess) {
			counts[id]++
		}
	}
	for _, sess := range m.ListSessions() {
		mark(sess)
	}
	for _, sess := range m.ListArchivedSessions(SessionFilter{}, func(*Session) bool { return true }, "", time.Time{}, 0) {
		mark(sess)
	}
	out := make([]TagInventoryEntry, 0, len(counts))
	for tag, n := range counts {
		axis, value := tagAxisValue(tag)
		out = append(out, TagInventoryEntry{Tag: tag, Axis: axis, Value: value, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Axis == out[j].Axis {
			if out[i].Count != out[j].Count {
				return out[i].Count > out[j].Count
			}
			return out[i].Tag < out[j].Tag
		}
		// A tag with no axis sorts after every axis-tagged one, then the
		// tagged axes sort ascending.
		if out[i].Axis == "" {
			return false
		}
		if out[j].Axis == "" {
			return true
		}
		return out[i].Axis < out[j].Axis
	})
	return out
}
