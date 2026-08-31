// Package webhook implements a generic HTTP webhook messaging.Backend.
// POST JSON to the endpoint: {"task": "write tests", "project_dir": "/opt/myapp"}
// Optionally include "image_url" as a base64 data URI to attach an image.
package webhook

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

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
	// G112 fix (v6.22.2): ReadHeaderTimeout prevents Slowloris attacks
	// where a client opens connections + drips bytes to keep them alive.
	b.srv = &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
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
	// ImageURL is an optional base64 data URI (e.g. "data:image/png;base64,...")
	// or a local file path. When set the image is delivered as an Attachment so
	// the router can invoke vision description before passing the task to Claude.
	ImageURL string `json:"image_url,omitempty"`
}

func (b *Backend) handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if b.token != "" {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+b.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
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

	var attachments []messaging.Attachment
	if req.ImageURL != "" {
		if att, err := decodeImageURL(req.ImageURL); err == nil {
			attachments = append(attachments, att)
		}
	}

	b.msgs <- messaging.Message{
		GroupID:     "webhook",
		Sender:      r.RemoteAddr,
		Text:        text,
		Backend:     "webhook",
		Attachments: attachments,
	}
	w.WriteHeader(200)
	w.Write([]byte(`{"ok":true}` + "\n")) //nolint:errcheck
}

// decodeImageURL handles "data:<mime>;base64,<b64>" URIs and local file paths.
// It writes the image to a temp file and returns an Attachment.
func decodeImageURL(imageURL string) (messaging.Attachment, error) {
	var data []byte
	var contentType, ext string

	if strings.HasPrefix(imageURL, "data:") {
		// data:<mime>;base64,<data>
		rest := strings.TrimPrefix(imageURL, "data:")
		semi := strings.Index(rest, ";")
		if semi < 0 {
			return messaging.Attachment{}, fmt.Errorf("webhook: malformed data URI")
		}
		contentType = rest[:semi]
		encoded := strings.TrimPrefix(rest[semi+1:], "base64,")
		var err error
		data, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return messaging.Attachment{}, fmt.Errorf("webhook: base64 decode: %w", err)
		}
		switch contentType {
		case "image/png":
			ext = ".png"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		default:
			ext = ".jpg"
			contentType = "image/jpeg"
		}
	} else {
		// Local file path
		var err error
		data, err = os.ReadFile(imageURL)
		if err != nil {
			return messaging.Attachment{}, fmt.Errorf("webhook: read image: %w", err)
		}
		switch {
		case strings.HasSuffix(imageURL, ".png"):
			contentType, ext = "image/png", ".png"
		case strings.HasSuffix(imageURL, ".gif"):
			contentType, ext = "image/gif", ".gif"
		case strings.HasSuffix(imageURL, ".webp"):
			contentType, ext = "image/webp", ".webp"
		default:
			contentType, ext = "image/jpeg", ".jpg"
		}
	}

	tmp, err := os.CreateTemp("", "dw-webhook-img-*"+ext)
	if err != nil {
		return messaging.Attachment{}, fmt.Errorf("webhook: temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return messaging.Attachment{}, fmt.Errorf("webhook: write temp: %w", err)
	}
	_ = tmp.Close()
	return messaging.Attachment{
		ContentType: contentType,
		Filename:    "image" + ext,
		FilePath:    tmp.Name(),
		Size:        int64(len(data)),
	}, nil
}

func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }
func (b *Backend) SelfID() string                                   { return "webhook:" + b.addr }
func (b *Backend) Close() error                                     { return b.srv.Shutdown(context.Background()) }
