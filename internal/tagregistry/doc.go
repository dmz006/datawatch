// Package tagregistry keeps one shared Automata tag registry across the
// federated daemons.
//
// One daemon is the keeper of the registry. Its name is the value of
// autonomous.tags.registry_keeper on the peers, and the daemon sets the same
// value to its own name to keep the registry. An empty value means no keeper:
// the daemon is standalone and nothing syncs. Peers pull the keeper's snapshot
// (revision, tags with their origin node, merge notes) every registry_sync_seconds
// and when a tag event arrives. A peer keeps the last copy in a cache file in
// the autonomous data directory, so the copy survives a restart.
//
// A peer forwards create, rename, re-parent, recolor, describe and delete to
// the keeper together with the registry revision it based the edit on. The
// keeper keeps a revision per field of each tag. An edit whose base is older
// than the last change to the same field of that tag is a conflict: the keeper
// keeps its own value and the peer receives that value with a conflict flag.
// Edits to different fields of one tag both apply.
//
// When the keeper is unreachable a peer serves its last copy marked stale,
// refuses rename, re-parent, recolor, describe and delete, and still allows
// create. A tag created that way is marked pending sync and is pushed when
// the keeper returns. A pushed tag whose parent and name (case-insensitive)
// match a keeper tag created meanwhile is merged into that tag: the peer
// rewrites its assignments to the kept id and both nodes are recorded in a
// merge note, so the merge is never silent. A tag the keeper deletes is
// dropped from the peer's assignments with a note saying it was removed on the
// keeper.
//
// The package has no network code. The caller supplies a Store (the local tag
// store), a KeeperClient (the federation client that talks to the keeper), an
// optional Cache and a Clock. Tests use fakes for all of them. A standalone
// daemon never syncs, so its registry does no network work and its writes are
// plain local writes.
//
// The tag errors (ErrTagCycle, ErrTagInUse, InUseError and the others) are the
// autonomous package's own values, so one errors.Is check matches a refusal
// from either. That makes this package import autonomous. For that reason the
// store adapter that wraps the autonomous tag records must live outside
// autonomous, or the import would cycle.
//
// Commit order. A change to the registry is first made in memory, then the
// cache is saved, then the store is changed. The cache save is the commit point:
// when it fails, the registry goes back to its previous state and the store is
// not touched. A store step that fails restores the registry and writes the
// previous copy back to the cache. A store step that fails after an earlier
// step already changed the store is not undone, because the Store interface has
// no inverse operations; the error reports it.
//
// Roles. An empty autonomous.tags.registry_keeper gives RoleStandalone: the
// daemon keeps its own tags and nothing syncs. A value equal to the daemon's own
// name gives RoleKeeper, and any other value gives RolePeer.
//
// Not wired yet. This package is the core only. The autonomous store adapter,
// the federation KeeperClient, the REST, MCP and CLI handlers that call
// Registry, the tag event hook that calls Notify, and the Run goroutine in daemon
// startup are follow-up work. Until they land, autonomous.tags.registry_keeper
// and registry_sync_seconds do not change behaviour.
package tagregistry
