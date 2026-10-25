// BL427 (phase 5) — outbound webhook delivery for persisted lifecycle
// subscriptions. Design:
// docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md
// (sections 2(a)-(c) and 3).
//
// WebhookSink.Deliver POSTs one lifecycle event to the receiver a webhook
// subscription names. The pieces it builds on are in sibling files: the
// target guard and the guarded HTTP client (lifecycle_webhook_guard.go) and
// the request signature (lifecycle_webhook_sign.go). This file adds the
// delivery loop around them:
//
//   - the sink is inert unless WebhookSettings.Enabled is true; the settings
//     are read on every delivery, so a config change applies to the next event;
//   - the target is validated against the live allow-list, the signing secret
//     is resolved from the vault at send time (the subscription only carries a
//     ${secret:name} reference), and a body is built once per delivery so every
//     retry sends the same bytes and the same delivery id;
//   - the response is classified (2xx delivered; network error, timeout, 408,
//     429 and 5xx retried with a jittered, growing delay; everything else is
//     permanent) and a delivery that is dropped raises one alert;
//   - every attempt, and the final failure, is audited through the injected
//     Audit function (the daemon wires the existing SetAuditFn path).
//
// Nothing a log, an audit record or an alert carries may contain the secret,
// the signature, URL userinfo or the URL query string: errors from the HTTP
// stack embed the full URL, so every error is scrubbed before it leaves here.
//
// Import cycle: the vault, the audit writer and the alert store are injected
// function values (WebhookSinkConfig); this package does not import
// internal/server or internal/session.

package autonomous

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

// Defaults for the retry policy. Pending operator confirmation: these are the
// proposal's recommended values (5 attempts, 1s / 4s / 16s / 64s between them
// with +-20% jitter, 10s per attempt). A changed decision is an edit here.
const (
	DefaultWebhookMaxAttempts = 5
	DefaultWebhookBackoffBase = 1 * time.Second
	DefaultWebhookBackoffMax  = 64 * time.Second
	DefaultWebhookTimeout     = 10 * time.Second

	// webhookBackoffFactor multiplies the delay after each failed attempt
	// (1s, 4s, 16s, 64s). Pending operator confirmation.
	webhookBackoffFactor = 4
	// webhookJitterFraction is the +- spread applied to each delay.
	// Pending operator confirmation.
	webhookJitterFraction = 0.2
	// webhookRetryAfterCap caps a 429 Retry-After the sink will honour.
	// Pending operator confirmation.
	webhookRetryAfterCap = 120 * time.Second
)

// Wire details of the request and the envelope. Pending operator
// confirmation (the header names live in lifecycle_webhook_sign.go).
const (
	// WebhookEnvelopeVersion is the version of the JSON envelope.
	WebhookEnvelopeVersion = 1
	// WebhookMaxBodyBytes caps the serialised envelope. Over the cap the
	// event's decision and verdict are dropped and the envelope is marked
	// truncated; still over, the delivery fails without being sent.
	WebhookMaxBodyBytes = 64 * 1024

	webhookContentType = "application/json"
	webhookUserAgent   = "datawatch-lifecycle/1"
	// webhookAlertLevel is the alert level a dropped delivery raises.
	webhookAlertLevel = "error"
)

// Delivery outcomes carried in the audit details.
const (
	WebhookOutcomeDelivered = "delivered"
	WebhookOutcomeRetry     = "retry"
	WebhookOutcomeFailed    = "failed"
	WebhookOutcomeRefused   = "refused"
	WebhookOutcomeSkipped   = "skipped"
)

// ErrWebhookDelivery is wrapped by the error Deliver returns for a delivery
// that was dropped (retries exhausted, a permanent response, a refused target,
// an unresolvable secret, an oversize body).
var ErrWebhookDelivery = errors.New("webhook delivery failed")

const redactedText = "[redacted]"

// WebhookSettings is the live configuration of the webhook sink. A zero
// MaxAttempts, BackoffBase, BackoffMax or Timeout falls back to its default.
// Enabled defaults to false: the sink ships disabled until the operator
// confirms the proposal.
type WebhookSettings struct {
	Enabled     bool
	AllowHosts  []string
	MaxAttempts int
	BackoffBase time.Duration
	BackoffMax  time.Duration
	Timeout     time.Duration
}

