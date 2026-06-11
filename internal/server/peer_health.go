// BL343 — background federation peer health monitor.
//
// Periodically pings every federated peer's /api/health endpoint and fires a
// system alert when a peer transitions from reachable → unreachable (and a
// recovery alert on the reverse). The interval defaults to 10 minutes.
//
// Wire-up: call StartPeerHealthMonitor after both serverStore and alertStore
// are set on the Server. The goroutine exits when ctx is cancelled.

package server

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

const peerHealthInterval = 10 * time.Minute

// StartPeerHealthMonitor launches the background loop. It is a no-op when
// either store is nil (e.g. in unit tests that only wire part of the stack).
func StartPeerHealthMonitor(ctx context.Context, store *multiserver.Store, alertStore *alerts.Store) {
	if store == nil || alertStore == nil {
		return
	}
	go runPeerHealthMonitor(ctx, store, alertStore)
}

func runPeerHealthMonitor(ctx context.Context, store *multiserver.Store, alertStore *alerts.Store) {
	// Jitter the first tick up to 60s so multiple daemons started at the same
	// time don't all probe at the same instant.
	jitter := time.Duration(rand.Intn(60)) * time.Second //nolint:gosec
	select {
	case <-ctx.Done():
		return
	case <-time.After(jitter):
	}

	ticker := time.NewTicker(peerHealthInterval)
	defer ticker.Stop()

	// reachable tracks last-known state per peer name (true = up).
	// Start with all peers unknown (absent from map = never checked yet).
	reachable := map[string]bool{}

	check := func() {
		peers := store.ListFederated()
		for _, peer := range peers {
			tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, _, err := store.Test(tctx, peer.Name)
			cancel()

			up := err == nil
			prev, seen := reachable[peer.Name]
			reachable[peer.Name] = up

			if !seen {
				// First check: record state silently, don't alert on the initial probe.
				continue
			}
			if prev == up {
				continue // state unchanged
			}
			if !up {
				alertStore.AddSystem(alerts.LevelWarn,
					fmt.Sprintf("Federation peer unreachable: %s", peer.Name),
					fmt.Sprintf("Peer %q (%s) failed health check: %v\n\nIf the peer's IP changed, update its URL with:\n  datawatch server update %s --url <new-url>\n\nTip: use a Tailscale MagicDNS hostname to survive IP churn.", peer.Name, peer.URL, err, peer.Name),
				)
			} else {
				alertStore.AddSystem(alerts.LevelInfo,
					fmt.Sprintf("Federation peer recovered: %s", peer.Name),
					fmt.Sprintf("Peer %q (%s) is reachable again.", peer.Name, peer.URL),
				)
			}
		}
	}

	check()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}
