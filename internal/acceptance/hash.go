package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// hashOf returns the hex SHA-256 of the JSON form of the parts.
func hashOf(parts ...any) string {
	h := sha256.New()
	enc := json.NewEncoder(h)
	for _, p := range parts {
		if err := enc.Encode(p); err != nil {
			// Only plain data is hashed; an encoding failure would make two
			// different lists look alike, so it must not be ignored.
			panic("acceptance: hash: " + err.Error())
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// listHash names the exact list an approval or a freeze is about: the ask, the
// kind, the items with their saved checks, and the reviewer settings.
func listHash(askHash string, kind Kind, items []Item, reviewer string) string {
	sorted := append([]Item(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	return hashOf(askHash, kind, sorted, reviewer)
}

// Hash returns the hash of the list as it stands now.
func (l *List) Hash() string { return listHash(l.Ask.Hash, l.Kind, l.Items, l.Reviewer) }

// itemHash covers what an item says and how it is checked, not its revision
// counter.
func itemHash(it Item) string {
	it.Revision = 0
	return hashOf(it)
}

// Changes is how the list differs from the list as first frozen.
type Changes struct {
	Added    []string `json:"added,omitempty"`
	Removed  []Item   `json:"removed,omitempty"`
	Altered  []string `json:"altered,omitempty"`
	AskText  bool     `json:"ask_changed,omitempty"`
	Reviewer bool     `json:"reviewer_changed,omitempty"`
}

// Any reports whether anything differs.
func (c Changes) Any() bool {
	return len(c.Added) > 0 || len(c.Removed) > 0 || len(c.Altered) > 0 || c.AskText || c.Reviewer
}

// ChangesSinceFirst compares the list with revision 1.
func (l *List) ChangesSinceFirst() Changes {
	var c Changes
	if len(l.Revisions) == 0 {
		return c
	}
	first := l.Revisions[0]
	was := map[string]Item{}
	for _, it := range first.Items {
		was[it.ID] = it
	}
	now := map[string]bool{}
	for _, it := range l.Items {
		now[it.ID] = true
		old, ok := was[it.ID]
		switch {
		case !ok:
			c.Added = append(c.Added, it.ID)
		case itemHash(old) != itemHash(it) || old.Revision != it.Revision:
			// A check saved again (new reviewed inputs) is a change too.
			c.Altered = append(c.Altered, it.ID)
		}
	}
	for _, it := range first.Items {
		if !now[it.ID] {
			c.Removed = append(c.Removed, it)
		}
	}
	c.AskText = first.AskHash != l.Ask.Hash
	c.Reviewer = first.Reviewer != l.Reviewer
	return c
}
