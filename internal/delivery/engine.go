package delivery

import (
	"context"
	"errors"
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
	// tell anyone.
	Notify func() bool

	// writeFile replaces the durable write in tests (a full disk).
	writeFile func(path string, data []byte) error
}

// Engine runs the check and keeps the list.
type Engine struct{ opts Options }

// New returns an engine over the records in opts.Dir.
func New(opts Options) (*Engine, error) { return &Engine{opts: opts}, nil }

var errNotBuilt = errors.New("not built")

// Pass runs the check once.
func (e *Engine) Pass(ctx context.Context) (Report, error) { return Report{}, errNotBuilt }

// Report returns the current report without running a pass.
func (e *Engine) Report() Report { return Report{} }

// Ack marks entries as seen by by.
func (e *Engine) Ack(sel Selector, by string) (int, error) { return 0, errNotBuilt }

// Accept lets entries wait until until for the reason code.
func (e *Engine) Accept(sel Selector, reason string, until time.Time, by string) (int, error) {
	return 0, errNotBuilt
}

// Drop closes failed or backlog entries for the reason code.
func (e *Engine) Drop(sel Selector, reason, by string) (int, error) { return 0, errNotBuilt }

// TestMessage sends a test message to every channel.
func (e *Engine) TestMessage(ctx context.Context) []SendResult { return nil }

// Record saves a failure a source cannot be asked about later.
func (e *Engine) Record(c Candidate) error { return errNotBuilt }