// DefaultWebhookSettings returns the disabled-by-default settings with the
// proposal's recommended retry policy.
func DefaultWebhookSettings() WebhookSettings {
	return WebhookSettings{
		MaxAttempts: DefaultWebhookMaxAttempts,
		BackoffBase: DefaultWebhookBackoffBase,
		BackoffMax:  DefaultWebhookBackoffMax,
		Timeout:     DefaultWebhookTimeout,
	}
}

func (s WebhookSettings) withDefaults() WebhookSettings {
	if s.MaxAttempts <= 0 {
		s.MaxAttempts = DefaultWebhookMaxAttempts
	}
	if s.BackoffBase <= 0 {
		s.BackoffBase = DefaultWebhookBackoffBase
	}
	if s.BackoffMax <= 0 {
		s.BackoffMax = DefaultWebhookBackoffMax
	}
	if s.Timeout <= 0 {
		s.Timeout = DefaultWebhookTimeout
	}
	return s
}

// WebhookSinkConfig is the dependency set of the webhook sink. Every entry is
// an injected function value. Settings == nil means disabled. A nil Audit or
// RaiseAlert drops that output; a nil ResolveSecret makes every delivery fail
// (an unsigned webhook is not allowed). The remaining entries are test seams
// with production defaults.
type WebhookSinkConfig struct {
	// Settings returns the live settings; it is called on every delivery.
	Settings func() WebhookSettings
	// ResolveSecret turns the subscription's ${secret:name} reference into
	// the HMAC key. The daemon wires the secrets vault.
	ResolveSecret func(ref string) (string, error)
	// Audit records one JSON-lines + CEF audit entry (the SetAuditFn path).
	Audit func(action string, details map[string]any)
	// RaiseAlert records an alert in the alert store.
	RaiseAlert func(level, title, body string)

	// Sleep waits between attempts. Nil waits on a timer that a cancelled
	// context interrupts.
	Sleep func(time.Duration)
	// Now is the clock. Nil is time.Now.
	Now func() time.Time
	// Jitter returns a value in [0,1) that spreads each delay by
	// +-webhookJitterFraction. Nil is math/rand.
	Jitter func() float64
	// NewClient builds the HTTP client for one delivery from the live
	// allow-list and the per-attempt timeout. Nil is NewWebhookHTTPClient.
	NewClient func(allow []string, timeout time.Duration) *http.Client
	// Hostname names the sending daemon in the envelope. Nil is
	// os.Hostname, falling back to the event's own host.
	Hostname func() string
}

// WebhookSink delivers lifecycle events to outbound webhook receivers. Deliver
// is safe for concurrent use: the sink keeps no per-delivery state.
type WebhookSink struct {
	cfg WebhookSinkConfig
}

// NewWebhookSink builds the sink, filling the seams cfg leaves nil.
func NewWebhookSink(cfg WebhookSinkConfig) *WebhookSink {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Jitter == nil {
		cfg.Jitter = mrand.Float64
	}
	if cfg.NewClient == nil {
		cfg.NewClient = NewWebhookHTTPClient
	}
	return &WebhookSink{cfg: cfg}
}

// Enabled reports whether the live settings allow delivery; a nil sink is
// disabled. Deliver returns nil while disabled (nothing is sent), so the
// session-push engine asks first and leaves a one-shot subscription armed.
func (s *WebhookSink) Enabled() bool {
	return s != nil && s.cfg.Settings != nil && s.cfg.Settings().Enabled
}

// WebhookEnvelope is the JSON body of one POST. Everything a receiver needs to
// de-duplicate and attribute the event is inside the signed bytes.
type WebhookEnvelope struct {
	Version        int            `json:"version"`
	DeliveryID     string         `json:"delivery_id"`
	SentAt         time.Time      `json:"sent_at"`
	Host           string         `json:"host"`
	SubscriptionID string         `json:"subscription_id"`
	Event          LifecycleEvent `json:"event"`
	// Truncated is true only when the event's decision and verdict were
	// removed to fit WebhookMaxBodyBytes.
	Truncated bool `json:"truncated,omitempty"`
}

// webhookRun is the state of one Deliver call.
type webhookRun struct {
	subID      string
	deliveryID string
	host       string // host:port of the target, "unknown" when unparseable
	maxAttempt int
	start      time.Time
	rawTarget  string
	secret     string
}

