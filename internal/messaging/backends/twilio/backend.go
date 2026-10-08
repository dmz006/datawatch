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
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- required by Twilio's documented signature spec, not used for anything else
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
)

// Backend implements messaging.Backend for Twilio SMS.
type Backend struct {
	accountSID       string
	authToken        string
	fromNumber       string
	toNumber         string
	webhookAddr      string
	webhookPublicURL string

	srv *http.Server
}

// New creates a new Twilio Backend. webhookPublicURL is the exact URL
// configured in the Twilio console for this number's inbound-SMS webhook
// (SEC-005) -- required for X-Twilio-Signature verification; see
// verifySignature.
func New(accountSID, authToken, fromNumber, toNumber, webhookAddr, webhookPublicURL string) *Backend {
	if webhookAddr == "" {
		webhookAddr = ":9003"
	}
	if webhookPublicURL == "" {
		log.Printf("[twilio webhook] WARNING: twilio.webhook_public_url is empty -- X-Twilio-Signature verification is DISABLED, any party that can reach the webhook address can forge inbound SMS and inject commands. Set twilio.webhook_public_url to the exact URL configured on the Twilio number's \"A MESSAGE COMES IN\" webhook.")
	}
	return &Backend{
		accountSID:       accountSID,
		authToken:        authToken,
		fromNumber:       fromNumber,
		toNumber:         toNumber,
		webhookAddr:      webhookAddr,
		webhookPublicURL: webhookPublicURL,
	}
}

// verifySignature checks Twilio's request signature (SEC-005). Twilio signs
// the exact webhook URL (as configured in the console) concatenated with
// every POST parameter's key+value, sorted by key, HMAC-SHA1-keyed with the
// account's auth token, base64-encoded. See:
// https://www.twilio.com/docs/usage/webhooks/webhooks-security
//
// Returns true (verification skipped) when fullURL is empty -- see New()'s
// own warning for that case. hmac.Equal is used for its constant-time
// comparison, matching the github backend's SEC-004 fix.
func verifySignature(authToken, fullURL string, form url.Values, sigHeader string) bool {
	if fullURL == "" {
		return true
	}
	if sigHeader == "" {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(sigHeader)
	if err != nil {
		return false
	}
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf strings.Builder
	buf.WriteString(fullURL)
	for _, k := range keys {
		buf.WriteString(k)
		buf.WriteString(form.Get(k))
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(buf.String())) //nolint:errcheck // hash.Hash.Write never returns an error
	got := mac.Sum(nil)
	return hmac.Equal(got, want)
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
	defer resp.Body.Close() //nolint:errcheck

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

// handleSMS is the inbound-webhook HTTP handler, extracted as a method
// (rather than an inline closure) so SEC-005's signature verification can
// be exercised directly in tests, matching the github backend's handleWebhook.
func (b *Backend) handleSMS(handler func(messaging.Message)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !verifySignature(b.authToken, b.webhookPublicURL, r.PostForm, r.Header.Get("X-Twilio-Signature")) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		from := r.FormValue("From")
		body := strings.TrimSpace(r.FormValue("Body"))

		// Only process messages from the configured to_number. Now a
		// secondary filter rather than the sole defense -- From is a
		// caller-controlled POST field, trivially spoofable on its own,
		// but the signature check above already rejects anything not
		// actually sent by Twilio.
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
		_, _ = fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`)
	}
}

// Subscribe starts the webhook HTTP server and calls handler for each inbound SMS.
// Blocks until ctx is cancelled.
func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/sms", b.handleSMS(handler))

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
