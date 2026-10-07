// Package apns implements BL397 Phase 4 / BL335 — Apple Push
// Notification service dispatch. The device REGISTRATION side
// (POST /api/devices/register, kind=apns) already existed; this package
// is the missing piece — actually sending a push to a registered APNs
// device token, mirroring the existing UnifiedPush/ntfy webhook
// dispatch shape (internal/server/push.go) rather than inventing a new
// pattern.
//
// Provider-token auth (RFC 8555-adjacent, Apple's own scheme): a JWT
// signed with the account's APNs Auth Key (ES256), sent as a bearer
// token on every request — no per-device certificate, one key for every
// device the Team ID owns. See
// https://developer.apple.com/documentation/usernotifications/establishing-a-token-based-connection-to-apns
package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/secrets"
)

const (
	prodBaseURL    = "https://api.push.apple.com"
	sandboxBaseURL = "https://api.sandbox.push.apple.com"
	// tokenTTL is how long a signed provider token is reused before
	// re-signing. Apple accepts tokens up to 60 minutes old; re-signing
	// well before that avoids both expiry-at-the-wire races and Apple's
	// documented rate limit on token generation (no more than once every
	// 20 minutes per key, well clear at 50).
	tokenTTL = 50 * time.Minute
)

// AlertPayload is the user-visible notification content.
type AlertPayload struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// ApsPayload is the required "aps" dictionary in every APNs payload.
type ApsPayload struct {
	Alert            *AlertPayload `json:"alert,omitempty"`
	ContentAvailable int           `json:"content-available,omitempty"`
	Badge            *int          `json:"badge,omitempty"`
}

// Payload is the full APNs notification body. SessionID/Type mirror the
// existing FCM-shaped payload documented in docs/parity-status.md's
// "APNs Server Work" section, so the app's notification handler can
// treat both platforms' payloads the same way.
type Payload struct {
	Aps       ApsPayload `json:"aps"`
	SessionID string     `json:"sessionId,omitempty"`
	Type      string     `json:"type,omitempty"`
}

// Dispatcher sends pushes to APNs using a cached, lazily-refreshed
// provider token. One Dispatcher per datawatch instance.
type Dispatcher struct {
	cfg        config.APNsConfig
	key        *ecdsa.PrivateKey
	httpClient *http.Client
	baseURL    string

	mu          sync.Mutex
	cachedToken string
	cachedAt    time.Time
}

// NewDispatcher loads the APNs Auth Key (from KeySecret via the secrets
// manager, or KeyPath on disk — KeySecret takes precedence) and returns
// a ready-to-use Dispatcher. Returns an error if APNs isn't fully
// configured — callers should treat that as "APNs dispatch unavailable,"
// not a fatal daemon-startup error (mirrors how acme.NewManager's
// failure is handled: logged, daemon continues without the feature).
func NewDispatcher(cfg config.APNsConfig, secretsStore secrets.Store) (*Dispatcher, error) {
	if cfg.KeyID == "" || cfg.TeamID == "" || cfg.BundleID == "" {
		return nil, errors.New("apns: key_id, team_id, and bundle_id are all required")
	}

	var keyPEM []byte
	switch {
	case cfg.KeySecret != "":
		if secretsStore == nil {
			return nil, errors.New("apns: key_secret is set but no secrets store is configured")
		}
		resolved, err := secrets.ResolveRef(cfg.KeySecret, secretsStore)
		if err != nil {
			return nil, fmt.Errorf("apns: resolve key_secret: %w", err)
		}
		keyPEM = []byte(resolved)
	case cfg.KeyPath != "":
		data, err := os.ReadFile(cfg.KeyPath) // #nosec G304 -- cfg.KeyPath is operator config, not external input
		if err != nil {
			return nil, fmt.Errorf("apns: read key_path: %w", err)
		}
		keyPEM = data
	default:
		return nil, errors.New("apns: one of key_secret or key_path is required")
	}

	key, err := parseAPNsKey(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("apns: parse auth key: %w", err)
	}

	baseURL := prodBaseURL
	if cfg.Sandbox {
		baseURL = sandboxBaseURL
	}

	return &Dispatcher{
		cfg:        cfg,
		key:        key,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    baseURL,
	}, nil
}

func parseAPNsKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	// Apple issues PKCS#8-wrapped EC keys for APNs Auth Keys.
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Fall back to a raw SEC1 EC key, in case the operator converted it.
		return x509.ParseECPrivateKey(block.Bytes)
	}
	ecKey, ok := keyAny.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("key is not an ECDSA private key (APNs Auth Keys are EC/P-256)")
	}
	return ecKey, nil
}

func base64URLEncode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// providerToken returns a cached JWT if still fresh, otherwise signs a
// new one. ES256 per Apple's spec: header {alg:ES256, kid}, claims
// {iss:team_id, iat:now}, signature is the raw (r||s) concatenation —
// NOT ASN.1 DER — each padded to the P-256 field size (32 bytes), which
// is what JWS requires and what crypto/ecdsa's SignASN1 does NOT
// produce directly.
func (d *Dispatcher) providerToken() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cachedToken != "" && time.Since(d.cachedAt) < tokenTTL {
		return d.cachedToken, nil
	}

	header := map[string]string{"alg": "ES256", "kid": d.cfg.KeyID}
	claims := map[string]any{"iss": d.cfg.TeamID, "iat": time.Now().Unix()}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64URLEncode(headerJSON) + "." + base64URLEncode(claimsJSON)

	hash := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, d.key, hash[:])
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}

	// P-256: each of r, s is at most 32 bytes; pad to exactly 32.
	sig := make([]byte, 64)
	r.FillBytes(sig[0:32])
	s.FillBytes(sig[32:64])

	token := signingInput + "." + base64URLEncode(sig)
	d.cachedToken = token
	d.cachedAt = time.Now()
	return token, nil
}

// ErrAPNs wraps an APNs error response (HTTP status + the "reason" field
// from Apple's JSON error body), so callers can distinguish retryable
// failures (e.g. 429 TooManyRequests) from permanent ones (e.g. 410
// Unregistered — the device token is dead, the caller should stop
// sending to it and the device record should be pruned).
type ErrAPNs struct {
	StatusCode int
	Reason     string
}

func (e *ErrAPNs) Error() string {
	return fmt.Sprintf("apns: HTTP %d: %s", e.StatusCode, e.Reason)
}

// Unregistered reports whether this error means the device token is
// permanently invalid (app uninstalled, token rotated) — Apple's signal
// to stop sending and remove the registration.
func (e *ErrAPNs) Unregistered() bool {
	return e.StatusCode == http.StatusGone && e.Reason == "Unregistered"
}

// Send delivers payload to one device token. Uses Go's standard
// net/http client, which negotiates HTTP/2 automatically over TLS (ALPN)
// — no separate HTTP/2 library needed; Apple's APNs provider API
// requires HTTP/2 and net/http already speaks it.
func (d *Dispatcher) Send(ctx context.Context, deviceToken string, payload Payload) error {
	token, err := d.providerToken()
	if err != nil {
		return fmt.Errorf("apns: provider token: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("apns: marshal payload: %w", err)
	}

	url := d.baseURL + "/3/device/" + deviceToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("apns: build request: %w", err)
	}
	req.Header.Set("authorization", "bearer "+token)
	req.Header.Set("apns-topic", d.cfg.BundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("content-type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("apns: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	var errBody struct {
		Reason string `json:"reason"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(respBody, &errBody)
	if errBody.Reason == "" {
		errBody.Reason = string(respBody)
	}
	return &ErrAPNs{StatusCode: resp.StatusCode, Reason: errBody.Reason}
}