// webhookAttempt is the result of one POST.
type webhookAttempt struct {
	ok         bool
	status     int
	err        error
	retry      bool
	guard      bool
	retryAfter time.Duration
}

// Deliver sends ev to the receiver sub names. It returns nil when the event
// was delivered or when the sink is disabled (nothing is sent, audited or
// alerted). It returns an error wrapping ErrWebhookDelivery for a dropped
// delivery (after raising one alert and writing the final audit record) and an
// error wrapping ctx.Err() when ctx ended first (no alert: the delivery was
// abandoned, not refused by the receiver).
func (s *WebhookSink) Deliver(ctx context.Context, sub Subscription, ev LifecycleEvent) error {
	if s.cfg.Settings == nil {
		return nil
	}
	set := s.cfg.Settings().withDefaults()
	if !set.Enabled {
		return nil
	}

	run := &webhookRun{
		subID:      sub.ID,
		deliveryID: newWebhookDeliveryID(),
		host:       webhookTargetHost(sub.Sink.Target),
		maxAttempt: set.MaxAttempts,
		start:      s.cfg.Now(),
		rawTarget:  sub.Sink.Target,
	}

	u, err := ValidateWebhookURL(sub.Sink.Target, set.AllowHosts)
	if err != nil {
		return s.dropBeforeSend(run, WebhookOutcomeRefused, err)
	}
	run.host = u.Host

	// Whatever the resolver returned is remembered before the error check, so
	// an error that echoes the value is scrubbed like any other text.
	secret, err := s.resolveSecret(sub.Sink.Secret)
	run.secret = secret
	if err != nil {
		return s.dropBeforeSend(run, WebhookOutcomeFailed, err)
	}

	body, err := s.buildBody(run, sub, ev)
	if err != nil {
		return s.dropBeforeSend(run, WebhookOutcomeFailed, err)
	}

	client := s.client(set)
	for attempt := 1; attempt <= set.MaxAttempts; attempt++ {
		if cerr := ctx.Err(); cerr != nil {
			return s.abandon(run, attempt-1, cerr)
		}
		res := s.post(ctx, client, u, set, run, body, ev.Kind)
		switch {
		case res.ok:
			s.attemptRecord(run, attempt, res.status, WebhookOutcomeDelivered, "")
			return nil
		case ctx.Err() != nil:
			return s.abandon(run, attempt, ctx.Err())
		case res.guard:
			return s.dropAfterAttempt(run, attempt, res, WebhookOutcomeRefused)
		case !res.retry || attempt == set.MaxAttempts:
			return s.dropAfterAttempt(run, attempt, res, WebhookOutcomeFailed)
		}
		s.attemptRecord(run, attempt, res.status, WebhookOutcomeRetry, s.errText(run, res.err))
		if werr := s.wait(ctx, s.backoff(set, attempt, res.retryAfter)); werr != nil {
			return s.abandon(run, attempt, werr)
		}
	}
	return nil // unreachable: the last attempt always returns above
}

// resolveSecret turns the subscription's reference into the HMAC key.
func (s *WebhookSink) resolveSecret(ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", errors.New("the subscription has no signing secret reference; an unsigned webhook is not allowed")
	}
	if s.cfg.ResolveSecret == nil {
		return "", errors.New("no secret resolver is configured")
	}
	v, err := s.cfg.ResolveSecret(ref)
	if err != nil {
		return v, fmt.Errorf("the signing secret could not be resolved: %w", err)
	}
	if v == "" {
		return "", errors.New("the signing secret resolved to an empty value")
	}
	return v, nil
}

// buildBody marshals the envelope once per delivery. Over the size cap the
// event's decision and verdict are dropped first; still over, it is an error.
func (s *WebhookSink) buildBody(run *webhookRun, sub Subscription, ev LifecycleEvent) ([]byte, error) {
	env := WebhookEnvelope{
		Version:        WebhookEnvelopeVersion,
		DeliveryID:     run.deliveryID,
		SentAt:         run.start.UTC(),
		Host:           s.hostname(ev),
		SubscriptionID: sub.ID,
		Event:          ev,
	}
	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("the event could not be encoded: %w", err)
	}
	if len(body) <= WebhookMaxBodyBytes {
		return body, nil
	}
	env.Event.Decision, env.Event.Verdict, env.Truncated = nil, nil, true
	body, err = json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("the event could not be encoded: %w", err)
	}
	if len(body) > WebhookMaxBodyBytes {
		return nil, fmt.Errorf("the payload is %d bytes, over the %d byte cap", len(body), WebhookMaxBodyBytes)
	}
	return body, nil
}

