package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/dmz006/datawatch/internal/channel"
)

// The startup cleanup of old channel registrations in claude's MCP list runs
// beside the start: `claude mcp list` connects to every configured MCP server,
// so one unreachable server holds it for claude's whole connect timeout.
// Sessions can start or resume while it runs; the channel package keeps their
// registrations safe (see channel.CleanupStaleRegistrations).
var channelMCPCleanup struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func startChannelMCPCleanup(usingGoBridge bool, sessionExists func(string) bool) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	channelMCPCleanup.mu.Lock()
	channelMCPCleanup.cancel, channelMCPCleanup.done = cancel, done
	channelMCPCleanup.mu.Unlock()
	go func() {
		defer close(done)
		defer func() {
			if p := recover(); p != nil {
				fmt.Printf("[channel] MCP registration cleanup panic (recovered): %v\n", p)
			}
		}()
		channel.CleanupStaleRegistrations(ctx, usingGoBridge, sessionExists)
	}()
}

// stopChannelMCPCleanup ends a running cleanup, and its claude call, and waits
// for it. It is safe at any time and more than once. Every path that ends or
// replaces the daemon calls it: syscall.Exec and os.Exit run no deferred call.
func stopChannelMCPCleanup() {
	channelMCPCleanup.mu.Lock()
	cancel, done := channelMCPCleanup.cancel, channelMCPCleanup.done
	channelMCPCleanup.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}
