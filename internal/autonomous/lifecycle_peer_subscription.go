// Peer event subscription: this daemon reads the lifecycle events of a
// registered federation peer and feeds them into its own local dispatch, so a
// local subscription (comm, session, topic, alert, mcp_queue, webhook) sees a
// peer's events like any other. It is the pull direction of federation; the
// push direction is PeerForwarder (lifecycle_forward.go) and its receiving
// side (lifecycle_forward_receive.go).
//
// How a read works. Each subscribed peer has one goroutine. The goroutine
// repeatedly calls GET <peer URL>/api/autonomous/events?queue=<name>&since=<cursor>
// on the peer, which answers with the lifecycle events its named pending-events
// queue retains after the cursor, oldest first. Every request:
//
//   - is authenticated with the peer's stored bearer token, which comes from the
//     federation peer registry through LookupPeer on every poll, so a rotated
//     token, a disabled peer or a revoked capability takes effect on the next
//     read and a subscription never carries a credential;
//   - carries the same federation hop-chain header and the same timestamped HMAC
//     header (keyed by the bearer token, over the empty body) as the forwarder;
//   - is made only when the peer's resolved capabilities include
//     autonomous:read (federation.CapAutonomousRead), the grant the peer's own
//     events endpoint requires. A peer without it is refused before any request
//     is made: the refusal is audited and raises one alert, and no goroutine
//     is left running for that peer.
//
// What is and is not forwarded into local dispatch.
//
//	Forwarded: lifecycle events only, that is the events this daemon's own
//	emitter produces: an Automaton, task or Automation changing status, a task
//	session starting or clearing, and an Automaton update notice. An event is
//	accepted only when its kind and level are a pair this daemon emits (the same
//	check the receiving side applies to a pushed envelope).
//	Not forwarded: session output, secrets, configuration and any other peer
//	data. The subscription reads exactly one endpoint, and that endpoint returns
//	only lifecycle events, so none of those can arrive through it. An event
//	that the peer itself had received as forwarded (it carries a forward chain)
//	and a synthetic test event are dropped, which keeps a pair of peers that
//	read each other from echoing an event back and forth.
//
// What a local subscriber sees. Each accepted event is published with Peer set
// to the registered peer name (never a value from the payload), Host set to the
// peer's own host name, and Forward set. A non-nil Forward is the existing
// marker of a forwarded event: it is never forwarded on, is never written to
// the executor's state and does not reach the Automation coordinator, so a
// peer's events only feed notification sinks.
//
// Failure handling. Each poll runs under the configured timeout. A failed poll
// (network error, timeout, 408, 429, 5xx or an unreadable answer) is retried
// with the shared backoff curve (webhookBackoff), which honours Retry-After; after
// the configured number of consecutive failures one alert is raised and the
// goroutine keeps retrying at the longest delay until the peer answers again. A
// 401 or 403 means the peer no longer accepts this daemon's token or grant: that
// is permanent, so the goroutine stops after one audit record and one alert.
//
// Enabled follows ForwardSettings.Enabled, the one switch for federation event
// exchange in both directions. While it is off a running goroutine stays armed
// and reads nothing. This file adds no setting and no event kind.
//
// Import cycle: the registry, the local host name, the settings, the HTTP client
// factory, the audit writer, the alert store and the local publisher are
// injected function values (PeerEventSubscriberConfig). This package does not
// import internal/server or internal/session.

package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/federation"
)

// PeerEventsPath is the peer endpoint a peer event subscription reads, appended
// to the peer's registered base URL. It is the pending-events queue endpoint of
// a datawatch daemon.
const PeerEventsPath = "/api/autonomous/events"

const (
	// DefaultPeerEventPollInterval is the wait between two successful reads of
	// one peer.
	DefaultPeerEventPollInterval = 5 * time.Second
	// peerEventPageLimit is the page size requested on each read.
	peerEventPageLimit = 100
	// peerEventMaxResponseBytes caps what one read may return.
	peerEventMaxResponseBytes = 4 << 20
	// peerEventAlertTitle titles the alert raised when a subscription stops or
	// keeps failing.
	peerEventAlertTitle = "Lifecycle peer subscription failed"
)

