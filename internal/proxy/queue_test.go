package proxy

import (
	"fmt"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestOfflineQueue_Enqueue(t *testing.T) {
	servers := []config.RemoteServerConfig{
		{Name: "s1", URL: "http://localhost:9999", Enabled: true},
	}
	p := NewPool(servers, DefaultPoolConfig())
	q := NewOfflineQueue(p, 3, nil, nil)

	if err := q.Enqueue("s1", "cmd1"); err != nil {
		t.Fatalf("Enqueue 1: %v", err)
	}
	if err := q.Enqueue("s1", "cmd2"); err != nil {
		t.Fatalf("Enqueue 2: %v", err)
	}
	if err := q.Enqueue("s1", "cmd3"); err != nil {
		t.Fatalf("Enqueue 3: %v", err)
	}
	// Queue full
	if err := q.Enqueue("s1", "cmd4"); err == nil {
		t.Error("expected error when queue is full")
	}

	if q.Pending("s1") != 3 {
		t.Errorf("Pending = %d, want 3", q.Pending("s1"))
	}
}

func TestOfflineQueue_PendingAll(t *testing.T) {
	servers := []config.RemoteServerConfig{
		{Name: "a", URL: "http://a", Enabled: true},
		{Name: "b", URL: "http://b", Enabled: true},
	}
	p := NewPool(servers, DefaultPoolConfig())
	q := NewOfflineQueue(p, 100, nil, nil)

	q.Enqueue("a", "x")
	q.Enqueue("a", "y")
	q.Enqueue("b", "z")

	all := q.PendingAll()
	if all["a"] != 2 {
		t.Errorf("Pending(a) = %d, want 2", all["a"])
	}
	if all["b"] != 1 {
		t.Errorf("Pending(b) = %d, want 1", all["b"])
	}
}

func TestOfflineQueue_Replay(t *testing.T) {
	servers := []config.RemoteServerConfig{
		{Name: "s1", URL: "http://localhost:9999", Enabled: true},
	}
	cfg := DefaultPoolConfig()
	cfg.CircuitBreakerThreshold = 1
	cfg.CircuitBreakerReset = 50 * time.Millisecond
	p := NewPool(servers, cfg)

	// Trip breaker so server is down
	p.RecordFailure("s1", fmt.Errorf("down"))

	var replayed []string
	dispatch := func(server, text string) ([]string, error) {
		replayed = append(replayed, text)
		return nil, nil
	}
	var replayNotified bool
	onReplay := func(server string, count int) {
		replayNotified = true
	}

	q := NewOfflineQueue(p, 100, dispatch, onReplay)

	// Enqueue while server is down
	q.Enqueue("s1", "cmd1")
	q.Enqueue("s1", "cmd2")

	// Server recovers
	time.Sleep(60 * time.Millisecond)
	p.RecordSuccess("s1")

	// Manually trigger replay
	q.tryReplay()

	if len(replayed) != 2 {
		t.Errorf("replayed %d commands, want 2", len(replayed))
	}
	if replayed[0] != "cmd1" || replayed[1] != "cmd2" {
		t.Errorf("replayed = %v, want [cmd1, cmd2]", replayed)
	}
	if !replayNotified {
		t.Error("replay notification not fired")
	}
	if q.Pending("s1") != 0 {
		t.Errorf("queue should be empty after replay, got %d", q.Pending("s1"))
	}
}
