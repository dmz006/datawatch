package autonomous

import (
	"fmt"
	"sync"
)

// DefaultNodeUnresponsiveThreshold is how many consecutive model-unresponsive
// failures on one compute node raise the operator alert.
const DefaultNodeUnresponsiveThreshold = 3

// nodeUnresponsiveCounter counts consecutive model-unresponsive failures per
// compute node. It is safe for concurrent use.
type nodeUnresponsiveCounter struct {
	mu        sync.Mutex
	counts    map[string]int
	threshold int
	// alert creates the operator alert; injected so tests use a fake.
	alert func(level, title, body string) string
}

func newNodeUnresponsiveCounter(threshold int, alert func(level, title, body string) string) *nodeUnresponsiveCounter {
	if threshold <= 0 {
		threshold = DefaultNodeUnresponsiveThreshold
	}
	return &nodeUnresponsiveCounter{counts: map[string]int{}, threshold: threshold, alert: alert}
}

// record adds one failure for node and returns the new consecutive count.
// The alert is raised once, when the count reaches the threshold exactly.
func (c *nodeUnresponsiveCounter) record(node, model string) int {
	c.mu.Lock()
	c.counts[node]++
	n := c.counts[node]
	alert := c.alert
	hit := n == c.threshold
	c.mu.Unlock()
	if hit && alert != nil {
		alert("error", "Compute node model unresponsive",
			fmt.Sprintf("Compute node %s, model %s: %d consecutive unresponsive failures.", node, model, n))
	}
	return n
}

// reset clears the count for node after a success.
func (c *nodeUnresponsiveCounter) reset(node string) {
	c.mu.Lock()
	delete(c.counts, node)
	c.mu.Unlock()
}

func (c *nodeUnresponsiveCounter) count(node string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[node]
}

// nodeCounter returns the Manager's counter, creating it on first use with
// the manager's alert sink read at alert time.
func (m *Manager) nodeCounter() *nodeUnresponsiveCounter {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.nodeUnresponsive == nil {
		m.nodeUnresponsive = newNodeUnresponsiveCounter(DefaultNodeUnresponsiveThreshold,
			func(level, title, body string) string {
				if sink := m.AlertSink(); sink != nil {
					return sink(level, title, body)
				}
				return ""
			})
	}
	return m.nodeUnresponsive
}

// RecordNodeUnresponsive counts one model-unresponsive failure on node and
// returns the consecutive count; at the threshold it raises an alert naming
// the node and model.
func (m *Manager) RecordNodeUnresponsive(node, model string) int {
	return m.nodeCounter().record(node, model)
}

// resetNodeUnresponsive ends the node's run of failures after a success.
func (m *Manager) resetNodeUnresponsive(node string) {
	if node != "" {
		m.nodeCounter().reset(node)
	}
}
