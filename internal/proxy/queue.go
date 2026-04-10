package proxy

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// QueuedCommand is a command destined for an unreachable server.
type QueuedCommand struct {
	ServerName string    `json:"server_name"`
	Text       string    `json:"text"`
	QueuedAt   time.Time `json:"queued_at"`
}

// OfflineQueue buffers commands for unreachable remote servers and replays
// them when the server comes back online.
type OfflineQueue struct {
	mu       sync.Mutex
	queues   map[string][]QueuedCommand // serverName → commands
	maxSize  int                        // max queued commands per server
	pool     *Pool                      // health source
	dispatch func(serverName, text string) ([]string, error)
	onReplay func(serverName string, count int) // notification callback

	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewOfflineQueue creates a queue that replays commands when servers recover.
// dispatchFn is called to forward a command (typically RemoteDispatcher.ForwardCommand).
// onReplayFn is called after replay with the server name and count (for messaging notifications).
func NewOfflineQueue(pool *Pool, maxSize int, dispatchFn func(string, string) ([]string, error), onReplayFn func(string, int)) *OfflineQueue {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &OfflineQueue{
		queues:   make(map[string][]QueuedCommand),
		maxSize:  maxSize,
		pool:     pool,
		dispatch: dispatchFn,
		onReplay: onReplayFn,
		stopCh:   make(chan struct{}),
	}
}

// Enqueue adds a command to the queue for an unreachable server.
// Returns an error if the queue is full.
func (q *OfflineQueue) Enqueue(serverName, text string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	cmds := q.queues[serverName]
	if len(cmds) >= q.maxSize {
		return fmt.Errorf("offline queue full for server %s (%d/%d)", serverName, len(cmds), q.maxSize)
	}
	q.queues[serverName] = append(cmds, QueuedCommand{
		ServerName: serverName,
		Text:       text,
		QueuedAt:   time.Now(),
	})
	log.Printf("[proxy-queue] queued command for %s (%d in queue): %s",
		serverName, len(q.queues[serverName]), truncate(text, 60))
	return nil
}

// Pending returns the number of queued commands for a server.
func (q *OfflineQueue) Pending(serverName string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queues[serverName])
}

// PendingAll returns queued command counts per server.
func (q *OfflineQueue) PendingAll() map[string]int {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make(map[string]int, len(q.queues))
	for name, cmds := range q.queues {
		if len(cmds) > 0 {
			result[name] = len(cmds)
		}
	}
	return result
}

// Start begins the background replay loop that drains queues when servers recover.
func (q *OfflineQueue) Start() {
	go q.replayLoop()
}

// Stop terminates the replay loop.
func (q *OfflineQueue) Stop() {
	q.stopOnce.Do(func() { close(q.stopCh) })
}

func (q *OfflineQueue) replayLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-q.stopCh:
			return
		case <-ticker.C:
			q.tryReplay()
		}
	}
}

func (q *OfflineQueue) tryReplay() {
	q.mu.Lock()
	// Collect servers that have queued commands and are now healthy
	var ready []string
	for name, cmds := range q.queues {
		if len(cmds) > 0 && q.pool.IsHealthy(name) {
			ready = append(ready, name)
		}
	}
	q.mu.Unlock()

	for _, name := range ready {
		q.replayServer(name)
	}
}

func (q *OfflineQueue) replayServer(serverName string) {
	q.mu.Lock()
	cmds := q.queues[serverName]
	if len(cmds) == 0 {
		q.mu.Unlock()
		return
	}
	// Take all commands and clear the queue
	toReplay := make([]QueuedCommand, len(cmds))
	copy(toReplay, cmds)
	q.queues[serverName] = nil
	q.mu.Unlock()

	replayed := 0
	for _, cmd := range toReplay {
		_, err := q.dispatch(cmd.ServerName, cmd.Text)
		if err != nil {
			log.Printf("[proxy-queue] replay failed for %s: %v — re-queuing remaining", serverName, err)
			// Re-queue this and remaining commands
			q.mu.Lock()
			remaining := toReplay[replayed:]
			q.queues[serverName] = append(q.queues[serverName], remaining...)
			q.mu.Unlock()
			return
		}
		replayed++
	}

	if replayed > 0 {
		log.Printf("[proxy-queue] replayed %d commands for %s", replayed, serverName)
		if q.onReplay != nil {
			q.onReplay(serverName, replayed)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
