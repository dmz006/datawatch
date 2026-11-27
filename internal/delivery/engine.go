package delivery

import (
	"context"
	"sync"
	"time"
)

// DefaultInterval is how often the check runs.
const DefaultInterval = 5 * time.Minute

// Options configure an Engine.
type Options struct {
	// Dir holds the delivery records and the heartbeat.
	Dir string
	// Key seals the records; nil on a non-secure install.
	Key      []byte
	Now      func() time.Time
	Interval time.Duration
	Host     string
	Sources  []Source
	Sender   Sender
	// Notify false means no message is sent; the check then says it cannot
	// tell anyone. Nil means true.
	Notify func() bool
	// Slots are the next fire times of pending schedules, saved in the start
	// mark.
	Slots func() map[string]time.Time

	// AdoptPlain lets plain records be read once and sealed: secure mode was
	// just turned on and the store key is new.
	AdoptPlain bool

	// writeFile replaces the durable write in tests (a full disk).
	writeFile func(path string, data, key []byte) error
}

// Engine runs the check and keeps the list.
type Engine struct {
	opts Options

	// passMu lets one pass run at a time. mu guards what follows and is never
	// held while a source is read or a message is sent.
	passMu sync.Mutex
	mu     sync.Mutex

	rec  *records
	mark *startMark
	// markDirty: the start mark has to be written with the next save.
	markDirty bool
	// broken are the reasons the saved records cannot be used. While any is
	// set the check does nothing.
	broken       []string
	cannotRecord bool
	// warned: the one warning from memory has been sent for this fault.
	warned bool
}

// New returns an engine over the records in opts.Dir. It writes nothing:
// records are written by the first pass, so a daemon start that fails before
// it leaves them as they were.
func New(opts Options) (*Engine, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	if opts.writeFile == nil {
		opts.writeFile = writeRecordFile
	}
	e := &Engine{opts: opts}
	e.load()
	return e, nil
}

// Interval is the time between checks.
func (e *Engine) Interval() time.Duration { return e.opts.Interval }

type sourceRead struct {
	name  string
	cands []Candidate
	err   error
}

func (e *Engine) read(ctx context.Context, now time.Time) []sourceRead {
	out := make([]sourceRead, 0, len(e.opts.Sources))
	for _, s := range e.opts.Sources {
		r := sourceRead{name: s.Name}
		r.cands, r.err = collect(ctx, s, now)
		out = append(out, r)
	}
	return out
}

// Pass runs the check once. The records are saved before anything is sent. It
// returns ErrRecords when the saved records cannot be used and
// ErrCannotRecord when they cannot be saved; no item is sent then.
func (e *Engine) Pass(ctx context.Context) (Report, error) {
	e.passMu.Lock()
	defer e.passMu.Unlock()

	e.mu.Lock()
	if len(e.broken) > 0 {
		e.load() // the operator may have repaired them
	}
	broken := len(e.broken) > 0
	e.mu.Unlock()
	if broken {
		e.warn(ctx)
		return e.Report(), ErrRecords
	}

	now := e.opts.Now()
	reads := e.read(ctx, now)

	e.mu.Lock()
	next, mark := e.rec.clone(), e.mark.clone()
	markDirty := e.markDirty
	if mark == nil {
		mark = &startMark{Version: recordsVersion, At: now, Sources: map[string]*sourceMark{}}
		if e.opts.Slots != nil {
			mark.Slots = e.opts.Slots()
		}
		markDirty = true
	}
	if e.apply(next, mark, reads, now) {
		markDirty = true
	}
	err := e.persistLocked(next, mark, markDirty)
	if err == nil {
		err = e.diskTest(now)
	}
	if err != nil {
		e.cannotRecord = true
		e.mu.Unlock()
		e.warn(ctx)
		return e.Report(), ErrCannotRecord
	}
	e.cannotRecord, e.warned = false, false

	var notice Notice
	due := false
	if e.notifyOn() && e.opts.Sender != nil {
		notice, due = DueNotice(e.opts.Host, e.rec.pointers(), now)
	}
	e.mu.Unlock()

	if due {
		results := Deliver(ctx, e.opts.Sender, notice.Text, now)
		e.mu.Lock()
		told := e.rec.clone()
		ApplySend(told.pointers(), notice, results, now)
		if len(results) > 0 {
			told.AllFailed = !Told(results)
		}
		// Kept in memory even when the save fails, so it is not sent again.
		err = e.persistLocked(told, e.mark, false)
		e.rec = told
		if err != nil {
			e.cannotRecord = true
		}
		e.mu.Unlock()
		if err != nil {
			e.warn(ctx)
			return e.Report(), ErrCannotRecord
		}
	}

	rep := e.Report()
	if err := e.writeHeartbeat(rep, now); err != nil {
		e.mu.Lock()
		e.cannotRecord = true
		e.mu.Unlock()
		e.warn(ctx)
		return e.Report(), ErrCannotRecord
	}
	return rep, nil
}

// warn sends the one content-free warning kept in memory, once for each time
// the records stop working.
func (e *Engine) warn(ctx context.Context) {
	e.mu.Lock()
	send := !e.warned && e.notifyOn() && e.opts.Sender != nil
	if send {
		e.warned = true
	}
	e.mu.Unlock()
	if send {
		Deliver(ctx, e.opts.Sender, CannotRecordText(e.opts.Host), e.opts.Now())
	}
}

func (e *Engine) notifyOn() bool { return e.opts.Notify == nil || e.opts.Notify() }

// TestMessage sends a test message on every channel and says what each
// answered.
func (e *Engine) TestMessage(ctx context.Context) []SendResult {
	return Deliver(ctx, e.opts.Sender, TestText(e.opts.Host), e.opts.Now())
}

// Run passes at once and then every interval, and sooner when a failed send is
// due another try, until ctx ends. report is told about each pass.
func (e *Engine) Run(ctx context.Context, report func(Report, error)) {
	tick := time.NewTicker(runTick)
	defer tick.Stop()
	var last time.Time
	for {
		now := e.opts.Now()
		if last.IsZero() || now.Sub(last) >= e.opts.Interval || now.Before(last) || e.retryDue(now) {
			r, err := e.Pass(ctx)
			last = now
			if report != nil {
				report(r, err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// runTick is how often Run looks whether a pass is due.
const runTick = 30 * time.Second

func (e *Engine) retryDue(now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.broken) > 0 || e.cannotRecord || !e.notifyOn() {
		return false
	}
	for _, en := range e.rec.Entries {
		if en.ClosedAt == nil && en.NextTryAt != nil && !now.Before(*en.NextTryAt) && en.SeenAt == nil {
			return true
		}
	}
	return false
}
