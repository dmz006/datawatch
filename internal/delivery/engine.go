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
	// unsaved are recorded failures that could not be saved yet; lost counts
	// those there was no room to hold.
	unsaved []heldEvent
	lost    int
	// warned: a channel accepted the warning from memory for this fault. It
	// is cleared only by a whole pass that worked. warnFails and warnNext
	// space the tries until one is accepted.
	warned    bool
	warnFails int
	warnNext  time.Time
	// tests are the times of the test messages of the last hour.
	tests []time.Time
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

// SealAdopted seals, at once, plain records that were read because the store
// key is new. It writes nothing otherwise.
func (e *Engine) SealAdopted() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.broken) > 0 {
		return ErrRecords
	}
	if !e.markDirty {
		return nil
	}
	return e.persistLocked(e.rec, e.mark, true)
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
	e.foldHeldLocked(next, now)
	err := e.persistLocked(next, mark, markDirty)
	if err == nil {
		e.clearHeldLocked()
	}
	if err == nil {
		err = e.diskTest(now)
	}
	if err != nil {
		e.cannotRecord = true
		e.mu.Unlock()
		e.warn(ctx)
		return e.Report(), ErrCannotRecord
	}
	e.cannotRecord = false

	var notice Notice
	due := false
	if e.canSend() {
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
	// The whole pass worked: a later fault is a new one.
	e.mu.Lock()
	e.warned, e.warnFails, e.warnNext = false, 0, time.Time{}
	e.mu.Unlock()
	return rep, nil
}

// warn sends the content-free warning kept in memory: once a channel accepts
// it, no more for this fault. Until then it is tried again, backing off to
// once an hour. Only Pass calls it.
func (e *Engine) warn(ctx context.Context) {
	now := e.opts.Now()
	e.mu.Lock()
	// A next try further off than the longest wait means the clock went back.
	waiting := now.Before(e.warnNext) && e.warnNext.Sub(now) <= notifyRetryMax
	send := !e.warned && !waiting && e.canSend()
	e.mu.Unlock()
	if !send {
		return
	}
	accepted := Told(Deliver(ctx, e.opts.Sender, CannotRecordText(e.opts.Host), now))
	e.mu.Lock()
	defer e.mu.Unlock()
	if accepted {
		e.warned = true
		return
	}
	e.warnFails++
	e.warnNext = now.Add(notifyBackoff(e.warnFails))
}

func (e *Engine) notifyOn() bool { return e.opts.Notify == nil || e.opts.Notify() }

// canSend reports whether there is a channel to send on. With none, nothing
// is counted as a failed send: the check says it cannot tell anyone.
func (e *Engine) canSend() bool {
	return e.notifyOn() && e.opts.Sender != nil && len(e.opts.Sender.Channels()) > 0
}

// TestMessage sends a test message on every channel and says what each
// answered. What they answer is the newest word on whether anyone can be told.
// A session may call it, so it is limited, and refused while messages are off.
// The limit is kept in memory.
func (e *Engine) TestMessage(ctx context.Context) ([]SendResult, error) {
	now := e.opts.Now()
	e.mu.Lock()
	if !e.notifyOn() {
		e.mu.Unlock()
		return nil, ErrNotifyOff
	}
	if !e.canSend() {
		e.mu.Unlock()
		return nil, nil // no channel: nothing is sent and nothing counted
	}
	var kept []time.Time
	for _, at := range e.tests {
		// A time ahead of now means the clock went back; it does not count.
		if since := now.Sub(at); since >= 0 && since < time.Hour {
			kept = append(kept, at)
		}
	}
	e.tests = kept
	if len(kept) >= testsPerHour || (len(kept) > 0 && now.Sub(kept[len(kept)-1]) < time.Minute) {
		e.mu.Unlock()
		return nil, ErrTestTooSoon
	}
	e.tests = append(e.tests, now) // taken before the send, which is slow
	e.mu.Unlock()

	results := Deliver(ctx, e.opts.Sender, TestText(e.opts.Host), now)
	if len(results) > 0 {
		e.mu.Lock()
		e.rec.AllFailed = !Told(results) // saved with the next pass
		e.mu.Unlock()
	}
	return results, nil
}

const (
	// maxUnsaved bounds the failures held while they cannot be saved.
	maxUnsaved = 200
	// testsPerHour bounds the test messages.
	testsPerHour = 6
)

type heldEvent struct {
	c     Candidate
	count int
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
	if len(e.broken) > 0 || e.cannotRecord || !e.canSend() {
		return false
	}
	for _, en := range e.rec.Entries {
		if en.ClosedAt == nil && en.NextTryAt != nil && !now.Before(*en.NextTryAt) && en.SeenAt == nil && en.Accepted == nil {
			return true
		}
	}
	return false
}