func (s *WebhookSink) hostname(ev LifecycleEvent) string {
	if s.cfg.Hostname != nil {
		if h := s.cfg.Hostname(); h != "" {
			return h
		}
	} else if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return ev.Host
}

// client builds the HTTP client for one delivery and makes sure that a
// factory-supplied client can never follow a redirect either.
func (s *WebhookSink) client(set WebhookSettings) *http.Client {
	c := s.cfg.NewClient(set.AllowHosts, set.Timeout)
	if c == nil {
		c = NewWebhookHTTPClient(set.AllowHosts, set.Timeout)
	}
	cc := *c
	cc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cc
}

// post makes one signed attempt and classifies the response.
func (s *WebhookSink) post(ctx context.Context, client *http.Client, u *url.URL, set WebhookSettings, run *webhookRun, body []byte, kind string) webhookAttempt {
	actx, cancel := context.WithTimeout(ctx, set.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(actx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return webhookAttempt{err: errors.New("the request could not be built")}
	}
	sentAt := s.cfg.Now()
	req.Header.Set("Content-Type", webhookContentType)
	req.Header.Set("User-Agent", webhookUserAgent)
	req.Header.Set(WebhookSignatureHeader, SignWebhookBody([]byte(run.secret), sentAt, body))
	req.Header.Set(WebhookDeliveryHeader, run.deliveryID)
	req.Header.Set(WebhookEventHeader, headerSafe(kind))

	resp, err := client.Do(req)
	if err != nil {
		res := webhookAttempt{err: err, retry: true}
		switch {
		case errors.Is(err, ErrWebhookGuard):
			res.retry, res.guard = false, true
		case ctx.Err() != nil:
			res.retry = false
		}
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, webhookMaxResponseBytes))

	code := resp.StatusCode
	res := webhookAttempt{status: code}
	switch {
	case code >= 200 && code <= 299:
		res.ok = true
	case code == http.StatusRequestTimeout, code == http.StatusTooManyRequests, code >= 500:
		res.retry = true
		res.err = fmt.Errorf("receiver answered %d", code)
		if code == http.StatusTooManyRequests {
			res.retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), s.cfg.Now())
		}
	case code >= 300 && code <= 399:
		res.err = fmt.Errorf("receiver answered %d; redirects are not followed", code)
	default:
		res.err = fmt.Errorf("receiver answered %d", code)
	}
	return res
}

// parseRetryAfter reads delta-seconds or an HTTP date; anything else is zero.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		if n <= 0 {
			return 0
		}
		if n > int64(webhookRetryAfterCap/time.Second) {
			return webhookRetryAfterCap
		}
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			if d > webhookRetryAfterCap {
				return webhookRetryAfterCap
			}
			return d
		}
	}
	return 0
}

// backoff returns the wait after the failed attempt number n (1-based):
// base * factor^(n-1), capped at BackoffMax, spread by the jitter, and raised
// to a Retry-After (itself capped) when that is longer.
func (s *WebhookSink) backoff(set WebhookSettings, n int, retryAfter time.Duration) time.Duration {
	d := set.BackoffBase
	for i := 1; i < n && d < set.BackoffMax; i++ {
		d *= webhookBackoffFactor
	}
	if d > set.BackoffMax {
		d = set.BackoffMax
	}
	j := s.cfg.Jitter()
	if j < 0 {
		j = 0
	} else if j > 1 {
		j = 1
	}
	d = time.Duration(float64(d) * (1 + webhookJitterFraction*(2*j-1)))
	if retryAfter > d {
		d = retryAfter
	}
	return d
}

