// Wiring for the shared Automata tag registry (internal/tagregistry). The
// registry is built once at boot from autonomous.tags, and the HTTP server
// serves its routes. A standalone or keeper daemon applies tag writes to its own
// registry. A peer forwards writes to the keeper that autonomous.tags.registry_keeper
// names, and its sync loop pulls the keeper's snapshot. The keeper's name and
// sync interval can change at runtime; PUT /api/config reconciles the registry.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/tagregistry"
)

// tagRegistryCacheFile is the file, in the data directory, that holds a peer's
// copy of the registry between restarts.
const tagRegistryCacheFile = "tag-registry.json"

// wireTagRegistry builds the tag registry, hands it to the HTTP server and
// starts its sync loop. The loop runs only while this daemon is a peer; on a
// standalone or keeper daemon the start returns at once. ctx stops the loop at
// shutdown.
func wireTagRegistry(ctx context.Context, cfg *config.Config, amgr *autonomouspkg.Manager, httpServer *server.HTTPServer) {
	eff := cfg.Autonomous.Tags.Effective()
	var reg *tagregistry.Registry
	client := &tagregistry.HTTPKeeperClient{
		Peers: func(name string) (string, string, bool) {
			st := httpServer.ServerStore()
			if st == nil {
				return "", "", false
			}
			return st.GetByName(name)
		},
		Keeper: func() string { return reg.KeeperName() },
	}
	dataDir := expandHome(cfg.DataDir)
	r, err := tagregistry.New(tagregistry.Options{
		NodeName:   cfg.Hostname,
		KeeperName: eff.RegistryKeeper,
		SyncEvery:  time.Duration(eff.RegistrySyncSeconds) * time.Second,
		Store:      tagregistry.AutonomousStore{Book: amgr},
		Keeper:     client,
		Cache:      tagregistry.FileCache{Path: filepath.Join(dataDir, tagRegistryCacheFile)},
		OnSnapshotChange: func() {
			httpServer.BroadcastTagUpdate(autonomouspkg.TagEvent{Kind: autonomouspkg.TagEventRegistrySynced})
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warn] tag registry: %v\n", err)
		return
	}
	reg = r
	startSync := func() { go reg.Run(ctx) }
	httpServer.SetTagRegistry(reg, startSync)
	startSync()
}
