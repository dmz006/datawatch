// Package webhook implements a generic HTTP webhook messaging.Backend.
// POST JSON to the endpoint: {"task": "write tests", "project_dir": "/opt/myapp"}
// Optionally include "image_url" as a base64 data URI to attach an image.
//
// BL394 security fix (docs/plans/2026-10-03-bl394-security-findings-review.md
// §3b, alert #554): "image_url" also accepted an arbitrary local file path
// with zero validation -- os.ReadFile(imageURL) directly, no scoping -- and
// this endpoint's bearer token is OPTIONAL (Token == "" means no auth at
// all). Any caller who could reach this listener (loopback by default, but
// operator-configurable) could read any file the daemon process can read
// and have its content attached into the task pipeline. Fixed by scoping
// the local-file-path feature to one operator-designated directory
// (webhook.image_dir, config.WebhookConfig) -- empty (the default)
// disables the feature entirely rather than silently allowing it
// unrestricted. The "data:<mime>;base64,..." form is unaffected either
// way; it never touches the filesystem.
package webhook

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
)

// Backend listens for generic webhook POST requests.
type Backend struct {
	addr     string
	token    string
	imageDir string
	srv      *http.Server
	msgs     chan messaging.Message
}

// New creates a new generic webhook backend. imageDir scopes the
// image_url-as-local-file-path feature (BL394); pass "" to disable it.
func New(addr, token, imageDir string) *Backend {
	b := &Backend{addr: addr, token: token, imageDir: imageDir, msgs: make(chan messaging.Message, 64)}
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
		if att, err := b.decodeImageURL(req.ImageURL); err == nil {
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

// decodeImageURL handles "data:<mime>;base64,<b64>" URIs and local file
// paths (the latter scoped to b.imageDir -- BL394, see package comment).
// It writes the image to a temp file and returns an Attachment.
func (b *Backend) decodeImageURL(imageURL string) (messaging.Attachment, error) {
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
		// Local file path -- scoped to b.imageDir (BL394).
		if b.imageDir == "" {
			return messaging.Attachment{}, fmt.Errorf("webhook: local file path attachments are disabled (set webhook.image_dir to enable)")
		}
		resolved := imageURL
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(b.imageDir, resolved)
		}
		cleanDir := filepath.Clean(b.imageDir)
		cleanPath := filepath.Clean(resolved)
		if cleanPath != cleanDir && !strings.HasPrefix(cleanPath+string(filepath.Separator), cleanDir+string(filepath.Separator)) {
			return messaging.Attachment{}, fmt.Errorf("webhook: image path outside webhook.image_dir")
		}
		var err error
		data, err = os.ReadFile(cleanPath)
		if err != nil {
			return messaging.Attachment{}, fmt.Errorf("webhook: read image: %w", err)
		}
		imageURL = cleanPath
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
func (b *Backend) SelfID() string                                  { return "webhook:" + b.addr }
func (b *Backend) Close() error                                    { return b.srv.Shutdown(context.Background()) }
