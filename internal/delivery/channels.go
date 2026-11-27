// The message channels the check tells the operator on.

package delivery

import (
	"context"
	"errors"
)

// Channel is one message channel: its backend name and a send that returns
// what the backend answered.
type Channel struct {
	Name string
	Send func(text string) error
}

// CanTell reports whether a backend's accepted send reaches a person. Some
// backends only receive, and their send succeeds without sending anything.
func CanTell(backend string) bool { return false }

// NewChannelSender is a Sender over the channels list returns, leaving out
// those that cannot tell.
func NewChannelSender(list func() []Channel) Sender { return channelSender{list: list} }

type channelSender struct{ list func() []Channel }

func (c channelSender) Channels() []string { return nil }

func (c channelSender) Send(ctx context.Context, channel, text string) error {
	return errors.New("not built")
}
