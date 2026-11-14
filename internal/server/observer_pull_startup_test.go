package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// The collector must be built and started by the production Start path, and
// stopped when the daemon's context ends.
func TestHTTPServerStart_StartsAndStopsObserverPull(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	cfg := &config.Config{}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port
	h := New(&cfg.Server, cfg, "", t.TempDir(), nil, "test", nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Start(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for h.api.observerPullCollector() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	col := h.api.observerPullCollector()
	if col == nil {
		t.Fatal("observer pull collector not built by Start")
	}
	if !col.Running() {
		t.Fatal("collector not running")
	}

	cancel()
	select {
	case err := <-done:
		_ = err
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
	deadline = time.Now().Add(5 * time.Second)
	for col.Running() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if col.Running() {
		t.Fatal("collector still running after shutdown")
	}
}
