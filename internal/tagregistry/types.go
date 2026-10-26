package tagregistry

import (
	"errors"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// Role is what a daemon does for the shared tag registry.
type Role string

const (
	// RoleStandalone is a daemon with no keeper configured. It keeps its own
	// tags and syncs with nobody.
	RoleStandalone Role = "standalone"
	// RoleKeeper is the daemon that keeps the registry for its peers.
	RoleKeeper Role = "keeper"
	// RolePeer pulls the registry from the keeper it names.
	RolePeer Role = "peer"
)

// RoleFor derives a daemon's role from its own name and the value of
// autonomous.tags.registry_keeper. An empty value means no keeper and the
// daemon is standalone. A value equal to the daemon's own name makes it the
// keeper. Any other value names the keeper peer this daemon pulls from.
func RoleFor(nodeName, keeper string) Role {
	switch keeper {
	case "":
		return RoleStandalone
	case nodeName:
		return RoleKeeper
	default:
		return RolePeer
	}
}

// Field names used by the per-field revisions and by conflict reports.
const (
	fieldName        = "name"
	fieldParent      = "parent"
	fieldColor       = "color"
	fieldDescription = "description"
)

// Tag is one tag of the shared registry as this daemon holds it.
type Tag struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ParentID    string    `json:"parent_id,omitempty"`
	Color       string    `json:"color,omitempty"`
	Description string    `json:"description,omitempty"`
	OriginNode  string    `json:"origin_node,omitempty"`
	PendingSync bool      `json:"pending_sync,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Note kinds.
const (
	// NoteMerged records a pending tag that was merged into a keeper tag.
	NoteMerged = "merged"
	// NoteRemovedOnKeeper records a tag the keeper deleted while this daemon
	// still had assignments on it.
	NoteRemovedOnKeeper = "removed_on_keeper"
)

// Note is a visible record of a merge or of a removal on the keeper. It stays
// until it is dismissed on this daemon.
type Note struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	ParentPath string    `json:"parent_path,omitempty"`
	KeptID     string    `json:"kept_id,omitempty"`
	ReplacedID string    `json:"replaced_id,omitempty"`
	TagID      string    `json:"tag_id,omitempty"`
	Nodes      []string  `json:"nodes,omitempty"`
	MergedAt   time.Time `json:"merged_at"`
}

// Snapshot is the keeper's registry as a peer pulls it.
type Snapshot struct {
	Revision int64  `json:"revision"`
	Tags     []Tag  `json:"tags"`
	Notes    []Note `json:"notes"`
}

// WriteKind is the kind of edit a write carries.
type WriteKind string

const (
	WriteCreate   WriteKind = "create"
	WriteRename   WriteKind = "rename"
	WriteReparent WriteKind = "reparent"
	WriteRecolor  WriteKind = "recolor"
	WriteDescribe WriteKind = "describe"
	WriteDelete   WriteKind = "delete"
)

// Write is one edit a peer forwards to the keeper, or a local edit the keeper
// applies to itself.
type Write struct {
	Kind WriteKind `json:"kind"`
	// TagID is the tag the edit applies to. For a create it is the new tag's id.
	TagID string `json:"tag_id"`
	// BaseRevision is the registry revision the writer had when it made the
	// edit. The keeper compares it with the last change to each field.
	BaseRevision int64  `json:"base_revision"`
	Name         string `json:"name,omitempty"`
	ParentID     string `json:"parent_id,omitempty"`
	Color        string `json:"color,omitempty"`
	Description  string `json:"description,omitempty"`
	OriginNode   string `json:"origin_node,omitempty"`
	// Pending marks a create that a peer made while the keeper was
	// unreachable. A same-name tag under the same parent then merges into the
	// keeper's tag.
	Pending bool `json:"pending,omitempty"`
}

// WriteResult is what the keeper returns for a write. Tag is the keeper's
// value after the write, or the kept tag of a merge; it is zero for a delete.
type WriteResult struct {
	Tag      Tag   `json:"tag"`
	Revision int64 `json:"revision"`
	// Conflict is set when the keeper kept its own value for a field the
	// writer also edited. ConflictField names that field.
	Conflict      bool   `json:"conflict,omitempty"`
	ConflictField string `json:"conflict_field,omitempty"`
	// Merge is set when a pending create merged into the keeper tag Tag.
	Merge *Note `json:"merge,omitempty"`
}

// CreateInput is the input to Registry.Create.
type CreateInput struct {
	Name        string
	ParentID    string
	Color       string
	Description string
}

// View is the registry as this daemon shows it. Stale is set on a peer whose
// keeper has not answered since LastSyncedAt; the tags are then the last copy.
type View struct {
	Role         Role
	Keeper       string
	Revision     int64
	Tags         []Tag
	Notes        []Note
	Stale        bool
	LastSyncedAt time.Time
	LastError    string
}

var (
	// ErrKeeperUnreachable is returned, or wrapped, when the keeper cannot be
	// reached over federation.
	ErrKeeperUnreachable = errors.New("tag registry keeper is unreachable")
	// ErrNotKeeper is returned when a keeper-only operation is asked of a peer.
	ErrNotKeeper = errors.New("this daemon does not keep the tag registry")
	// ErrNoteNotFound matches a dismissal of a note that does not exist.
	ErrNoteNotFound = errors.New("merge note not found")
)

// The tag errors are the autonomous package's own values. A refusal by the
// registry therefore matches the same errors.Is checks as a refusal by the tag
// store, and the REST layer maps both the same way.
var (
	// ErrTagNotFound matches a tag that does not exist.
	ErrTagNotFound = autonomous.ErrTagNotFound
	// ErrTagInvalid matches a rejected tag value, such as an empty name.
	ErrTagInvalid = autonomous.ErrTagInvalid
	// ErrTagHasChildren matches a delete refused because child tags remain.
	ErrTagHasChildren = autonomous.ErrTagHasChildren
	// ErrTagInUse matches a delete refused because the tag is still assigned.
	ErrTagInUse = autonomous.ErrTagInUse
	// ErrTagCycle matches a re-parent that would make a tag its own ancestor.
	ErrTagCycle = autonomous.ErrTagCycle
)

// InUseError reports how many items still carry a tag. It is the autonomous
// package's TagInUseError, and it matches ErrTagInUse via errors.Is.
type InUseError = autonomous.TagInUseError
