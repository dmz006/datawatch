// BL427 (phase 5) — federation peer forwarding for persisted lifecycle
// subscriptions. Design: docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md
// (sections 2(d), 2(e) and 3: loop prevention, URL policy and audit).
//
// A peer subscription (SinkPeer) names a federation peer that is registered
// in the multi-server registry. PeerForwarder.Forward POSTs the event to that
// peer's receiving endpoint (ForwardedEventsPath) as a forwarded envelope:
//
//   - the address, the bearer token and the TLS-skip setting come from the
//     registry through LookupPeer, so a subscription never carries a
//     credential;
//   - the request is authenticated with the peer's bearer token, signed with
//     the same timestamped HMAC as the webhook sink (the token is the HMAC key,
//     as in the federation hop chain), and carries the federation hop-chain
//     header built by federation.BuildOrExtendChain;
//   - the retry policy, the response classification, the audit records and the
//     one alert on a dropped delivery are those of the webhook sink; a
//     peer that stays offline is dropped after its last attempt (no durable
//     outbox: forwarding is best-effort, the sending daemon's own Automaton
//     state is the record).
//
// Loop prevention (proposal section 3): a daemon never forwards an event it
// received as forwarded. An event carries its incoming chain in
// LifecycleEvent.Forward; a non-nil chain marks it as forwarded and the
// forwarder refuses it, naming the loop rule that applies. A locally
// originated event leaves with a chain stamped with this host.
//
// Import cycle: the registry, the local host name, the settings, the HTTP
// client factory, the audit writer and the alert store are injected function
// values (PeerForwarderConfig). This package does not import internal/server
// or internal/session. The daemon passes multiserver.HTTPClient, so a peer's
// TLS skip-verify setting behaves like every other peer call.

package autonomous

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// ForwardedEventsPath is the receiving endpoint on a peer daemon, appended to
// the peer's registered base URL. Pending operator confirmation: the proposal
// names this path. The receiving endpoint itself is implemented separately.
const ForwardedEventsPath = "/api/autonomous/events/forward"

// DefaultForwardMaxHops is the hop cap of a forwarded event: the number of
// daemons an event may pass through, counting the sender. Pending operator
// confirmation (the proposal's recommended value, section 3).
const DefaultForwardMaxHops = config.DefaultNotifyMaxHops

// MaxPeerNameLen caps the registered peer name a peer subscription names.
const MaxPeerNameLen = 64

// peerNameRe is the charset a peer subscription's target may use. The
// federation peer API (internal/server/federation_peers_api.go) does not
// validate a name charset; this one is the choice made here. Pending operator
// confirmation.
var peerNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// forwardActorDefault names the origin actor of a forwarded call when the
// subscription has no owner (the federation hop chain carries it).
const forwardActorDefault = "lifecycle-subscription"

// ErrForwardDelivery is wrapped by the error Forward returns for a forward
// that was dropped (retries exhausted, a permanent response, a dropped
// delivery). ErrForwardRefused marks a forward that was never sent because a
// precondition or a loop rule refused it.
var (
	ErrForwardDelivery = errors.New("peer forwarding failed")
	ErrForwardRefused  = errors.New("peer forward refused")
)

// ForwardSettings is the live configuration of the peer forwarder. A zero
// MaxHops, MaxAttempts, BackoffBase, BackoffMax or Timeout falls back to its
// default. Enabled defaults to false: forwarding ships disabled until the
// operator confirms the proposal.
type ForwardSettings struct {
	Enabled     bool
	MaxHops     int
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
	// MaxBodyBytes is the envelope cap: a forwarded envelope over it is
	// refused by the receiver. It is the same cap the webhook sink uses.
	MaxBodyBytes int
}

