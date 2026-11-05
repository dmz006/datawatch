package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
	"github.com/dmz006/datawatch/internal/trace/adapters"
)

// TraceSink receives finished trace events; *trace.Store satisfies it.
type TraceSink interface {
	Append(events ...trace.Event) (int, error)
}

// DefaultTraceInterval is how often running sessions are re-read for new
// transcript lines when TraceIngestConfig.Interval is zero.
const DefaultTraceInterval = 5 * time.Second

const defaultTraceQueue = 256

// TraceIngestConfig configures a TraceIngestor.
type TraceIngestConfig struct {
	Sink     TraceSink
	Settings trace.Settings
	// Registry supplies the per-backend transcript adapters; nil reads
	// adapters.DefaultRegistry.
	Registry *adapters.Registry
	// Locate returns the transcript path of a session, or "" when the
	// backend leaves none (the daemon-observed fallback is used then). Nil
	// reads DefaultTranscriptLocator.
	Locate func(Session) string
	// Timeline appends one line to the session's existing timeline. Each
	// event is appended as it is stored. Nil disables the timeline copy.
	Timeline func(sessionID, line string)
	// Now is the clock for events without a timestamp; nil reads the wall
	// clock. Tests inject a fixed clock.
	Now func() time.Time
	// Interval is the incremental ingest period of Run; 0 reads
	// DefaultTraceInterval.
	Interval time.Duration
	// Queue bounds pending work; 0 reads 256. A full queue drops the work
	// and counts it (Dropped) rather than blocking the caller.
	Queue int
}

type traceJob struct {
	kind  int // jobIngest, jobFinal, jobEvent
	sess  Session
	event trace.Event
	done  chan struct{} // non-nil: closed once the job (and all before it) ran
}

const (
	jobIngest = iota
	jobFinal
	jobEvent
	jobBarrier
)

type traceSessionState struct {
	sess    Session
	adapter adapters.Adapter
	ctx     *adapters.Context
	emitted int
}

// TraceIngestor feeds a session's trace events into the trace store and
// the session timeline. All parsing, storing and timeline writes happen on
// one background goroutine; callers only enqueue and never wait, so a slow
// disk or a huge transcript cannot stall a session. It only observes.
type TraceIngestor struct {
	cfg      TraceIngestConfig
	settings trace.Settings
	jobs     chan traceJob
	dropped  atomic.Int64

	mu     sync.Mutex
	active map[string]Session // running sessions polled by Run

	state map[string]*traceSessionState // worker-owned

	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
	wg        sync.WaitGroup
}

// NewTraceIngestor returns an ingestor; call Start to run its worker.
func NewTraceIngestor(cfg TraceIngestConfig) *TraceIngestor {
	if cfg.Registry == nil {
		cfg.Registry = adapters.DefaultRegistry()
	}
	if cfg.Locate == nil {
		cfg.Locate = DefaultTranscriptLocator("")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultTraceInterval
	}
	if cfg.Queue <= 0 {
		cfg.Queue = defaultTraceQueue
	}
	return &TraceIngestor{
		cfg:      cfg,
		settings: cfg.Settings.WithDefaults(),
		jobs:     make(chan traceJob, cfg.Queue),
		active:   map[string]Session{},
		state:    map[string]*traceSessionState{},
		stop:     make(chan struct{}),
	}
}

// Start launches the worker. Run drives incremental ingest on top of it.
func (t *TraceIngestor) Start() {
	t.startOnce.Do(func() {
		t.wg.Add(1)
		go t.work()
	})
}

// Close stops the worker after it drains queued work.
func (t *TraceIngestor) Close() {
	t.stopOnce.Do(func() { close(t.stop) })
	t.wg.Wait()
}

// Dropped reports how many pieces of work a full queue discarded.
func (t *TraceIngestor) Dropped() int64 { return t.dropped.Load() }

func (t *TraceIngestor) enabled() bool {
	return t != nil && t.cfg.Sink != nil && t.settings.Mode != trace.ModeOff
}

func (t *TraceIngestor) enqueue(j traceJob) bool {
	select {
	case t.jobs <- j:
		return true
	default:
		t.dropped.Add(1)
		return false
	}
}

// SessionStarted registers a running session for incremental ingest.
func (t *TraceIngestor) SessionStarted(s Session) {
	if !t.enabled() {
		return
	}
	t.mu.Lock()
	t.active[s.FullID] = s
	t.mu.Unlock()
}

// SessionEnded runs the final ingest for a session that reached a
// terminal state and stops polling it.
func (t *TraceIngestor) SessionEnded(s Session) {
	if !t.enabled() {
		return
	}
	t.mu.Lock()
	delete(t.active, s.FullID)
	t.mu.Unlock()
	t.enqueue(traceJob{kind: jobFinal, sess: s})
}

// Poll queues one incremental ingest for every running session. Run calls
// it on its interval; tests call it directly.
func (t *TraceIngestor) Poll() {
	if !t.enabled() {
		return
	}
	t.mu.Lock()
	running := make([]Session, 0, len(t.active))
	for _, s := range t.active {
		running = append(running, s)
	}
	t.mu.Unlock()
	for _, s := range running {
		t.enqueue(traceJob{kind: jobIngest, sess: s})
	}
}

// Run polls running sessions until ctx ends. It starts the worker.
func (t *TraceIngestor) Run(ctx context.Context) {
	t.Start()
	tick := time.NewTicker(t.cfg.Interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.stop:
			return
		case <-tick.C:
			t.Poll()
		}
	}
}

// RecordEvent stores one event of a session (a capability denial, a
// permission decision, a call made with the session's identity) and appends
// it to the session timeline. It never blocks and never fails the caller.
func (t *TraceIngestor) RecordEvent(sessionID string, ev trace.Event) {
	if !t.enabled() || sessionID == "" {
		return
	}
	ev.SessionID = sessionID
	t.enqueue(traceJob{kind: jobEvent, event: ev})
}