// ErrPeerEventRefused marks a peer event subscription that was never started,
// or was stopped, because a precondition refused it (an unknown or disabled
// peer, a peer that is not a federation peer, a missing read capability, no
// token or an unusable URL). ErrPeerEventRunning means the peer already has a
// subscription goroutine.
var (
	ErrPeerEventRefused = errors.New("peer event subscription refused")
	ErrPeerEventRunning = errors.New("peer event subscription already running")
)

// PeerEventSource names what to read: the registered peer, the name of the
// pending-events queue on that peer, and the cursor to resume from. A zero
// SinceCursor reads the queue's whole retained backlog first.
type PeerEventSource struct {
	Peer        string
	Queue       string
	SinceCursor uint64
}

// PeerEventSubscriberConfig is the dependency set of the subscriber. Settings
// == nil means disabled. A nil LookupPeer, LocalHost, NewClient or Publish, and
// an empty local host name, refuse every subscription. A nil Audit or
// RaiseAlert drops that output. The remaining entries are test seams with
// production defaults.
type PeerEventSubscriberConfig struct {
	// LookupPeer resolves a registered peer by name; ok=false for an unknown or
	// disabled entry. It is called on every poll.
	LookupPeer func(name string) (PeerRef, bool)
	// LocalHost names this daemon in the hop-chain header and in the loop check.
	LocalHost func() string
	// Settings returns the live settings (Enabled, Timeout, MaxAttempts and the
	// backoff curve); it is called on every poll.
	Settings func() ForwardSettings
	// NewClient builds the HTTP client for one poll (the daemon passes
	// multiserver.HTTPClient). The peer's TLS skip-verify setting is passed in.
	NewClient func(skipVerify bool, timeout time.Duration) *http.Client
	// Publish hands one accepted event to local dispatch.
	Publish func(LifecycleEvent)
	// Audit records one JSON-lines + CEF audit entry.
	Audit func(action string, details map[string]any)
	// RaiseAlert records an alert in the alert store.
	RaiseAlert func(level, title, body string)

	// PollInterval is the wait between successful reads. Zero is
	// DefaultPeerEventPollInterval.
	PollInterval time.Duration
	// Sleep waits between polls. Nil waits on a timer that a cancelled context
	// interrupts.
	Sleep func(time.Duration)
	// Now is the clock. Nil is time.Now.
	Now func() time.Time
	// Jitter returns a value in [0,1) that spreads each delay. Nil is math/rand.
	Jitter func() float64
}

// PeerEventSubscriber runs one reading goroutine per subscribed peer. It is
// safe for concurrent use.
type PeerEventSubscriber struct {
	cfg  PeerEventSubscriberConfig
	mu   sync.Mutex
	runs map[string]context.CancelFunc
	wg   sync.WaitGroup
}

// NewPeerEventSubscriber builds the subscriber, filling the seams cfg leaves nil.
func NewPeerEventSubscriber(cfg PeerEventSubscriberConfig) *PeerEventSubscriber {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Jitter == nil {
		cfg.Jitter = mrand.Float64
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPeerEventPollInterval
	}
	return &PeerEventSubscriber{cfg: cfg, runs: map[string]context.CancelFunc{}}
}

// NewPeerEventSubscriber builds a subscriber whose events enter this Manager's
// local dispatch: cfg.Publish is set to the Manager's dispatcher. The event
// reaches every local subscription of every sink type, with the peer name set.
func (m *Manager) NewPeerEventSubscriber(cfg PeerEventSubscriberConfig) *PeerEventSubscriber {
	if m != nil {
		cfg.Publish = m.lifecycleBus().publish
	}
	return NewPeerEventSubscriber(cfg)
}

// peerEventRun is the state of one peer's goroutine.
type peerEventRun struct {
	src      PeerEventSource
	local    string
	host     string // host:port of the peer endpoint, "unknown" until resolved
	secret   string
	endpoint string
	failures int
	alerted  bool
}

// peerPoll is the result of one read.
type peerPoll struct {
	ok         bool
	permanent  bool
	status     int
	err        error
	retryAfter time.Duration
	events     []LifecycleEvent
	next       uint64
	dropped    bool
}