// DefaultForwardSettings returns the disabled-by-default settings with the
// proposal's recommended retry policy (the webhook values, section 2(e)).
func DefaultForwardSettings() ForwardSettings {
	return ForwardSettings{
		MaxHops:       DefaultForwardMaxHops,
		MaxAttempts:   DefaultWebhookMaxAttempts,
		Backoff:       secondsToDurations(config.DefaultNotifyBackoffSeconds()),
		BackoffBase:   DefaultWebhookBackoffBase,
		BackoffMax:    DefaultWebhookBackoffMax,
		JitterPercent: config.DefaultNotifyJitterPercent,
		Timeout:       DefaultWebhookTimeout,
		MaxBodyBytes:  config.DefaultNotifyMaxBodyBytes,
	}
}

func (s ForwardSettings) withDefaults() ForwardSettings {
	if s.MaxHops <= 0 {
		s.MaxHops = DefaultForwardMaxHops
	}
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
	return s
}

// curve is the delay policy the peer forwarder uses.
func (s ForwardSettings) curve() retryCurve {
	return retryCurve{
		schedule:      s.Backoff,
		base:          s.BackoffBase,
		ceiling:       s.BackoffMax,
		jitterPercent: s.JitterPercent,
	}
}

// PeerRef is the part of a federation peer registry entry the forwarder needs.
// LookupPeer returns ok=false for an unknown entry and for a disabled one.
type PeerRef struct {
	Name          string
	URL           string
	Token         string
	TLSSkipVerify bool
	// Federated is the registry's Federated flag: only a federation peer
	// (added through the federation peer API) may receive forwarded events.
	Federated bool
	// HasAutonomousWrite reports that the peer's resolved capabilities include
	// autonomous:write, the grant that lets a peer deliver events into this daemon.
	HasAutonomousWrite bool
}

// ForwardChain records how an event has travelled between daemons. On an
// envelope it is the "forward" block (origin_host, hops, visited); on an event
// it is set only by the receiving side and marks the event as forwarded.
type ForwardChain struct {
	OriginHost string   `json:"origin_host"`
	Hops       int      `json:"hops"`
	Visited    []string `json:"visited"`
}

// PeerForwarderConfig is the dependency set of the peer forwarder. Every
// entry is an injected function value. Settings == nil means disabled. A nil
// LookupPeer, LocalHost, NewClient or an empty local host name refuses every
// forward. A nil Audit or RaiseAlert drops that output. The remaining entries
// are test seams with production defaults.
type PeerForwarderConfig struct {
	// LookupPeer resolves a registered peer by name. It returns ok=false for an
	// unknown or disabled entry.
	LookupPeer func(name string) (PeerRef, bool)
	// LocalHost names this daemon in the envelope, the visited list and the
	// hop chain. It is the federation self-name.
	LocalHost func() string
	// Settings returns the live settings; it is called on every forward.
	Settings func() ForwardSettings
	// NewClient builds the HTTP client for one attempt (the daemon passes
	// multiserver.HTTPClient). The peer's TLS skip-verify setting is passed in.
	NewClient func(skipVerify bool, timeout time.Duration) *http.Client
	// Audit records one JSON-lines + CEF audit entry (the SetAuditFn path).
	Audit func(action string, details map[string]any)
	// RaiseAlert records an alert in the alert store.
	RaiseAlert func(level, title, body string)

	// Sleep waits between attempts. Nil waits on a timer that a cancelled
	// context interrupts.
	Sleep func(time.Duration)
	// Now is the clock. Nil is time.Now.
	Now func() time.Time
	// Jitter returns a value in [0,1) that spreads each delay. Nil is math/rand.
	Jitter func() float64
}

// PeerForwarder forwards lifecycle events to federation peers. Forward is safe
// for concurrent use: the forwarder keeps no per-delivery state.
type PeerForwarder struct {
	cfg PeerForwarderConfig
}

// NewPeerForwarder builds the forwarder, filling the seams cfg leaves nil.
func NewPeerForwarder(cfg PeerForwarderConfig) *PeerForwarder {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Jitter == nil {
		cfg.Jitter = mrand.Float64
	}
	return &PeerForwarder{cfg: cfg}
}

