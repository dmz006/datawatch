package tagregistry

import "github.com/dmz006/datawatch/internal/autonomous"

// TagBook is the part of the autonomous manager that AutonomousStore uses. The
// autonomous Manager implements it.
type TagBook interface {
	ListTags() []*autonomous.Tag
	SaveTagRecords(tags []*autonomous.Tag) error
	TagAssignmentCount(tagID string) int
	RewriteTagAssignments(fromID, toID string) (int, error)
	RemoveTagAssignments(tagID string) (int, error)
}

// AutonomousStore is the Store the registry writes through. It maps the
// registry's tags onto the autonomous tag records. The records have no origin
// node or pending flag, so those two fields live only in the registry's cache.
type AutonomousStore struct {
	Book TagBook
}

// ListTags returns every tag record the autonomous store holds.
func (s AutonomousStore) ListTags() ([]Tag, error) {
	recs := s.Book.ListTags()
	out := make([]Tag, 0, len(recs))
	for _, t := range recs {
		out = append(out, tagFromRecord(t))
	}
	return out, nil
}

// ApplySnapshot replaces the tag records with tags.
func (s AutonomousStore) ApplySnapshot(tags []Tag) error {
	recs := make([]*autonomous.Tag, 0, len(tags))
	for _, t := range tags {
		recs = append(recs, recordFromTag(t))
	}
	return s.Book.SaveTagRecords(recs)
}

// AssignmentCount returns how many Automata and sessions carry the tag.
func (s AutonomousStore) AssignmentCount(tagID string) (int, error) {
	return s.Book.TagAssignmentCount(tagID), nil
}

// RewriteAssignments moves every assignment of fromID to toID.
func (s AutonomousStore) RewriteAssignments(fromID, toID string) (int, error) {
	return s.Book.RewriteTagAssignments(fromID, toID)
}

// RemoveAssignments drops every assignment of tagID.
func (s AutonomousStore) RemoveAssignments(tagID string) (int, error) {
	return s.Book.RemoveTagAssignments(tagID)
}

// tagFromRecord converts an autonomous tag record to a registry tag.
func tagFromRecord(t *autonomous.Tag) Tag {
	return Tag{
		ID:          t.ID,
		Name:        t.Name,
		ParentID:    t.ParentID,
		Color:       t.Color,
		Description: t.Description,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// recordFromTag converts a registry tag to an autonomous tag record.
func recordFromTag(t Tag) *autonomous.Tag {
	return &autonomous.Tag{
		ID:          t.ID,
		Name:        t.Name,
		ParentID:    t.ParentID,
		Color:       t.Color,
		Description: t.Description,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}