// Start begins reading src.Peer's events. The peer is checked first, with the
// peer's read capability among the checks: a refused peer returns an error
// wrapping ErrPeerEventRefused, writes one audit record, raises one alert and
// starts no goroutine. A peer that already has a goroutine returns
// ErrPeerEventRunning. The goroutine ends when ctx ends, when Stop or StopAll is
// called, or when the peer permanently refuses this daemon.
func (s *PeerEventSubscriber) Start(ctx context.Context, src PeerEventSource) error {
	if s == nil {
		return fmt.Errorf("%w: no subscriber is configured", ErrPeerEventRefused)
	}
	src.Peer = strings.TrimSpace(src.Peer)
	src.Queue = strings.TrimSpace(src.Queue)
	run := &peerEventRun{src: src, host: "unknown"}
	if s.cfg.LocalHost != nil {
		run.local = strings.TrimSpace(s.cfg.LocalHost())
	}
	if src.Queue == "" {
		return s.refuse(run, errors.New("the queue name is required"))
	}
	if err := ValidatePeerName(src.Peer); err != nil {
		return s.refuse(run, err)
	}
	if _, err := s.resolve(run); err != nil {
		return s.refuse(run, err)
	}

	s.mu.Lock()
	if _, busy := s.runs[src.Peer]; busy {
		s.mu.Unlock()
		return fmt.Errorf("%w: peer %s", ErrPeerEventRunning, src.Peer)
	}
	rctx, cancel := context.WithCancel(ctx)
	s.runs[src.Peer] = cancel
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.runs, src.Peer)
			s.mu.Unlock()
			cancel()
		}()
		s.loop(rctx, run)
	}()
	return nil
}

// Stop ends the goroutine of one peer. It reports whether one was running. It
// does not wait for the goroutine to finish.
func (s *PeerEventSubscriber) Stop(peer string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cancel, ok := s.runs[strings.TrimSpace(peer)]
	if ok {
		cancel()
	}
	return ok
}

// StopAll ends every goroutine and waits for them to finish.
func (s *PeerEventSubscriber) StopAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	for _, cancel := range s.runs {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// Running lists the peers that currently have a goroutine.
func (s *PeerEventSubscriber) Running() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.runs))
	for p := range s.runs {
		out = append(out, p)
	}
	return out
}

// resolve runs the checks that decide whether a read may be made and returns the
// peer. It records the peer's address and token on run so that every text the
// run produces can be scrubbed. Every failure is a refusal.
func (s *PeerEventSubscriber) resolve(run *peerEventRun) (PeerRef, error) {
	if run.local == "" {
		return PeerRef{}, errors.New("the local host name is unknown")
	}
	if s.cfg.LookupPeer == nil || s.cfg.NewClient == nil || s.cfg.Publish == nil {
		return PeerRef{}, errors.New("peer event subscription is not wired to a peer registry, an HTTP client and local dispatch")
	}
	peer, ok := s.cfg.LookupPeer(run.src.Peer)
	if !ok {
		return PeerRef{}, fmt.Errorf("no registered, enabled federation peer named %q", run.src.Peer)
	}
	if !peer.Federated {
		return PeerRef{}, fmt.Errorf("peer %q is not a federation peer", run.src.Peer)
	}
	if !peer.HasAutonomousRead {
		return PeerRef{}, fmt.Errorf("peer %q does not grant the %s capability", run.src.Peer, federation.CapAutonomousRead)
	}
	if strings.TrimSpace(peer.Token) == "" {
		return PeerRef{}, fmt.Errorf("peer %q has no bearer token", run.src.Peer)
	}
	endpoint, err := peerEndpoint(peer.URL, PeerEventsPath)
	if err != nil {
		return PeerRef{}, err
	}
	run.host = endpoint.Host
	run.endpoint = endpoint.String()
	run.secret = peer.Token
	return peer, nil
}

// refuse records a refusal (one audit record, one alert) and returns the error
// that wraps ErrPeerEventRefused.
func (s *PeerEventSubscriber) refuse(run *peerEventRun, cause error) error {
	text := webhookErrText(cause, run.endpoint, run.secret)
	s.audit(run, map[string]any{"outcome": WebhookOutcomeRefused, "reason": text})
	s.alert(run, fmt.Sprintf("Reading events from peer %s was refused: %s", run.src.Peer, text))
	return fmt.Errorf("%w: peer %s: %s", ErrPeerEventRefused, run.src.Peer, text)
}