// Flush waits until everything queued before the call has been handled.
func (t *TraceIngestor) Flush() {
	if t == nil {
		return
	}
	done := make(chan struct{})
	select {
	case t.jobs <- traceJob{kind: jobBarrier, done: done}:
	case <-t.stop:
		return
	}
	select {
	case <-done:
	case <-t.stop:
	}
}

func (t *TraceIngestor) work() {
	defer t.wg.Done()
	for {
		select {
		case j := <-t.jobs:
			t.handle(j)
		case <-t.stop:
			for {
				select {
				case j := <-t.jobs:
					t.handle(j)
				default:
					return
				}
			}
		}
	}
}

func (t *TraceIngestor) now() time.Time {
	if t.cfg.Now != nil {
		return t.cfg.Now().UTC()
	}
	return time.Now().UTC()
}

func (t *TraceIngestor) handle(j traceJob) {
	defer func() { _ = recover() }() // an adapter bug must not take the daemon down
	switch j.kind {
	case jobBarrier:
		close(j.done)
	case jobEvent:
		t.recordEvent(j.event)
	case jobIngest:
		t.ingest(j.sess, false)
	case jobFinal:
		t.ingest(j.sess, true)
		delete(t.state, j.sess.FullID)
	}
}

func backendOf(s Session) string {
	if s.BackendFamily != "" {
		return s.BackendFamily
	}
	return s.LLMRef
}

func (t *TraceIngestor) stateFor(s Session) *traceSessionState {
	st := t.state[s.FullID]
	if st == nil {
		st = &traceSessionState{}
		t.state[s.FullID] = st
	}
	st.sess = s
	if st.ctx == nil {
		st.ctx = &adapters.Context{SessionID: s.FullID, TaskID: s.TaskID, StoryID: s.StoryID,
			AutomatonID: s.PRDID, Now: t.cfg.Now}
	}
	if st.adapter == nil {
		if a, ok := t.cfg.Registry.For(backendOf(s), t.settings); ok {
			st.adapter = a
		}
	}
	return st
}

func (t *TraceIngestor) ingest(s Session, final bool) {
	st := t.stateFor(s)
	if st.adapter == nil {
		return // tracing is off for this backend, or no adapter exists
	}
	var evs []trace.Event
	if path := t.cfg.Locate(s); path != "" {
		evs, _ = adapters.ParseFile(st.adapter, path, st.ctx)
	}
	if final && st.emitted == 0 && len(evs) == 0 {
		evs = adapters.FallbackEvents(st.adapter.Name(),
			[]adapters.Observation{{State: string(s.State), At: t.now()}}, st.ctx)
	}
	st.emitted += t.store(evs)
}

func (t *TraceIngestor) recordEvent(ev trace.Event) {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = t.now()
	}
	if st := t.state[ev.SessionID]; st != nil {
		fillIdentity(&ev, st.sess)
	} else {
		t.mu.Lock()
		if s, ok := t.active[ev.SessionID]; ok {
			fillIdentity(&ev, s)
		}
		t.mu.Unlock()
	}
	t.store([]trace.Event{ev})
}

func fillIdentity(ev *trace.Event, s Session) {
	if ev.TaskID == "" {
		ev.TaskID = s.TaskID
	}
	if ev.StoryID == "" {
		ev.StoryID = s.StoryID
	}
	if ev.AutomatonID == "" {
		ev.AutomatonID = s.PRDID
	}
	if ev.Backend == "" {
		ev.Backend = backendOf(s)
	}
}

// store appends events to the trace store and, for each, one line to the
// session timeline. A store error drops the batch: tracing is best effort.
func (t *TraceIngestor) store(evs []trace.Event) int {
	if len(evs) == 0 {
		return 0
	}
	if _, err := t.cfg.Sink.Append(evs...); err != nil {
		return 0
	}
	if t.cfg.Timeline != nil {
		for _, ev := range evs {
			t.cfg.Timeline(ev.SessionID, TimelineLine(ev))
		}
	}
	return len(evs)
}

// TimelineLine renders an event as one timeline.md line in the existing
// "timestamp | event-type | details" format.
func TimelineLine(ev trace.Event) string {
	var b strings.Builder
	b.WriteString(ev.Timestamp.UTC().Format(time.RFC3339))
	b.WriteString(" | trace-")
	b.WriteString(string(ev.Kind))
	b.WriteString(" | ")
	b.WriteString(timelineField(ev.Name))
	if ev.Outcome != "" {
		b.WriteString(" | outcome=" + timelineField(truncateStr(ev.Outcome, 80)))
	}
	return b.String()
}

// DefaultTranscriptLocator finds a session's transcript. Claude Code
// transcripts live under the Claude projects folder of the session's work
// directory; the newest one written since the session was created is used.
// claudeConfigDir "" reads the home directory's .claude. Other backends
// have no locatable transcript and return "".
func DefaultTranscriptLocator(claudeConfigDir string) func(Session) string {
	return func(s Session) string {
		if backendOf(s) != "claude-code" || s.ProjectDir == "" {
			return ""
		}
		dir := filepath.Join(adapters.ClaudeProjectsDir(claudeConfigDir), adapters.ClaudeProjectDirName(s.ProjectDir))
		entries, err := os.ReadDir(dir)
		if err != nil {
			return ""
		}
		var best string
		var bestMod time.Time
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil || info.ModTime().Before(s.CreatedAt) {
				continue
			}
			if best == "" || info.ModTime().After(bestMod) {
				best, bestMod = filepath.Join(dir, e.Name()), info.ModTime()
			}
		}
		return best
	}
}