// Enabled reports whether the live settings allow forwarding; a nil forwarder
// is disabled. The session-push engine asks first, so a one-shot peer
// subscription stays armed while forwarding is off.
func (f *PeerForwarder) Enabled() bool {
	return f != nil && f.cfg.Settings != nil && f.cfg.Settings().Enabled
}

// ValidatePeerName checks a peer subscription's target: a non-empty name of at
// most MaxPeerNameLen characters from the peer-name charset.
func ValidatePeerName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("sink target is required: the registered peer name")
	}
	if len(name) > MaxPeerNameLen {
		return fmt.Errorf("peer name is %d characters; limit is %d", len(name), MaxPeerNameLen)
	}
	if !peerNameRe.MatchString(name) {
		// The name is not echoed: a pasted credential in the target field must not come back in the answer.
		return fmt.Errorf("peer name may contain only letters, digits and . _ -")
	}
	return nil
}

// ValidatePeerSubscriptionTarget checks the parts of a peer sink that depend on
// the live settings and the peer registry: forwarding must be enabled, and the
// named peer must exist, be a federation peer and grant autonomous:write. It
// returns nil for every other sink type. The store runs it on Add and Update
// (SetSinkValidator), so a peer subscription is refused at creation.
func ValidatePeerSubscriptionTarget(sub Subscription, settings ForwardSettings, lookup func(name string) (PeerRef, bool)) error {
	if sub.Sink.Type != SinkPeer {
		return nil
	}
	if !settings.Enabled {
		return fmt.Errorf("peer forwarding is disabled; set autonomous.notify.forwarding.enabled to true before creating a peer subscription")
	}
	if err := ValidatePeerName(sub.Sink.Target); err != nil {
		return err
	}
	if lookup == nil {
		return fmt.Errorf("no federation peer registry is configured")
	}
	peer, ok := lookup(sub.Sink.Target)
	if !ok {
		return fmt.Errorf("peer %q is not a registered, enabled federation peer", sub.Sink.Target)
	}
	if !peer.Federated {
		return fmt.Errorf("peer %q is not a federation peer", sub.Sink.Target)
	}
	if !peer.HasAutonomousWrite {
		return fmt.Errorf("peer %q does not grant the autonomous:write capability", sub.Sink.Target)
	}
	return nil
}

// extendForwardChain returns the chain an event leaves this host with. A
// locally originated event (prior nil) starts a chain at local; a received one
// extends its chain with local. prior is never mutated.
func extendForwardChain(prior *ForwardChain, local string) ForwardChain {
	if prior == nil {
		return ForwardChain{OriginHost: local, Hops: 1, Visited: []string{local}}
	}
	return ForwardChain{
		OriginHost: prior.OriginHost,
		Hops:       prior.Hops + 1,
		Visited:    append(append([]string(nil), prior.Visited...), local),
	}
}

// forwardRefusal returns why an event carrying chain must not leave this host,
// or "" when it may. A locally originated event (nil chain) may always leave.
// A received event never does: the reason names the first loop rule that
// applies, or the forwarded marker when none of the chain rules does.
func forwardRefusal(chain *ForwardChain, local string, maxHops int) string {
	switch {
	case chain == nil:
		return ""
	case chain.OriginHost == local:
		return "the event originated on this host"
	case forwardVisited(chain.Visited, local):
		return "this host already appears in the visited list"
	case chain.Hops >= maxHops:
		return "the hop cap was reached"
	}
	return "the event was already forwarded to this host; a forwarded event is never forwarded on"
}

func forwardVisited(visited []string, host string) bool {
	for _, v := range visited {
		if v == host {
			return true
		}
	}
	return false
}