// loop is the body of one peer's goroutine.
func (s *PeerEventSubscriber) loop(ctx context.Context, run *peerEventRun) {
	cursor := run.src.SinceCursor
	for ctx.Err() == nil {
		set := s.settings()
		if !set.Enabled {
			// Stay armed while federation event exchange is off.
			if s.wait(ctx, s.cfg.PollInterval) != nil {
				return
			}
			continue
		}
		peer, err := s.resolve(run)
		if err != nil {
			// The peer was removed, disabled or lost its read capability
			// while the subscription ran: stop.
			_ = s.refuse(run, err)
			return
		}
		res := s.poll(ctx, peer, run, cursor, set)
		switch {
		case ctx.Err() != nil:
			return
		case res.ok:
			run.failures, run.alerted = 0, false
			cursor = s.accept(run, peer, res, cursor)
			if s.wait(ctx, s.cfg.PollInterval) != nil {
				return
			}
		case res.permanent:
			text := webhookErrText(res.err, run.endpoint, run.secret)
			s.audit(run, map[string]any{"outcome": WebhookOutcomeRefused, "status_code": res.status, "reason": text})
			s.alert(run, fmt.Sprintf("Peer %s no longer accepts this daemon's token or grant; the subscription stopped: %s", run.src.Peer, text))
			return
		default:
			run.failures++
			text := webhookErrText(res.err, run.endpoint, run.secret)
			s.audit(run, map[string]any{"outcome": WebhookOutcomeRetry, "status_code": res.status, "reason": text, "failures": run.failures})
			if run.failures >= set.MaxAttempts && !run.alerted {
				run.alerted = true
				s.alert(run, fmt.Sprintf("Reading events from peer %s failed %d times in a row; it keeps retrying: %s", run.src.Peer, run.failures, text))
			}
			delay := webhookBackoff(set.curve(), run.failures, res.retryAfter, s.cfg.Jitter())
			if s.wait(ctx, delay) != nil {
				return
			}
		}
	}
}

// settings returns the live settings with defaults; a nil hook is disabled.
func (s *PeerEventSubscriber) settings() ForwardSettings {
	if s.cfg.Settings == nil {
		return ForwardSettings{}
	}
	return s.cfg.Settings().withDefaults()
}

func (s *PeerEventSubscriber) wait(ctx context.Context, d time.Duration) error {
	return waitForRetry(ctx, s.cfg.Sleep, d)
}

// poll makes one authenticated read under the configured timeout.
func (s *PeerEventSubscriber) poll(ctx context.Context, peer PeerRef, run *peerEventRun, cursor uint64, set ForwardSettings) peerPoll {
	endpoint, err := peerEndpoint(peer.URL, PeerEventsPath)
	if err != nil {
		return peerPoll{permanent: true, err: err}
	}
	q := url.Values{}
	q.Set("queue", run.src.Queue)
	q.Set("since", strconv.FormatUint(cursor, 10))
	q.Set("limit", strconv.Itoa(peerEventPageLimit))
	endpoint.RawQuery = q.Encode()

	actx, cancel := context.WithTimeout(ctx, set.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return peerPoll{err: errors.New("the request could not be built")}
	}
	sentAt := s.cfg.Now()
	req.Header.Set("Authorization", "Bearer "+peer.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", webhookUserAgent)
	req.Header.Set(WebhookSignatureHeader, SignWebhookBody([]byte(peer.Token), sentAt, nil))
	if chain, cerr := federation.BuildOrExtendChain(federation.ContextWithPrincipal(actx, forwardActorDefault), run.local, peer.Token); cerr == nil && len(chain) > 0 {
		req.Header.Set(federation.HopChainHeader, federation.EncodeChain(chain))
	}

	client := s.client(peer, set)
	resp, err := client.Do(req)
	if err != nil {
		att := webhookTransportAttempt(err, ctx.Err() != nil)
		return peerPoll{}.fromAttempt(att)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, webhookMaxResponseBytes))
		return peerPoll{permanent: true, status: resp.StatusCode, err: fmt.Errorf("peer answered %d", resp.StatusCode)}
	}
	att := classifyWebhookStatus(resp.StatusCode, resp.Header.Get("Retry-After"), s.cfg.Now())
	if !att.ok {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, webhookMaxResponseBytes))
		return peerPoll{}.fromAttempt(att)
	}
	var body struct {
		Events     []LifecycleEvent `json:"events"`
		NextCursor uint64           `json:"next_cursor"`
		Dropped    bool             `json:"dropped"`
	}
	if derr := json.NewDecoder(io.LimitReader(resp.Body, peerEventMaxResponseBytes)).Decode(&body); derr != nil {
		return peerPoll{status: resp.StatusCode, err: errors.New("the peer's answer could not be read")}
	}
	return peerPoll{ok: true, status: resp.StatusCode, events: body.Events, next: body.NextCursor, dropped: body.Dropped}
}

