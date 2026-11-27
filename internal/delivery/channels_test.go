package delivery

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChannelCanTell(t *testing.T) {
	// The names are what each backend's Name() returns.
	for _, name := range []string{"signal", "telegram", "discord", "slack", "twilio", "ntfy", "email", "imap_mcp", "matrix"} {
		if !CanTell(name) {
			t.Errorf("%s reaches a person", name)
		}
	}
	// webhook, github and dns only receive: their Send returns no error and
	// sends nothing. "test" is the capture backend. An unknown name is not
	// trusted to reach anyone.
	for _, name := range []string{"webhook", "github", "dns", "test", "", "carrier-pigeon"} {
		if CanTell(name) {
			t.Errorf("%q must not count as telling", name)
		}
	}
}

func TestChannelSenderLeavesOutThoseThatCannotTell(t *testing.T) {
	var sent []string
	ok := func(name string) Channel {
		return Channel{Name: name, Send: func(text string) error { sent = append(sent, name+":"+text); return nil }}
	}
	s := NewChannelSender(func() []Channel {
		return []Channel{ok("webhook"), ok("github"), ok("dns"), ok("test"), ok("ntfy"), {Name: "email"}, ok("signal"), ok("ntfy")}
	})
	if got := strings.Join(s.Channels(), ","); got != "ntfy,signal" {
		t.Fatalf("channels=%s", got)
	}
	if err := s.Send(context.Background(), "signal", "hi"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"webhook", "dns", "email", "gone"} {
		if err := s.Send(context.Background(), name, "hi"); err == nil {
			t.Fatalf("a send on %s reported success", name)
		}
	}
	if got := strings.Join(sent, ","); got != "signal:hi" {
		t.Fatalf("sent=%s", got)
	}
}

func TestChannelSenderWithOnlyReceiversHasNoChannel(t *testing.T) {
	s := NewChannelSender(func() []Channel {
		return []Channel{{Name: "webhook", Send: func(string) error { return nil }}}
	})
	if len(s.Channels()) != 0 {
		t.Fatalf("%v", s.Channels())
	}
	if res := Deliver(context.Background(), s, "x", notifyNow); len(res) != 0 || Told(res) {
		t.Fatalf("%+v", res)
	}
}

func TestChannelListIsReadEachTime(t *testing.T) {
	var list []Channel
	s := NewChannelSender(func() []Channel { return list })
	if len(s.Channels()) != 0 {
		t.Fatal("expected none")
	}
	list = []Channel{{Name: "slack", Send: func(string) error { return nil }}}
	if len(s.Channels()) != 1 {
		t.Fatal("a channel added later is missing")
	}
}

func TestChannelErrorTextIsNeverPassedOn(t *testing.T) {
	timeout := &net.OpError{Op: "dial", Err: channelTimeoutErr{}}
	for want, backendErr := range map[string]error{
		sendCodeFailed:  errors.New("post https://api.example/bot123:hunter2/send: 401"),
		sendCodeTimeout: timeout,
	} {
		s := NewChannelSender(func() []Channel {
			return []Channel{{Name: "telegram", Send: func(string) error { return backendErr }}}
		})
		err := s.Send(context.Background(), "telegram", "hi")
		if err == nil || strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "dial") {
			t.Fatalf("%s: %v", want, err)
		}
		res := Deliver(context.Background(), s, "hi", notifyNow)
		if len(res) != 1 || res[0].OK || res[0].Code != want {
			t.Fatalf("%s: %+v", want, res)
		}
	}
}

type channelTimeoutErr struct{}

func (channelTimeoutErr) Error() string   { return "i/o timeout token=hunter2" }
func (channelTimeoutErr) Timeout() bool   { return true }
func (channelTimeoutErr) Temporary() bool { return true }

func TestChannelAPanickingBackendIsAFailedSend(t *testing.T) {
	s := NewChannelSender(func() []Channel {
		return []Channel{{Name: "matrix", Send: func(string) error { panic("nil client") }}}
	})
	res := Deliver(context.Background(), s, "hi", notifyNow)
	if len(res) != 1 || res[0].OK || res[0].Code != sendCodeFailed {
		t.Fatalf("%+v", res)
	}
}

func TestChannelAHungSendTimesOutAndIsNotStartedAgain(t *testing.T) {
	old := notifySendTimeout
	notifySendTimeout = 100 * time.Millisecond
	defer func() { notifySendTimeout = old }()
	release := make(chan struct{})
	var calls atomic.Int32
	s := NewChannelSender(func() []Channel {
		return []Channel{{Name: "email", Send: func(string) error {
			if calls.Add(1) == 1 {
				<-release
			}
			return nil
		}}}
	})
	for i := 0; i < 5; i++ {
		start := time.Now()
		res := Deliver(context.Background(), s, "hi", notifyNow)
		if len(res) != 1 || res[0].OK || res[0].Code != sendCodeTimeout {
			t.Fatalf("send %d: %+v", i, res)
		}
		if time.Since(start) > 3*time.Second {
			t.Fatalf("send %d took %s", i, time.Since(start))
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("%d sends piled up behind a hung one", calls.Load())
	}
	// Once the old call returns, the channel is used again.
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if res := Deliver(context.Background(), s, "hi", notifyNow); Told(res) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the channel never recovered")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
