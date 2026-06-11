package imapmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
)

func TestBackendName(t *testing.T) {
	b := New("http://localhost:8765", "", "")
	if b.Name() != "imap_mcp" {
		t.Fatalf("Name() = %q, want %q", b.Name(), "imap_mcp")
	}
}

func TestSend(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/messages/send") {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		writeJSON(w, map[string]string{"status": "sent"})
	}))
	defer srv.Close()

	b := New(srv.URL, "personal", "test")
	if err := b.Send("user@example.com", "hello world"); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if gotBody["to"] != "user@example.com" {
		t.Errorf("to = %q, want user@example.com", gotBody["to"])
	}
	if gotBody["body"] != "hello world" {
		t.Errorf("body = %q, want hello world", gotBody["body"])
	}
	if !strings.HasPrefix(gotBody["subject"], "test") {
		t.Errorf("subject = %q, want prefix 'test'", gotBody["subject"])
	}
}

func TestSendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "smtp error", http.StatusBadGateway)
	}))
	defer srv.Close()

	b := New(srv.URL, "", "")
	err := b.Send("u@e.com", "hi")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error %q should mention 502", err.Error())
	}
}

func TestSubscribeInboundCommand(t *testing.T) {
	// Build a fake SSE event that mirrors what imap-mcp would send.
	cmd := verifiedCommand{Account: "personal", From: "ops@example.com"}
	cmd.Command.Verb = "status"
	cmd.Command.Args = ""
	cmd.Command.Nonce = "abc123"

	payload, _ := json.Marshal(cmd)
	event := sseEvent{Type: "inbound.command", Account: "personal", Payload: payload}
	eventJSON, _ := json.Marshal(event)
	sseBody := fmt.Sprintf("data: %s\n\n", eventJSON)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/events" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody) //nolint:errcheck
		// Close immediately to let stream() return.
	}))
	defer srv.Close()

	b := New(srv.URL, "personal", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var got []messaging.Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.Subscribe(ctx, func(m messaging.Message) { //nolint:errcheck
			got = append(got, m)
			cancel() // got one message, done
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Subscribe did not return within 5s")
	}

	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
	m := got[0]
	if m.Sender != "ops@example.com" {
		t.Errorf("Sender = %q, want ops@example.com", m.Sender)
	}
	if m.Text != "status" {
		t.Errorf("Text = %q, want 'status'", m.Text)
	}
	if m.ID != "abc123" {
		t.Errorf("ID = %q, want abc123", m.ID)
	}
	if m.Backend != "imap_mcp" {
		t.Errorf("Backend = %q, want imap_mcp", m.Backend)
	}
}

func TestSubscribeIgnoresNonCommand(t *testing.T) {
	// Only inbound.command should be dispatched; message.synced should be dropped.
	event := sseEvent{Type: "message.synced", Account: "personal"}
	eventJSON, _ := json.Marshal(event)
	sseBody := fmt.Sprintf("data: %s\n\n", eventJSON)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody) //nolint:errcheck
	}))
	defer srv.Close()

	b := New(srv.URL, "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var count int
	b.Subscribe(ctx, func(_ messaging.Message) { count++ }) //nolint:errcheck
	if count != 0 {
		t.Errorf("got %d messages from non-command event, want 0", count)
	}
}

func TestHandleSSELine(t *testing.T) {
	b := New("http://localhost:8765", "", "datawatch")

	t.Run("valid inbound.command", func(t *testing.T) {
		cmd := verifiedCommand{Account: "acct", From: "a@b.com"}
		cmd.Command.Verb = "deploy"
		cmd.Command.Args = `{"env":"prod"}`
		cmd.Command.Nonce = "n1"
		payload, _ := json.Marshal(cmd)
		e := sseEvent{Type: "inbound.command", Payload: payload}
		line, _ := json.Marshal(e)

		var got messaging.Message
		b.handleSSELine(string(line), func(m messaging.Message) { got = m })
		if got.Sender != "a@b.com" {
			t.Errorf("sender = %q", got.Sender)
		}
		if !strings.HasPrefix(got.Text, "deploy") {
			t.Errorf("text = %q, want deploy prefix", got.Text)
		}
	})

	t.Run("inbound.rejected is ignored", func(t *testing.T) {
		e := sseEvent{Type: "inbound.rejected"}
		line, _ := json.Marshal(e)
		var called bool
		b.handleSSELine(string(line), func(_ messaging.Message) { called = true })
		if called {
			t.Error("handler should not be called for inbound.rejected")
		}
	})

	t.Run("malformed json is ignored", func(t *testing.T) {
		var called bool
		b.handleSSELine("{not json", func(_ messaging.Message) { called = true })
		if called {
			t.Error("handler should not be called on malformed JSON")
		}
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