// wait sleeps d, returning ctx's error when ctx ended first.
func (s *WebhookSink) wait(ctx context.Context, d time.Duration) error {
	if s.cfg.Sleep != nil {
		s.cfg.Sleep(d)
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ---------------------------------------------------------------------------
// Audit, alert and error text
// ---------------------------------------------------------------------------

func (s *WebhookSink) auditEmit(action string, details map[string]any) {
	if s.cfg.Audit != nil {
		s.cfg.Audit(action, details)
	}
}

func (s *WebhookSink) details(run *webhookRun, attempt, status int, outcome, errText string) map[string]any {
	return map[string]any{
		"subscription_id": run.subID,
		"delivery_id":     run.deliveryID,
		"attempt":         attempt,
		"max_attempts":    run.maxAttempt,
		"host":            run.host,
		"status_code":     status,
		"outcome":         outcome,
		"error":           errText,
		"duration_ms":     s.cfg.Now().Sub(run.start).Milliseconds(),
	}
}

// attemptRecord writes the record of one attempt. attempt is 0 when no request
// was made (the target, the secret or the body was refused first).
func (s *WebhookSink) attemptRecord(run *webhookRun, attempt, status int, outcome, errText string) {
	s.auditEmit(audit.ActionLifecycleWebhookDelivery, s.details(run, attempt, status, outcome, errText))
}

// dropBeforeSend ends a delivery that never reached the network.
func (s *WebhookSink) dropBeforeSend(run *webhookRun, outcome string, cause error) error {
	text := s.errText(run, cause)
	s.attemptRecord(run, 0, 0, outcome, text)
	return s.drop(run, 0, 0, outcome, text)
}

// dropAfterAttempt ends a delivery on its attempt-th (failed) POST.
func (s *WebhookSink) dropAfterAttempt(run *webhookRun, attempt int, res webhookAttempt, outcome string) error {
	text := s.errText(run, res.err)
	s.attemptRecord(run, attempt, res.status, outcome, text)
	return s.drop(run, attempt, res.status, outcome, text)
}

// drop writes the final-failure record, raises the one alert and builds the
// returned error.
func (s *WebhookSink) drop(run *webhookRun, attempts, status int, outcome, text string) error {
	s.auditEmit(audit.ActionLifecycleWebhookFailed, s.details(run, attempts, status, outcome, text))
	if s.cfg.RaiseAlert != nil {
		s.cfg.RaiseAlert(webhookAlertLevel, "Lifecycle webhook delivery failed",
			fmt.Sprintf("Subscription %s: delivery to %s was dropped after %d attempt(s): %s",
				run.subID, run.host, attempts, text))
	}
	return fmt.Errorf("%w: host %s: %s", ErrWebhookDelivery, run.host, text)
}

// abandon ends a delivery whose context ended: recorded as skipped, no alert.
func (s *WebhookSink) abandon(run *webhookRun, attempts int, cause error) error {
	s.attemptRecord(run, attempts, 0, WebhookOutcomeSkipped, "delivery abandoned: "+cause.Error())
	return fmt.Errorf("webhook delivery abandoned: %w", cause)
}

// errText renders err for an audit record, an alert or a returned error: the
// HTTP stack's own wrapper (which embeds the full URL) is removed and then the
// secret, the raw target and its query string are scrubbed from what is left.
func (s *WebhookSink) errText(run *webhookRun, err error) string {
	if err == nil {
		return ""
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() && !errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return scrubWebhookText(err.Error(), run.rawTarget, run.secret)
}

// scrubWebhookText removes the secret, the raw target URL and the target's
// query string from text.
func scrubWebhookText(text, rawTarget, secret string) string {
	var drops []string
	if rawTarget != "" {
		drops = append(drops, rawTarget)
		if u, err := url.Parse(rawTarget); err == nil {
			if u.RawQuery != "" {
				drops = append(drops, u.RawQuery)
			}
			if u.User != nil {
				drops = append(drops, u.User.String())
				if pw, ok := u.User.Password(); ok && pw != "" {
					drops = append(drops, pw)
				}
			}
		}
	}
	if secret != "" {
		drops = append(drops, secret)
	}
	for _, d := range drops {
		text = strings.ReplaceAll(text, d, redactedText)
	}
	return text
}

// webhookTargetHost returns the host:port of raw for audit and alert text, or
// "unknown" when raw has no parseable host. Userinfo, path and query never
// appear in it.
func webhookTargetHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "unknown"
	}
	return u.Host
}

// headerSafe strips control characters so an event kind cannot break a header.
func headerSafe(v string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, v)
}

// newWebhookDeliveryID returns 32 hex characters, stable across the retries of
// one delivery because Deliver calls it once.
func newWebhookDeliveryID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
