// Package github implements a messaging.Backend that receives GitHub webhook events.
// Start an HTTP listener; configure your GitHub repo webhook to point to it.
// Supported events: issue_comment, pull_request_review_comment, workflow_dispatch.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/dmz006/datawatch/internal/messaging"
)

// Backend listens for GitHub webhook POST requests.
type Backend struct {
	addr   string
	secret string
	srv    *http.Server
	msgs   chan messaging.Message
}

// New creates a new GitHub webhook backend.
func New(addr, secret string) *Backend {
	b := &Backend{addr: addr, secret: secret, msgs: make(chan messaging.Message, 64)}
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", b.handleWebhook)
	b.srv = &http.Server{Addr: addr, Handler: mux}
	return b
}

func (b *Backend) Name() string { return "github" }

func (b *Backend) Send(recipient, message string) error { return nil } // read-only source

func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	go func() {
		if err := b.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[github webhook] server error: %v\n", err)
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

func (b *Backend) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}
	r.Body.Close()
	eventType := r.Header.Get("X-GitHub-Event")
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		w.WriteHeader(200)
		return
	}
	var text, sender, groupID string
	switch eventType {
	case "issue_comment":
		text, _ = payload["comment"].(map[string]interface{})["body"].(string)
		senderMap, _ := payload["sender"].(map[string]interface{})
		sender, _ = senderMap["login"].(string)
		issueMap, _ := payload["issue"].(map[string]interface{})
		groupID = fmt.Sprintf("issue:%v", issueMap["number"])
	case "pull_request_review_comment":
		text, _ = payload["comment"].(map[string]interface{})["body"].(string)
		senderMap, _ := payload["sender"].(map[string]interface{})
		sender, _ = senderMap["login"].(string)
		prMap, _ := payload["pull_request"].(map[string]interface{})
		groupID = fmt.Sprintf("pr:%v", prMap["number"])
	case "workflow_dispatch":
		inputsMap, _ := payload["inputs"].(map[string]interface{})
		text, _ = inputsMap["task"].(string)
		senderMap, _ := payload["sender"].(map[string]interface{})
		sender, _ = senderMap["login"].(string)
		groupID = "workflow_dispatch"
	default:
		w.WriteHeader(200)
		return
	}
	if text == "" {
		w.WriteHeader(200)
		return
	}
	b.msgs <- messaging.Message{
		GroupID: groupID, Sender: sender, Text: text, Backend: "github",
	}
	w.WriteHeader(200)
}

func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }
func (b *Backend) SelfID() string                                   { return "github-webhook" }
func (b *Backend) Close() error                                     { return b.srv.Shutdown(context.Background()) }
