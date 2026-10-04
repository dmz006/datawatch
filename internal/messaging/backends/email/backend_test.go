// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-review.md
// §3f, alert #627).
//
// IMPORTANT CORRECTION, found while writing these tests: the original
// write-up classified this as "CONFIRMED REAL" email-header-injection.
// That overstated the practical risk. Verified live (not just read from
// source) that Go's own net/smtp.SendMail already calls validateLine on
// both the envelope `from` and every envelope `to`, rejecting a CRLF in
// either BEFORE dialing at all -- confirmed by timing a call against a
// non-routable address: it returned in ~1 microsecond with
// "smtp: A line must not contain CR or LF", not a dial timeout. Since
// Send() passes the exact same b.from/to strings as BOTH the SMTP
// envelope parameters (which stdlib validates) AND the hand-built header
// block (which it doesn't), the header-injection payload in this
// specific code never reaches the wire -- SendMail already errors out
// first. So this was not a live, exploitable gap as originally
// characterized.
//
// The hasCRLF check added here is kept anyway, as real but narrower
// defense in depth: (1) it fires before the tainted header string is
// even constructed in memory, where stdlib's check fires slightly later
// (after Dial, inside Mail/Rcpt); (2) more importantly, it protects
// against a plausible FUTURE change where the header value diverges from
// the envelope value (e.g. adding a display name like "Alice
// <a@example.com>" to the header while the envelope keeps the bare
// address) -- at that point stdlib's envelope-only validation would no
// longer cover the header string, and this explicit check would be the
// only thing still protecting it.
package email

import (
	"bufio"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

func TestHasCRLF(t *testing.T) {
	cases := map[string]bool{
		"a@example.com":              false,
		"":                           false,
		"a\r\nBcc: evil@x.com":       true,
		"a\nBcc: evil@x.com":         true,
		"a\rBcc: evil@x.com":         true,
		"normal multi\nline is fine": true, // hasCRLF doesn't distinguish context -- callers decide what to check
	}
	for in, want := range cases {
		if got := hasCRLF(in); got != want {
			t.Errorf("hasCRLF(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSend_RejectsCRLFInRecipient_BeforeDialing(t *testing.T) {
	b := &Backend{host: "203.0.113.1", port: 25, from: "a@example.com"} // TEST-NET-3, non-routable
	start := time.Now()
	err := b.Send("evil\r\nBcc: attacker@evil.com", "a normal message")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected an error for a CRLF-bearing recipient")
	}
	// If this ever regresses to actually dialing 203.0.113.1, the test
	// will hang/time out rather than return instantly -- that's the
	// point of using a non-routable address here.
	if elapsed > time.Second {
		t.Errorf("took %v -- expected an instant rejection before any dial attempt, not a network timeout", elapsed)
	}
}

func TestSend_RejectsCRLFInFromAddress_BeforeDialing(t *testing.T) {
	b := &Backend{host: "203.0.113.1", port: 25, from: "a\r\nBcc: attacker@evil.com", to: "legit@example.com"}
	start := time.Now()
	err := b.Send("", "a normal message")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected an error for a CRLF-bearing From address")
	}
	if elapsed > time.Second {
		t.Errorf("took %v -- expected an instant rejection before any dial attempt", elapsed)
	}
}

func TestSend_EmptyRecipientFallsBackToConfiguredTo(t *testing.T) {
	// Regression check, unrelated to the CRLF fix: recipient=="" must
	// still fall back to b.to, not be treated as itself invalid.
	b := &Backend{host: "203.0.113.1", port: 25, from: "a@example.com", to: "configured@example.com"}
	err := b.Send("", "msg")
	// Expect a network-related error (non-routable host), NOT the CRLF
	// rejection error -- proves the empty recipient took the b.to
	// fallback path rather than being rejected outright.
	if err != nil && strings.Contains(err.Error(), "newline") {
		t.Fatalf("empty recipient was incorrectly rejected as if it contained a newline: %v", err)
	}
}

// ---------------------------------------------------------------------
// E2E: a real (fake) SMTP server, proving a legitimate send still works
// end to end -- including a genuinely multi-line message body, which
// must NOT be affected by this fix (only header values are checked).
// ---------------------------------------------------------------------

// fakeSMTPServer speaks just enough SMTP to satisfy net/smtp.SendMail
// with PlainAuth, and returns the exact bytes received after DATA.
type fakeSMTPServer struct {
	addr     string
	received chan string
}

func startFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTPServer{addr: ln.Addr().String(), received: make(chan string, 1)}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		s.serve(conn)
	}()
	t.Cleanup(func() { ln.Close() }) //nolint:errcheck
	return s
}

func (s *fakeSMTPServer) serve(conn net.Conn) {
	r := bufio.NewReader(conn)
	w := conn
	send := func(line string) { fmt.Fprintf(w, "%s\r\n", line) } //nolint:errcheck

	send("220 fake.smtp.test ESMTP ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		upper := strings.ToUpper(cmd)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			send("250-fake.smtp.test")
			send("250 AUTH PLAIN")
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			send("235 2.7.0 Authentication successful")
		case strings.HasPrefix(upper, "MAIL FROM"):
			send("250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			send("250 OK")
		case upper == "DATA":
			send("354 Start mail input; end with <CRLF>.<CRLF>")
			var sb strings.Builder
			for {
				dataLine, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
				sb.WriteString(dataLine)
			}
			s.received <- sb.String()
			send("250 OK: queued")
		case upper == "QUIT":
			send("221 Bye")
			return
		default:
			send("500 unrecognized command")
		}
	}
}

func TestSend_LegitimateMessage_DeliversEndToEnd(t *testing.T) {
	srv := startFakeSMTPServer(t)
	host, portStr, _ := net.SplitHostPort(srv.addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port) //nolint:errcheck

	b := &Backend{host: host, port: port, username: "u", password: "p", from: "alerts@datawatch.example", to: "ops@example.com"}
	multiLineBody := "Session abc123 completed.\nLine two of the summary.\nLine three."

	if err := b.Send("", multiLineBody); err != nil {
		t.Fatalf("expected a legitimate send to succeed end-to-end, got: %v", err)
	}

	select {
	case got := <-srv.received:
		if !strings.Contains(got, "From: alerts@datawatch.example") {
			t.Errorf("missing expected From header, got:\n%s", got)
		}
		if !strings.Contains(got, "To: ops@example.com") {
			t.Errorf("missing expected To header, got:\n%s", got)
		}
		// The multi-line body's own newlines must survive unmangled --
		// this fix must never touch body content, only header values.
		if !strings.Contains(got, "Line two of the summary.") || !strings.Contains(got, "Line three.") {
			t.Errorf("multi-line body was not preserved correctly, got:\n%s", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fake SMTP server never received a DATA payload")
	}
}

func TestSend_RealSendMail_AlsoRejectsCRLF_StdlibDoubleCheck(t *testing.T) {
	// Direct confirmation (not just reading the source) that
	// net/smtp.SendMail's own validateLine already rejects this --
	// documents why alert #627 was not a live exploitable gap in this
	// code as originally characterized (see the file header comment).
	start := time.Now()
	err := smtp.SendMail("203.0.113.1:25", nil, "a@example.com", []string{"evil\r\nBcc: x@y.com"}, []byte("body"))
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "CR or LF") {
		t.Fatalf("expected net/smtp's own validateLine error, got: %v", err)
	}
	if elapsed > time.Second {
		t.Errorf("took %v -- expected stdlib to reject before dialing", elapsed)
	}
}
