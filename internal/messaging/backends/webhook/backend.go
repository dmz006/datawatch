// Package webhook implements a generic HTTP webhook messaging.Backend.
// POST JSON to the endpoint: {"task": "write tests", "project_dir": "/opt/myapp"}
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dmz006/datawatch/internal/messaging"
)

// Backend listens for generic webhook POST requests.
type Backend struct {
	addr  string
	token string
	srv   *http.Server
	msgs  chan messaging.Message
}

// New creates a new generic webhook backend.
func New(addr, token string) *Backend {
	b := &Backend{addr: addr, token: token, msgs: make(chan messaging.Message, 64)}
	mux := http.NewServeMux()
	mux.HandleFunc("/task", b.handleTask)
	b.srv = &http.Server{Addr: addr, Handler: mux}
	return b
}

func (b *Backend) Name() string { return "webhook" }

func (b *Backend) Send(recipient, message string) error { return nil }

func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	go func() {
		if err := b.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[webhook] server error: %v\n", err)
		}
	}()
	defer b.srv.Shutdown(context.Background()) //nolint:errcheck
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg := <-b.msgs:
			handler(msg)
		}
	}
}

type taskRequest struct {
	Task       string `json:"task"`
	ProjectDir string `json:"project_dir"`
}

func (b *Backend) handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	if b.token != "" {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+b.token {
			http.Error(w, "unauthorized", 401)
			return
		}
	}
	var req taskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad JSON", 400)
		return
	}
	if req.Task == "" {
		http.Error(w, "task required", 400)
		return
	}
	text := req.Task
	if req.ProjectDir != "" {
		text = req.ProjectDir + ": " + text
	}
	b.msgs <- messaging.Message{
		GroupID: "webhook", Sender: r.RemoteAddr, Text: text, Backend: "webhook",
	}
	w.WriteHeader(200)
	w.Write([]byte(`{"ok":true}` + "\n")) //nolint:errcheck
}

func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }
func (b *Backend) SelfID() string                                   { return "webhook:" + b.addr }
func (b *Backend) Close() error                                     { return b.srv.Shutdown(context.Background()) }
