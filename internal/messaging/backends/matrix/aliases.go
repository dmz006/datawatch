// aliases.go — room alias resolver for the Matrix backend (BL241 P1, Q10.2).
//
// Matrix room IDs (e.g. !abc123:matrix.org) and room aliases (e.g.
// #datawatch:matrix.org) are both valid as RoomID in config. This resolver
// detects an alias, calls /_matrix/client/v3/directory/room/<alias>, and
// caches the resolved room ID. It re-resolves when the daemon receives an
// m.room.canonical_alias state-change event.
package matrix

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

// aliasResolver caches alias → room ID mappings for one backend instance.
type aliasResolver struct {
	mu      sync.RWMutex
	client  *mautrix.Client
	cache   map[string]id.RoomID // alias string → resolved room ID
}

func newAliasResolver(client *mautrix.Client) *aliasResolver {
	return &aliasResolver{
		client: client,
		cache:  make(map[string]id.RoomID),
	}
}

// Resolve returns the room ID for roomIDOrAlias. If the input is already a
// room ID (starts with '!') it is returned unchanged. Aliases are resolved via
// the homeserver and cached.
func (r *aliasResolver) Resolve(ctx context.Context, roomIDOrAlias string) (id.RoomID, error) {
	if !isAlias(roomIDOrAlias) {
		return id.RoomID(roomIDOrAlias), nil
	}

	r.mu.RLock()
	cached, ok := r.cache[roomIDOrAlias]
	r.mu.RUnlock()
	if ok {
		return cached, nil
	}

	resp, err := r.client.ResolveAlias(ctx, id.RoomAlias(roomIDOrAlias))
	if err != nil {
		return "", fmt.Errorf("resolve alias %q: %w", roomIDOrAlias, err)
	}
	resolved := resp.RoomID

	r.mu.Lock()
	r.cache[roomIDOrAlias] = resolved
	r.mu.Unlock()

	return resolved, nil
}

// Invalidate drops a cached alias so the next call re-resolves from the
// homeserver. Called when an m.room.canonical_alias state change arrives.
func (r *aliasResolver) Invalidate(alias string) {
	r.mu.Lock()
	delete(r.cache, alias)
	r.mu.Unlock()
}

// isAlias returns true when s looks like a Matrix room alias (#name:server).
func isAlias(s string) bool {
	return strings.HasPrefix(s, "#")
}
