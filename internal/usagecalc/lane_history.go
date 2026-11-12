package usagecalc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

const (
	// LaneSampleInterval is the longest the history goes without recording a sample while
	// the lane count stays the same.
	LaneSampleInterval = 60 * time.Second

	// LaneLookupWindow is how far before a moment the newest sample may be and still answer
	// a lookup for it.
	LaneLookupWindow = 2 * time.Minute

	// LaneCheckInterval is how often Run asks the counter. A change in the count is recorded
	// at the next check, so it is much shorter than LaneSampleInterval.
	LaneCheckInterval = 10 * time.Second

	// LaneHistorySchemaVersion is the version written to the lane history file.
	LaneHistorySchemaVersion = 1

	laneFileName = "lanes.json"
)

// LaneCounter reports how many Claude lanes are running now. ok is false when no capacity
// ledger is wired, so the count is unknown rather than zero.
type LaneCounter interface {
	Lanes() (count int, ok bool)
}

// ClaudeLanes returns the number of distinct lease holders whose pools include at least one
// of claudePools. A lease with no Claude pool is not counted, and a holder that appears in
// more than one lease is counted once.
func ClaudeLanes(leases []capacity.Lease, claudePools map[string]struct{}) int {
	holders := make(map[string]struct{})
	for _, l := range leases {
		for _, p := range l.Pools {
			if _, ok := claudePools[p]; ok {
				holders[l.Holder] = struct{}{}
				break
			}
		}
	}
	return len(holders)
}

// LaneSample is the Claude lane count at one moment.
type LaneSample struct {
	At    time.Time `json:"at"`
	Count int       `json:"count"`
}

type laneFile struct {
	Version int          `json:"version"`
	Samples []LaneSample `json:"samples"`
}

// LaneHistory keeps the Claude lane count over time, so a usage reading can be matched with
// the lanes that were running when it was taken. It records a sample when the count changes
// and otherwise at most every LaneSampleInterval, keeps samples for ReadingRetention, and
// saves them as lanes.json next to the usage store file.
type LaneHistory struct {
	mu      sync.Mutex
	path    string
	now     func() time.Time
	counter LaneCounter
	logf    func(format string, args ...any)
	tick    time.Duration
	samples []LaneSample
}

// LaneHistoryOption adjusts a LaneHistory at open time.
type LaneHistoryOption func(*LaneHistory)

// WithLaneLogger replaces the default logger, which writes through the standard log package.
func WithLaneLogger(logf func(format string, args ...any)) LaneHistoryOption {
	return func(h *LaneHistory) {
		if logf != nil {
			h.logf = logf
		}
	}
}

// OpenLaneHistory opens the history under dataDir/usagecalc, the directory the usage store
// uses, creating it if needed. now is the injected clock and counter the injected source of
// the current count. A file that cannot be parsed is moved aside with a .bad suffix, one
// error is logged, and the history starts empty.
func OpenLaneHistory(dataDir string, now func() time.Time, counter LaneCounter, opts ...LaneHistoryOption) (*LaneHistory, error) {
	if dataDir == "" {
		return nil, errors.New("usagecalc lane history: data directory is required")
	}
	if now == nil {
		return nil, errors.New("usagecalc lane history: a clock is required")
	}
	if counter == nil {
		return nil, errors.New("usagecalc lane history: a lane counter is required")
	}
	dir := filepath.Join(dataDir, storeDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("usagecalc lane history: mkdir %s: %w", dir, err)
	}
	h := &LaneHistory{
		path:    filepath.Join(dir, laneFileName),
		now:     now,
		counter: counter,
		logf:    log.Printf,
		tick:    LaneCheckInterval,
	}
	for _, opt := range opts {
		opt(h)
	}
	if err := h.load(); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *LaneHistory) load() error {
	raw, err := os.ReadFile(h.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("usagecalc lane history: read %s: %w", h.path, err)
	}
	var f laneFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return h.moveAside(fmt.Sprintf("does not parse: %v", err))
	}
	if f.Version != LaneHistorySchemaVersion {
		return h.moveAside(fmt.Sprintf("has schema version %d, want %d", f.Version, LaneHistorySchemaVersion))
	}
	sort.SliceStable(f.Samples, func(i, j int) bool { return f.Samples[i].At.Before(f.Samples[j].At) })
	h.samples = h.prune(f.Samples)
	return nil
}

// moveAside renames the unreadable file to the first free .bad name and logs once. The file
// is never overwritten or deleted.
func (h *LaneHistory) moveAside(reason string) error {
	dest := h.path + ".bad"
	for i := 1; ; i++ {
		if _, err := os.Lstat(dest); errors.Is(err, os.ErrNotExist) {
			break
		}
		dest = fmt.Sprintf("%s.bad.%d", h.path, i)
	}
	if err := os.Rename(h.path, dest); err != nil {
		return fmt.Errorf("usagecalc lane history: %s %s and could not be moved aside: %w", h.path, reason, err)
	}
	h.logf("usagecalc lane history: %s %s; moved to %s and starting empty", h.path, reason, dest)
	return nil
}

// prune drops samples older than ReadingRetention. The input must be sorted oldest first.
func (h *LaneHistory) prune(in []LaneSample) []LaneSample {
	cutoff := h.now().Add(-ReadingRetention)
	i := sort.Search(len(in), func(i int) bool { return !in[i].At.Before(cutoff) })
	return append([]LaneSample(nil), in[i:]...)
}

// Sample asks the counter for the current count and records it when the count differs from
// the newest sample or that sample is at least LaneSampleInterval old. It reports whether a
// sample was recorded. When the counter has no answer nothing is recorded.
func (h *LaneHistory) Sample() bool {
	count, ok := h.counter.Lanes()
	if !ok || count < 0 {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now().UTC()
	if n := len(h.samples); n > 0 {
		last := h.samples[n-1]
		if last.Count == count && now.Sub(last.At) < LaneSampleInterval {
			return false
		}
	}
	next := append(append([]LaneSample(nil), h.samples...), LaneSample{At: now, Count: count})
	sort.SliceStable(next, func(i, j int) bool { return next[i].At.Before(next[j].At) })
	next = h.prune(next)
	if err := writeFileAtomic(h.path, laneFile{Version: LaneHistorySchemaVersion, Samples: next}); err != nil {
		h.logf("usagecalc lane history: %v; keeping the sample in memory only", err)
	}
	h.samples = next
	return true
}

// Lookup returns the Claude lane count at t: the count of the newest sample at or before t,
// provided that sample is no more than LaneLookupWindow older than t. Otherwise ok is false
// and nothing was recorded for t.
func (h *LaneHistory) Lookup(t time.Time) (count int, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	i := sort.Search(len(h.samples), func(i int) bool { return h.samples[i].At.After(t) })
	if i == 0 {
		return 0, false
	}
	s := h.samples[i-1]
	if t.Sub(s.At) > LaneLookupWindow {
		return 0, false
	}
	return s.Count, true
}

// Samples returns a copy of the recorded samples, oldest first.
func (h *LaneHistory) Samples() []LaneSample {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]LaneSample(nil), h.samples...)
}

// Run samples once at once and then on a ticker until ctx is cancelled.
func (h *LaneHistory) Run(ctx context.Context) {
	h.Sample()
	t := time.NewTicker(h.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Sample()
		}
	}
}