// forwardEndpoint returns the receiving URL on a peer: its registered base URL
// plus ForwardedEventsPath. Only http and https are accepted, and user
// information is refused so a credential can never ride in a URL. The errors
// are fixed text: they never echo the URL.
func forwardEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil {
		return nil, errors.New("the peer URL could not be parsed")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("the peer URL must be an absolute http or https URL")
	}
	if u.User != nil {
		return nil, errors.New("the peer URL must not contain user information")
	}
	out := *u
	out.Path = strings.TrimRight(u.Path, "/") + ForwardedEventsPath
	out.RawQuery, out.Fragment = "", ""
	return &out, nil
}

// forwardRun is the state of one Forward call.
type forwardRun struct {
	subID      string
	deliveryID string
	peer       string
	kind       string
	local      string
	host       string // host:port of the peer endpoint, "unknown" until resolved
	hops       int
	maxAttempt int
	start      time.Time
	rawTarget  string
	secret     string
	attempts   int // POSTs made so far
	status     int // status of the last response, 0 when none arrived
}

// Forward sends ev to the federation peer sub names. It returns nil when the
// event was delivered, or when forwarding is disabled (nothing is sent, audited
// or alerted). It returns an error wrapping ErrForwardRefused for a refused
// forward and ErrForwardDelivery for a dropped one; both raise one alert and
// write the final audit record. An error wrapping ctx.Err() means the context
// ended first (no alert: the forward was abandoned, not refused).
func (f *PeerForwarder) Forward(ctx context.Context, sub Subscription, ev LifecycleEvent) error {
	_, err := f.ForwardReport(ctx, sub, ev)
	return err
}

// ForwardReport is Forward that also reports the attempt count and the last
// response status. Disabled forwarding returns the zero report and nil.
func (f *PeerForwarder) ForwardReport(ctx context.Context, sub Subscription, ev LifecycleEvent) (DeliveryReport, error) {
	if f == nil || f.cfg.Settings == nil {
		return DeliveryReport{}, nil
	}
	set := f.cfg.Settings().withDefaults()
	if !set.Enabled {
		return DeliveryReport{}, nil
	}

	run := &forwardRun{
		subID:      sub.ID,
		deliveryID: newWebhookDeliveryID(),
		peer:       sub.Sink.Target,
		kind:       ev.Kind,
		host:       "unknown",
		maxAttempt: set.MaxAttempts,
		start:      f.cfg.Now(),
	}
	err := f.forward(ctx, sub, ev, set, run)
	return DeliveryReport{Attempts: run.attempts, Status: run.status}, err
}

