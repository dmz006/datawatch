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
	"github.com/dmz006/datawatch/internal/config"
)

// Defaults for the retry policy. The recommended values live in the config
// package (config.DefaultNotify*, pending operator confirmation); these names
// re-export them so the sender and its tests read one source. The configured
// backoff schedule is the default; the geometric curve below is only the
// fallback for a settings value with no explicit schedule.
const (
	DefaultWebhookMaxAttempts = config.DefaultNotifyMaxAttempts
	DefaultWebhookBackoffBase = 1 * time.Second
	DefaultWebhookBackoffMax  = 64 * time.Second
	DefaultWebhookTimeout     = config.DefaultNotifyAttemptTimeoutSeconds * time.Second

	// webhookBackoffFactor multiplies the delay after each failed attempt in
	// the geometric fallback (1s, 4s, 16s, 64s). Pending operator confirmation.
	webhookBackoffFactor = 4
	// webhookRetryAfterCap caps a 429 Retry-After the sink will honour.
	// Pending operator confirmation.
	webhookRetryAfterCap = 120 * time.Second
)

// Wire details of the request and the envelope. Pending operator
// confirmation (the header names live in lifecycle_webhook_sign.go).
const (
	// WebhookEnvelopeVersion is the version of the JSON envelope.
	WebhookEnvelopeVersion = 1
	// WebhookMaxBodyBytes is the default envelope cap (the live value comes
	// from WebhookSettings.MaxBodyBytes). Over the cap the event's decision and
	// verdict are dropped and the envelope is marked truncated; still over, the
	// delivery fails without being sent.
	WebhookMaxBodyBytes = config.DefaultNotifyMaxBodyBytes

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

// WebhookSettings is the live configuration of the webhook sink. Enabled
// defaults to false: the sink ships disabled until the operator confirms the
// proposal. A zero value for any other field falls back to its default (see
// withDefaults). WebhookSettingsFromConfig builds it from the config keys.
type WebhookSettings struct {
	Enabled     bool
	AllowHosts  []string
	MaxAttempts int
	// Backoff is the explicit delay after each failed attempt; the last entry
	// repeats. When empty, the geometric curve from BackoffBase to BackoffMax
	// applies.
	Backoff     []time.Duration
	BackoffBase time.Duration
	BackoffMax  time.Duration
	// JitterPercent is the +- spread on each delay, in percent.
	JitterPercent int
	Timeout       time.Duration
	// MaxBodyBytes is the envelope cap, shared with peer forwarding.
	MaxBodyBytes int
	// SignatureTolerance is the replay window a receiver enforces on the
	// signed timestamp (see VerifyWebhookSignature).
	SignatureTolerance time.Duration
}

// DefaultWebhookSettings returns the disabled-by-default settings with the
// proposal's recommended values.
func DefaultWebhookSettings() WebhookSettings {
	return WebhookSettings{
		MaxAttempts:        DefaultWebhookMaxAttempts,
		Backoff:            secondsToDurations(config.DefaultNotifyBackoffSeconds()),
		BackoffBase:        DefaultWebhookBackoffBase,
		BackoffMax:         DefaultWebhookBackoffMax,
		JitterPercent:      config.DefaultNotifyJitterPercent,
		Timeout:            DefaultWebhookTimeout,
		MaxBodyBytes:       config.DefaultNotifyMaxBodyBytes,
		SignatureTolerance: time.Duration(config.DefaultNotifySignatureToleranceSeconds) * time.Second,
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
	if s.JitterPercent <= 0 {
		s.JitterPercent = config.DefaultNotifyJitterPercent
	}
	if s.Timeout <= 0 {
		s.Timeout = DefaultWebhookTimeout
	}
	if s.MaxBodyBytes <= 0 {
		s.MaxBodyBytes = config.DefaultNotifyMaxBodyBytes
	}
	if s.SignatureTolerance <= 0 {
		s.SignatureTolerance = time.Duration(config.DefaultNotifySignatureToleranceSeconds) * time.Second
	}
	return s
}

// curve is the delay policy the webhook sender uses.
func (s WebhookSettings) curve() retryCurve {
	return retryCurve{
		schedule:      s.Backoff,
		base:          s.BackoffBase,
		ceiling:       s.BackoffMax,
		jitterPercent: s.JitterPercent,
	}
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
	// +- the settings' JitterPercent. Nil is math/rand.
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
	// Forward is present only on a peer-forwarded envelope (lifecycle_forward.go):
	// the origin host, the hop count and the hosts the event has visited.
	Forward *ForwardChain `json:"forward,omitempty"`
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
	attempts   int // POSTs made so far
	status     int // status of the last response, 0 when none arrived
}

// DeliveryReport is the outcome of a delivery that reached the attempt loop:
// how many POSTs were made and the status of the last response (0 when the
// last attempt got no HTTP response). The subscription test reports both.
type DeliveryReport struct {
	Attempts int
	Status   int
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
	_, err := s.DeliverReport(ctx, sub, ev)
	return err
}

// DeliverReport is Deliver that also reports the attempt count and the last
// response status. A disabled sink returns the zero report and nil.
func (s *WebhookSink) DeliverReport(ctx context.Context, sub Subscription, ev LifecycleEvent) (DeliveryReport, error) {
	if s.cfg.Settings == nil {
		return DeliveryReport{}, nil
	}
	set := s.cfg.Settings().withDefaults()
	if !set.Enabled {
		return DeliveryReport{}, nil
	}

	run := &webhookRun{
		subID:      sub.ID,
		deliveryID: newWebhookDeliveryID(),
		host:       webhookTargetHost(sub.Sink.Target),
		maxAttempt: set.MaxAttempts,
		start:      s.cfg.Now(),
		rawTarget:  sub.Sink.Target,
	}
	err := s.deliverRun(ctx, sub, ev, set, run)
	return DeliveryReport{Attempts: run.attempts, Status: run.status}, err
}

// deliverRun runs the attempt loop for one delivery; run carries its state.
func (s *WebhookSink) deliverRun(ctx context.Context, sub Subscription, ev LifecycleEvent, set WebhookSettings, run *webhookRun) error {
	u, err := ValidateWebhookURL(sub.Sink.Target, set.AllowHosts)
	if err != nil {
		return s.dropBeforeSend(run, WebhookOutcomeRefused, err)
	}
	run.host = u.Host

	// Whatever the resolver returned is remembered before the error check, so
	// an error that echoes the value is scrubbed like any other text.
	secret, err := s.resolveSecret(sub.Sink.SecretRef)
	run.secret = secret
	if err != nil {
		return s.dropBeforeSend(run, WebhookOutcomeFailed, err)
	}

	body, err := s.buildBody(run, sub, ev, set.MaxBodyBytes)
	if err != nil {
		return s.dropBeforeSend(run, WebhookOutcomeFailed, err)
	}

	client := s.client(set)
	for attempt := 1; attempt <= set.MaxAttempts; attempt++ {
		if cerr := ctx.Err(); cerr != nil {
			return s.abandon(run, attempt-1, cerr)
		}
		run.attempts = attempt
		res := s.post(ctx, client, u, set, run, body, ev.Kind)
		run.status = res.status
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
func (s *WebhookSink) buildBody(run *webhookRun, sub Subscription, ev LifecycleEvent, maxBytes int) ([]byte, error) {
	env := WebhookEnvelope{
		Version:        WebhookEnvelopeVersion,
		DeliveryID:     run.deliveryID,
		SentAt:         run.start.UTC(),
		Host:           s.hostname(ev),
		SubscriptionID: sub.ID,
		Event:          ev,
		// Forward is set only when the event arrived from a peer; the top-level
		// block then names the origin host, the hops and the visited list.
		Forward: ev.Forward,
	}
	return marshalWebhookEnvelope(env, maxBytes)
}

// marshalWebhookEnvelope serialises env for either sink. Over maxBytes the
// event's decision and verdict are dropped and the envelope is marked
// truncated; still over, it is an error. A maxBytes below 1 means the default cap.
func marshalWebhookEnvelope(env WebhookEnvelope, maxBytes int) ([]byte, error) {
	if maxBytes < 1 {
		maxBytes = WebhookMaxBodyBytes
	}
	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("the event could not be encoded: %w", err)
	}
	if len(body) <= maxBytes {
		return body, nil
	}
	env.Event.Decision, env.Event.Verdict, env.Truncated = nil, nil, true
	body, err = json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("the event could not be encoded: %w", err)
	}
	if len(body) > maxBytes {
		return nil, fmt.Errorf("the payload is %d bytes, over the %d byte cap", len(body), maxBytes)
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
		return webhookTransportAttempt(err, ctx.Err() != nil)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, webhookMaxResponseBytes))
	return classifyWebhookStatus(resp.StatusCode, resp.Header.Get("Retry-After"), s.cfg.Now())
}

// webhookTransportAttempt classifies a request that never got a response. A
// guard refusal is permanent; so is an attempt whose context ended; anything
// else is retried.
func webhookTransportAttempt(err error, ctxEnded bool) webhookAttempt {
	res := webhookAttempt{err: err, retry: true}
	switch {
	case errors.Is(err, ErrWebhookGuard):
		res.retry, res.guard = false, true
	case ctxEnded:
		res.retry = false
	}
	return res
}

// classifyWebhookStatus maps a receiver's status to an attempt result: 2xx is
// delivered; 408, 429 and 5xx are retried (a 429 Retry-After is honoured); every
// other status, redirects included, is permanent.
func classifyWebhookStatus(code int, retryAfter string, now time.Time) webhookAttempt {
	res := webhookAttempt{status: code}
	switch {
	case code >= 200 && code <= 299:
		res.ok = true
	case code == http.StatusRequestTimeout, code == http.StatusTooManyRequests, code >= 500:
		res.retry = true
		res.err = fmt.Errorf("receiver answered %d", code)
		if code == http.StatusTooManyRequests {
			res.retryAfter = parseRetryAfter(retryAfter, now)
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
	return webhookBackoff(set.curve(), n, retryAfter, s.cfg.Jitter())
}

// retryCurve is the delay policy both senders share. An explicit schedule wins
// when one is set (its last entry repeats); otherwise the geometric curve
// base * factor^(n-1) applies, capped at ceiling. jitterPercent spreads each
// delay by +- that percentage (a value below 1 means the default).
type retryCurve struct {
	schedule      []time.Duration
	base          time.Duration
	ceiling       time.Duration
	jitterPercent int
}

// webhookBackoff is the shared delay curve of the webhook and peer senders:
// the delay for the failed attempt n (1-based) from c, spread by jitter in
// [0,1), raised to a Retry-After when that is longer.
func webhookBackoff(c retryCurve, n int, retryAfter time.Duration, jitter float64) time.Duration {
	var d time.Duration
	if len(c.schedule) > 0 {
		i := n - 1
		if i < 0 {
			i = 0
		}
		if i >= len(c.schedule) {
			i = len(c.schedule) - 1
		}
		d = c.schedule[i]
	} else {
		d = c.base
		for i := 1; i < n && d < c.ceiling; i++ {
			d *= webhookBackoffFactor
		}
		if d > c.ceiling {
			d = c.ceiling
		}
	}
	spread := float64(c.jitterPercent) / 100
	if c.jitterPercent < 1 {
		spread = float64(config.DefaultNotifyJitterPercent) / 100
	}
	j := jitter
	if j < 0 {
		j = 0
	} else if j > 1 {
		j = 1
	}
	d = time.Duration(float64(d) * (1 + spread*(2*j-1)))
	if retryAfter > d {
		d = retryAfter
	}
	return d
}

// wait sleeps d, returning ctx's error when ctx ended first.
func (s *WebhookSink) wait(ctx context.Context, d time.Duration) error {
	return waitForRetry(ctx, s.cfg.Sleep, d)
}

// waitForRetry sleeps d through sleep when it is set, else on a timer that a
// cancelled context interrupts. It returns ctx's error when ctx ended first.
func waitForRetry(ctx context.Context, sleep func(time.Duration), d time.Duration) error {
	if sleep != nil {
		sleep(d)
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
	return webhookErrText(err, run.rawTarget, run.secret)
}

// webhookErrText renders err for a record, an alert or a returned error. The
// HTTP stack's own wrapper is removed; then rawTarget and secret are scrubbed.
func webhookErrText(err error, rawTarget, secret string) string {
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
	return scrubWebhookText(err.Error(), rawTarget, secret)
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