// fromAttempt copies the classification of a response into the poll result. A
// retryable result and a permanent one differ only in att.retry.
func (p peerPoll) fromAttempt(att webhookAttempt) peerPoll {
	p.status = att.status
	p.err = att.err
	p.retryAfter = att.retryAfter
	p.permanent = !att.retry && !att.ok
	return p
}

// client builds the HTTP client for one poll. A client the factory supplies can
// never follow a redirect either, so the bearer token is never sent to a second
// host.
func (s *PeerEventSubscriber) client(peer PeerRef, set ForwardSettings) *http.Client {
	c := s.cfg.NewClient(peer.TLSSkipVerify, set.Timeout)
	if c == nil {
		c = &http.Client{Timeout: set.Timeout}
	}
	cc := *c
	cc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cc
}

// accept stamps and publishes the events of one successful read and returns the
// cursor to read from next. An event that fails the checks is audited as
// refused and skipped; it never stops the read.
func (s *PeerEventSubscriber) accept(run *peerEventRun, peer PeerRef, res peerPoll, cursor uint64) uint64 {
	if res.dropped {
		s.audit(run, map[string]any{"outcome": WebhookOutcomeSkipped, "reason": "the peer's queue dropped events this daemon had not read yet"})
	}
	for _, ev := range res.events {
		out, reason := stampPeerEvent(run.src.Peer, run.local, ev, s.cfg.Now())
		if reason != "" {
			s.audit(run, map[string]any{"outcome": WebhookOutcomeRefused, "reason": reason, "event_id": auditPeerText(ev.ID), "event_kind": auditPeerText(ev.Kind)})
			continue
		}
		s.cfg.Publish(out)
		s.audit(run, map[string]any{
			"outcome": "accepted", "reason": "read from peer",
			"event_id": auditPeerText(out.ID), "event_kind": auditPeerText(out.Kind),
			"origin_host": auditPeerText(out.Host), "hops": out.Forward.Hops,
		})
	}
	if res.next > cursor {
		return res.next
	}
	return cursor
}

// stampPeerEvent turns an event read from a peer into the event local dispatch
// receives, or returns the reason it must not enter. The accepted event has its
// Peer set from peer (the registered name, never the payload), its Host set to
// the peer's own host name (the peer's name when the peer left it empty) and a
// one-hop Forward chain, which marks it as forwarded.
func stampPeerEvent(peer, local string, ev LifecycleEvent, now time.Time) (LifecycleEvent, string) {
	switch {
	case ev.Forward != nil:
		return ev, "the peer had received the event as forwarded; a forwarded event is never forwarded on"
	case ev.Test:
		return ev, "the event is a synthetic test event"
	case !knownForwardKind(ev):
		return ev, "the event kind is not one this daemon emits"
	}
	host := strings.TrimSpace(ev.Host)
	if host == "" {
		host = peer
	}
	if host == local {
		return ev, "the event originated on this host"
	}
	if !validForwardHost(host) {
		return ev, "the origin host is too long"
	}
	ev.Host = host
	ev.Peer = peer
	ev.Forward = &ForwardChain{OriginHost: host, Hops: 1, Visited: []string{host}}
	if ev.ID == "" {
		ev.ID = newLifecycleEventID()
	}
	if ev.At.IsZero() {
		ev.At = now
	}
	return ev, ""
}

// auditPeerText bounds a payload value written to the audit log.
func auditPeerText(v string) string {
	const max = 128
	if len(v) > max {
		return v[:max]
	}
	return v
}

// audit writes one record. The details never carry the token, the signature or
// the body.
func (s *PeerEventSubscriber) audit(run *peerEventRun, details map[string]any) {
	if s.cfg.Audit == nil {
		return
	}
	out := map[string]any{
		"direction": "read", "peer": run.src.Peer, "queue": run.src.Queue, "host": run.host,
	}
	for k, v := range details {
		out[k] = v
	}
	s.cfg.Audit(audit.ActionLifecycleForwardReceive, out)
}

func (s *PeerEventSubscriber) alert(run *peerEventRun, body string) {
	if s.cfg.RaiseAlert != nil {
		s.cfg.RaiseAlert(webhookAlertLevel, peerEventAlertTitle, body)
	}
}