// forward runs the refusal checks and the attempt loop for one forward; run
// carries its state.
func (f *PeerForwarder) forward(ctx context.Context, sub Subscription, ev LifecycleEvent, set ForwardSettings, run *forwardRun) error {
	if f.cfg.LocalHost != nil {
		run.local = strings.TrimSpace(f.cfg.LocalHost())
	}
	if run.local == "" {
		return f.dropBeforeSend(run, WebhookOutcomeRefused, errors.New("the local host name is unknown"))
	}
	if reason := forwardRefusal(ev.Forward, run.local, set.MaxHops); reason != "" {
		return f.dropBeforeSend(run, WebhookOutcomeRefused, errors.New(reason))
	}
	if f.cfg.LookupPeer == nil || f.cfg.NewClient == nil {
		return f.dropBeforeSend(run, WebhookOutcomeRefused, errors.New("peer forwarding is not wired to a peer registry and HTTP client"))
	}
	peer, ok := f.cfg.LookupPeer(sub.Sink.Target)
	if !ok {
		return f.dropBeforeSend(run, WebhookOutcomeRefused, fmt.Errorf("no registered, enabled federation peer named %q", sub.Sink.Target))
	}
	if !peer.Federated {
		return f.dropBeforeSend(run, WebhookOutcomeRefused, fmt.Errorf("peer %q is not a federation peer", sub.Sink.Target))
	}
	if !peer.HasAutonomousWrite {
		return f.dropBeforeSend(run, WebhookOutcomeRefused, fmt.Errorf("peer %q does not grant the autonomous:write capability", sub.Sink.Target))
	}
	if strings.TrimSpace(peer.Token) == "" {
		return f.dropBeforeSend(run, WebhookOutcomeRefused, fmt.Errorf("peer %q has no bearer token", sub.Sink.Target))
	}
	endpoint, err := forwardEndpoint(peer.URL)
	if err != nil {
		return f.dropBeforeSend(run, WebhookOutcomeRefused, err)
	}

	// The peer's address and token are known from here on: scrub them from
	// every text the run produces.
	run.host = endpoint.Host
	run.rawTarget = endpoint.String()
	run.secret = peer.Token

	chain := extendForwardChain(ev.Forward, run.local)
	run.hops = chain.Hops
	env := WebhookEnvelope{
		Version:        WebhookEnvelopeVersion,
		DeliveryID:     run.deliveryID,
		SentAt:         run.start.UTC(),
		Host:           run.local,
		SubscriptionID: sub.ID,
		Event:          ev,
		Forward:        &chain,
	}
	body, err := marshalWebhookEnvelope(env, set.MaxBodyBytes)
	if err != nil {
		return f.dropBeforeSend(run, WebhookOutcomeFailed, err)
	}

	client := f.client(peer, set)
	actor := strings.TrimSpace(sub.Owner)
	if actor == "" {
		actor = forwardActorDefault
	}
	for attempt := 1; attempt <= set.MaxAttempts; attempt++ {
		if cerr := ctx.Err(); cerr != nil {
			return f.abandon(run, attempt-1, cerr)
		}
		run.attempts = attempt
		res := f.post(ctx, client, endpoint, peer.Token, actor, set, run, body)
		run.status = res.status
		switch {
		case res.ok:
			f.attemptRecord(run, attempt, res.status, WebhookOutcomeDelivered, "")
			return nil
		case ctx.Err() != nil:
			return f.abandon(run, attempt, ctx.Err())
		case res.guard, res.status == http.StatusConflict:
			// A guard refusal or a peer's loop refusal (409) is permanent.
			return f.dropAfterAttempt(run, attempt, res, WebhookOutcomeRefused)
		case !res.retry || attempt == set.MaxAttempts:
			return f.dropAfterAttempt(run, attempt, res, WebhookOutcomeFailed)
		}
		f.attemptRecord(run, attempt, res.status, WebhookOutcomeRetry, f.errText(run, res.err))
		delay := webhookBackoff(set.curve(), attempt, res.retryAfter, f.cfg.Jitter())
		if werr := waitForRetry(ctx, f.cfg.Sleep, delay); werr != nil {
			return f.abandon(run, attempt, werr)
		}
	}
	return nil // unreachable: the last attempt always returns above
}

// client builds the HTTP client for one forward. The factory supplies the
// peer's TLS setting and timeout; a factory-supplied client can never follow a
// redirect either, so the bearer token is never sent to a second host.
func (f *PeerForwarder) client(peer PeerRef, set ForwardSettings) *http.Client {
	c := f.cfg.NewClient(peer.TLSSkipVerify, set.Timeout)
	if c == nil {
		c = &http.Client{Timeout: set.Timeout}
	}
	cc := *c
	cc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cc
}

// post makes one signed attempt and classifies the response.
func (f *PeerForwarder) post(ctx context.Context, client *http.Client, endpoint *url.URL, token, actor string, set ForwardSettings, run *forwardRun, body []byte) webhookAttempt {
	actx, cancel := context.WithTimeout(ctx, set.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(actx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return webhookAttempt{err: errors.New("the request could not be built")}
	}
	sentAt := f.cfg.Now()
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", webhookContentType)
	req.Header.Set("User-Agent", webhookUserAgent)
	req.Header.Set(WebhookSignatureHeader, SignWebhookBody([]byte(token), sentAt, body))
	req.Header.Set(WebhookDeliveryHeader, run.deliveryID)
	req.Header.Set(WebhookEventHeader, headerSafe(run.kind))
	// The federation hop chain names the origin actor to the receiver. The
	// bearer token is the HMAC key, as on every other federation call.
	if chain, cerr := federation.BuildOrExtendChain(federation.ContextWithPrincipal(actx, actor), run.local, token); cerr == nil && len(chain) > 0 {
		req.Header.Set(federation.HopChainHeader, federation.EncodeChain(chain))
	}

	resp, err := client.Do(req)
	if err != nil {
		return webhookTransportAttempt(err, ctx.Err() != nil)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, webhookMaxResponseBytes))
	return classifyWebhookStatus(resp.StatusCode, resp.Header.Get("Retry-After"), f.cfg.Now())
}

