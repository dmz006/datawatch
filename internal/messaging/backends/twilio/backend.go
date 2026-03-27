// Package twilio implements a messaging.Backend for Twilio SMS.
// It sends outbound messages via the Twilio REST API and receives inbound
// messages via a webhook HTTP server.
//
// Config keys (config.yaml):
//
//	twilio:
//	  enabled: true
//	  account_sid: "ACxxxxxxxx"
//	  auth_token: "your_auth_token"
//	  from_number: "+12125550001"   # Twilio number or alphanumeric sender ID
//	  to_number:   "+12125550002"   # Your phone number to send/receive
//	  webhook_addr: ":9003"          # Local port for incoming SMS webhooks
//
// # Twilio setup
//
//  1. Buy a Twilio number at https://console.twilio.com
//  2. In the number's Messaging configuration, set "A MESSAGE COMES IN" webhook
//     to https://<your-host>:9003/sms (use Tailscale Funnel or ngrok to expose externally).
//  3. Set account_sid, auth_token, from_number, to_number in config.yaml.
package twilio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
)

// Backend implements messaging.Backend for Twilio SMS.
type Backend struct {
	accountSID  string
	authToken   string
	fromNumber  string
	toNumber    string
	webhookAddr string

	srv *http.Server
}

// New creates a new Twilio Backend.
func New(accountSID, authToken, fromNumber, toNumber, webhookAddr string) *Backend {
	if webhookAddr == "" {
		webhookAddr = ":9003"
	}
	return &Backend{
		accountSID:  accountSID,
		authToken:   authToken,
		fromNumber:  fromNumber,
		toNumber:    toNumber,
		webhookAddr: webhookAddr,
	}
}

func (b *Backend) Name() string { return "twilio" }

// Send sends an SMS to recipient (phone number) via the Twilio REST API.
// If recipient is empty, uses the configured to_number.
func (b *Backend) Send(recipient, msg string) error {
	to := recipient
	if to == "" {
		to = b.toNumber
	}
	if to == "" {
		return fmt.Errorf("twilio: no recipient phone number")
	}

	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json",
		b.accountSID)

	data := url.Values{
		"From": {b.fromNumber},
		"To":   {to},
		"Body": {msg},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL,
		strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("twilio send: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(
		[]byte(b.accountSID+":"+b.authToken)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("twilio send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &apiErr)
		return fmt.Errorf("twilio send: HTTP %d: %s", resp.StatusCode, apiErr.Message)
	}
	return nil
}

// Subscribe starts the webhook HTTP server and calls handler for each inbound SMS.
// Blocks until ctx is cancelled.
func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/sms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		from := r.FormValue("From")
		body := strings.TrimSpace(r.FormValue("Body"))

		// Only process messages from the configured to_number.
		if b.toNumber != "" && from != b.toNumber {
			log.Printf("twilio: ignoring message from unexpected number %s", from)
		} else if body != "" {
			handler(messaging.Message{
				ID:      fmt.Sprintf("sms-%d", time.Now().UnixNano()),
				Sender:  from,
				Text:    body,
				Backend: "twilio",
			})
		}

		// Twilio expects a TwiML response; an empty Response is fine.
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`)
	})

	b.srv = &http.Server{
		Addr:        b.webhookAddr,
		Handler:     mux,
		ReadTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := b.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	log.Printf("twilio: webhook listening on %s/sms — configure Twilio to POST inbound SMS here", b.webhookAddr)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return b.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// Link is a no-op for Twilio (credentials are in config).
func (b *Backend) Link(_ string, _ func(string)) error { return nil }

// SelfID returns the Twilio from number.
func (b *Backend) SelfID() string { return b.fromNumber }

// Close shuts down the webhook server.
func (b *Backend) Close() error {
	if b.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return b.srv.Shutdown(ctx)
	}
	return nil
}
