// The message channels the check tells the operator on.

package delivery

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

// Channel is one message channel: its backend name and a send that returns
// what the backend answered.
type Channel struct {
	Name string
	Send func(text string) error
}

// tellingBackends are the backends whose accepted send reaches a person, by
// the name each backend reports. It lists what can tell rather than what
// cannot: the webhook, github and dns backends only receive and their send
// succeeds without sending anything, and a backend added later must not count
// as telling until someone has checked it.
var tellingBackends = map[string]bool{
	"signal":   true,
	"telegram": true,
	"discord":  true,
	"slack":    true,
	"twilio":   true,
	"ntfy":     true,
	"email":    true,
	"imap_mcp": true,
	"matrix":   true,
}

// CanTell reports whether a backend's accepted send reaches a person. Some
// backends only receive, and their send succeeds without sending anything.
func CanTell(backend string) bool { return tellingBackends[backend] }

// channelBackend is the backend of a channel name: a second channel of one
// backend is named "backend#2".
func channelBackend(name string) string {
	base, n, numbered := strings.Cut(name, "#")
	if numbered && (n == "" || strings.Trim(n, "0123456789") != "") {
		return name
	}
	return base
}

// A send returns one of these and never the backend's own error, which can
// carry credentials or content.
var (
	errSendFailed  = errors.New("the channel did not accept the message")
	errSendTimeout = errors.New("the channel did not answer in time")
	errChannelGone = errors.New("no such message channel")
)

// NewChannelSender is a Sender over the channels list returns, leaving out
// those that cannot tell.
func NewChannelSender(list func() []Channel) Sender {
	return channelSender{list: list, stuck: &stuckSends{names: map[string]bool{}}}
}

type channelSender struct {
	list  func() []Channel
	stuck *stuckSends
}

// stuckSends are the channels with a send that timed out and has not returned.
// Most backends take no deadline, so such a call cannot be ended; no second
// one is started on that channel until it returns.
type stuckSends struct {
	mu    sync.Mutex
	names map[string]bool
}

func (c channelSender) usable() []Channel {
	if c.list == nil {
		return nil
	}
	var out []Channel
	seen := map[string]bool{}
	for _, ch := range c.list() {
		if ch.Send == nil || !CanTell(channelBackend(ch.Name)) || seen[ch.Name] {
			continue
		}
		seen[ch.Name] = true
		out = append(out, ch)
	}
	return out
}

func (c channelSender) Channels() []string {
	var out []string
	for _, ch := range c.usable() {
		out = append(out, ch.Name)
	}
	return out
}

func (c channelSender) Send(ctx context.Context, channel, text string) error {
	for _, ch := range c.usable() {
		if ch.Name != channel {
			continue
		}
		return c.call(ctx, ch, text)
	}
	return errChannelGone
}

func (c channelSender) call(ctx context.Context, ch Channel, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.stuck.mu.Lock()
	hung := c.stuck.names[ch.Name]
	c.stuck.mu.Unlock()
	if hung {
		return errSendTimeout
	}
	returned := false // guarded by c.stuck.mu
	err := boundedSend(ctx, notifySendTimeout, func() error {
		defer func() {
			c.stuck.mu.Lock()
			returned = true
			delete(c.stuck.names, ch.Name)
			c.stuck.mu.Unlock()
		}()
		return ch.Send(text)
	})
	if err != nil {
		// Given up on, by time or because the pass ended, and still running.
		c.stuck.mu.Lock()
		if !returned {
			c.stuck.names[ch.Name] = true
		}
		c.stuck.mu.Unlock()
	}
	return err
}

// boundedSend runs send and waits for it at most wait, or until ctx ends. A
// send that is still running is left to return on its own.
func boundedSend(ctx context.Context, wait time.Duration, send func() error) error {
	limit := time.NewTimer(wait)
	defer limit.Stop()
	done := make(chan error, 1)
	go func() {
		err := errSendFailed
		// A panic in a backend is a failed send, not the end of the daemon.
		defer func() {
			_ = recover()
			done <- err
		}()
		err = plainSendError(send())
	}()
	select {
	case err := <-done:
		return err
	case <-limit.C:
		return errSendTimeout
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errSendTimeout
		}
		return ctx.Err()
	}
}

// plainSendError replaces a backend's error with one that holds none of its
// text.
func plainSendError(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{errSendTimeout, errChannelGone, context.Canceled} {
		if errors.Is(err, known) {
			return known
		}
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return errSendTimeout
	}
	return errSendFailed
}