// ---------------------------------------------------------------------------
// Audit, alert and error text
// ---------------------------------------------------------------------------

func (f *PeerForwarder) auditEmit(action string, details map[string]any) {
	if f.cfg.Audit != nil {
		f.cfg.Audit(action, details)
	}
}

func (f *PeerForwarder) details(run *forwardRun, attempt, status int, outcome, errText string) map[string]any {
	return map[string]any{
		"subscription_id": run.subID,
		"delivery_id":     run.deliveryID,
		"peer":            run.peer,
		"event_kind":      run.kind,
		"host":            run.host,
		"attempt":         attempt,
		"max_attempts":    run.maxAttempt,
		"status_code":     status,
		"outcome":         outcome,
		"error":           errText,
		"hops":            run.hops,
		"duration_ms":     f.cfg.Now().Sub(run.start).Milliseconds(),
	}
}

// attemptRecord writes the record of one attempt. attempt is 0 when no request
// was made. The last record of a dropped forward carries the final outcome.
func (f *PeerForwarder) attemptRecord(run *forwardRun, attempt, status int, outcome, errText string) {
	f.auditEmit(audit.ActionLifecycleForwardDelivery, f.details(run, attempt, status, outcome, errText))
}

// dropBeforeSend ends a forward that never reached the network.
func (f *PeerForwarder) dropBeforeSend(run *forwardRun, outcome string, cause error) error {
	text := f.errText(run, cause)
	f.attemptRecord(run, 0, 0, outcome, text)
	return f.drop(run, 0, outcome, text)
}

// dropAfterAttempt ends a forward on its attempt-th (failed) POST.
func (f *PeerForwarder) dropAfterAttempt(run *forwardRun, attempt int, res webhookAttempt, outcome string) error {
	text := f.errText(run, res.err)
	f.attemptRecord(run, attempt, res.status, outcome, text)
	return f.drop(run, attempt, outcome, text)
}

// drop raises the one alert of a dropped forward and builds the returned error.
// A refused forward wraps ErrForwardRefused; every other drop wraps
// ErrForwardDelivery.
func (f *PeerForwarder) drop(run *forwardRun, attempts int, outcome string, text string) error {
	if f.cfg.RaiseAlert != nil {
		f.cfg.RaiseAlert(webhookAlertLevel, "Lifecycle peer forward failed",
			fmt.Sprintf("Subscription %s: forwarding to peer %s was dropped after %d attempt(s): %s",
				run.subID, run.peer, attempts, text))
	}
	sentinel := ErrForwardDelivery
	if outcome == WebhookOutcomeRefused {
		sentinel = ErrForwardRefused
	}
	return fmt.Errorf("%w: peer %s: %s", sentinel, run.peer, text)
}

// abandon ends a forward whose context ended: recorded as skipped, no alert.
func (f *PeerForwarder) abandon(run *forwardRun, attempts int, cause error) error {
	f.attemptRecord(run, attempts, 0, WebhookOutcomeSkipped, "forward abandoned: "+cause.Error())
	return fmt.Errorf("peer forward abandoned: %w", cause)
}

func (f *PeerForwarder) errText(run *forwardRun, err error) string {
	return webhookErrText(err, run.rawTarget, run.secret)
}
