// BL427 (phase 5) — tests for webhook-sink subscriptions: create-time
// validation against the live settings, engine routing to the webhook sink,
// one-shot consumption only after success, failure alerting, stall isolation,
// audit redaction and store persistence. Receivers are httptest servers; the
// retry sleep is a no-op so every test runs at full speed.

package autonomous

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

const (
	webhookEngineHost   = "hooks.example.net"
	webhookEngineTarget = "https://hooks.example.net/datawatch"
)

// webhookRig is a Manager, a persisted store and a session-push engine whose
// webhook sink reads settings the test can change between steps. The store
// runs the creation check against those same settings.
type webhookRig struct {
	m     *Manager
	store *SubscriptionStore
	eng   *LifecycleSubscriptionEngine
	rec   *sessionPushRecorder

	mu     sync.Mutex
	set    WebhookSettings
	audits []webhookAuditRec
	alerts []webhookAlertRec
}

func newWebhookRig(t *testing.T, set WebhookSettings, sessions sessionTable) *webhookRig {
	t.Helper()
	r := &webhookRig{rec: &sessionPushRecorder{}, set: set}
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// The notify-events resolver gates every subscription sink: opt prd-1 into
	// task.session_started (an opt-in kind) so the delivery tests reach the sink.
	if err := m.Store().SavePRD(&PRD{ID: "prd-1", NotifyEvents: []string{KindTaskSessionStarted}}); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	store, err := NewSubscriptionStore(filepath.Join(t.TempDir(), "subs.json"), nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	store.SetSinkValidator(func(sub Subscription) error {
		return ValidateWebhookSubscriptionTarget(sub, r.settings())
	})
	sink := NewWebhookSink(WebhookSinkConfig{
		Settings: r.settings,
		ResolveSecret: func(ref string) (string, error) {
			if ref != webhookTestRef {
				return "", fmt.Errorf("no such secret %q", ref)
			}
			return webhookTestSecret, nil
		},
		Audit: func(action string, details map[string]any) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.audits = append(r.audits, webhookAuditRec{action, details})
		},
		RaiseAlert: func(level, title, body string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.alerts = append(r.alerts, webhookAlertRec{level, title, body})
		},
		Sleep: func(time.Duration) {},
	})
	r.m, r.store = m, store
	r.eng = NewLifecycleSubscriptionEngine(m, store, SessionPushConfig{
		FindSession: sessions.find,
		PostBridge:  r.rec.postBridge,
		SendInput:   r.rec.sendInput,
		Webhook:     sink,
	})
	if err := r.eng.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		r.eng.Close()
		m.lifecycleBus().Close()
	})
	return r
}

func (r *webhookRig) settings() WebhookSettings {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.set
	s.AllowHosts = append([]string(nil), s.AllowHosts...)
	return s
}

func (r *webhookRig) setSettings(fn func(*WebhookSettings)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(&r.set)
}

func (r *webhookRig) alertCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.alerts)
}

func (r *webhookRig) alertsSnapshot() []webhookAlertRec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]webhookAlertRec(nil), r.alerts...)
}

// auditOutcomeCount counts audit records carrying the given outcome.
func (r *webhookRig) auditOutcomeCount(outcome string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, rec := range r.audits {
		if rec.details["outcome"] == outcome {
			n++
		}
	}
	return n
}

// auditRecsFor returns the details of every record with action.
func (r *webhookRig) auditRecsFor(action string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]any
	for _, rec := range r.audits {
		if rec.action == action {
			out = append(out, rec.details)
		}
	}
	return out
}

// webhookRigSettings is an enabled sink that allow-lists the loopback
// receivers the tests start.
func webhookRigSettings(maxAttempts int) WebhookSettings {
	return WebhookSettings{
		Enabled:     true,
		AllowHosts:  []string{"127.0.0.1"},
		MaxAttempts: maxAttempts,
		Timeout:     5 * time.Second,
	}
}

// installWebhookLiveCheck gives store the live check a daemon installs: delivery
// enabled and the engine test host allow-listed.
func installWebhookLiveCheck(store *SubscriptionStore) {
	store.SetSinkValidator(func(sub Subscription) error {
		return ValidateWebhookSubscriptionTarget(sub, WebhookSettings{Enabled: true, AllowHosts: []string{webhookEngineHost}})
	})
}

// webhookEngineSub is a webhook subscription on the started-session kind.
func webhookEngineSub(target string, oneShot bool) Subscription {
	return Subscription{
		Owner:   "alice",
		Filter:  LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:    SubscriptionSink{Type: SinkWebhook, Target: target, SecretRef: webhookTestRef},
		OneShot: oneShot,
	}
}

// ---------------------------------------------------------------------------
// Create-time validation
// ---------------------------------------------------------------------------

// TestWebhookSubscriptionStructureNeedsNoLiveSettings checks the URL policy
// that ValidateSubscription applies without any webhook settings, so a store
// built without the live check still refuses a malformed webhook target. An
// http target passes here; the allow-list decides it at the live check.
func TestWebhookSubscriptionStructureNeedsNoLiveSettings(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		secret  string
		wantErr string
	}{
		{name: "https target", target: webhookEngineTarget, secret: webhookTestRef},
		{name: "http target is decided by the allow-list", target: "http://hooks.example.net/datawatch", secret: webhookTestRef},
		{name: "ftp scheme", target: "ftp://hooks.example.net/datawatch", secret: webhookTestRef, wantErr: "scheme must be https"},
		{name: "userinfo", target: "https://ops:hunter2@hooks.example.net/datawatch", secret: webhookTestRef, wantErr: "user name or password"},
		{name: "no host", target: "https:///datawatch", secret: webhookTestRef, wantErr: "no host"},
		{name: "empty target", target: "  ", secret: webhookTestRef, wantErr: "webhook URL is empty"},
		{name: "port out of range", target: "https://hooks.example.net:70000/datawatch", secret: webhookTestRef, wantErr: "port is not valid"},
		{name: "obfuscated IP spelling", target: "https://2130706433/datawatch", secret: webhookTestRef, wantErr: "non-canonical IP spelling"},
		{name: "missing secret reference", target: webhookEngineTarget, secret: "", wantErr: "signing secret reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := webhookEngineSub(tc.target, false)
			sub.Sink.SecretRef = tc.secret
			err := ValidateSubscription(sub, time.Now())
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSubscription: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateSubscription error = %v, want it to contain %q", err, tc.wantErr)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Errorf("error echoes the URL userinfo: %v", err)
			}
		})
	}
}

// TestWebhookSubscriptionStoreWithoutLiveCheckRefused proves a store with no
// live check installed refuses a webhook subscription on Add and Update, while
// a non-webhook subscription still goes through.
func TestWebhookSubscriptionStoreWithoutLiveCheckRefused(t *testing.T) {
	store, err := NewSubscriptionStore(filepath.Join(t.TempDir(), "subs.json"), nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	if _, err := store.Add(webhookEngineSub(webhookEngineTarget, false)); err == nil || !strings.Contains(err.Error(), "live webhook settings check") {
		t.Fatalf("Add without a live check: err = %v, want the live-check refusal", err)
	}
	comm := Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkComm, Target: "ops"},
	}
	added, err := store.Add(comm)
	if err != nil {
		t.Fatalf("Add comm without a live check: %v", err)
	}
	if _, err := store.Update(added.ID, webhookEngineSub(webhookEngineTarget, false)); err == nil || !strings.Contains(err.Error(), "live webhook settings check") {
		t.Fatalf("Update to a webhook sink without a live check: err = %v, want the live-check refusal", err)
	}
	if n := len(store.List("")); n != 1 {
		t.Errorf("store holds %d subscriptions, want only the comm one", n)
	}
}

func TestWebhookSubscriptionCreateValidation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Subscription)
		set     func(*WebhookSettings)
		wantErr string
	}{
		{name: "https target on an allow-listed host with a secret reference"},
		{
			name:    "missing secret reference",
			mutate:  func(s *Subscription) { s.Sink.SecretRef = "" },
			wantErr: "signing secret reference",
		},
		{
			name:    "literal secret refused",
			mutate:  func(s *Subscription) { s.Sink.SecretRef = "literal-secret-value" },
			wantErr: "single ${secret:name} reference",
		},
		{
			name:    "secret reference with surrounding text refused",
			mutate:  func(s *Subscription) { s.Sink.SecretRef = "Bearer " + webhookTestRef },
			wantErr: "single ${secret:name} reference",
		},
		{
			name:    "host not on the allow-list",
			set:     func(s *WebhookSettings) { s.AllowHosts = []string{"other.example.net"} },
			wantErr: "not on the webhook allow-list",
		},
		{
			name:    "empty allow-list",
			set:     func(s *WebhookSettings) { s.AllowHosts = nil },
			wantErr: "allow-list is empty",
		},
		{
			name:    "delivery disabled",
			set:     func(s *WebhookSettings) { s.Enabled = false },
			wantErr: "webhook delivery is disabled",
		},
		{
			name:    "non-https scheme",
			mutate:  func(s *Subscription) { s.Sink.Target = "ftp://hooks.example.net/datawatch" },
			wantErr: "scheme must be https",
		},
		{
			name:    "userinfo in the URL",
			mutate:  func(s *Subscription) { s.Sink.Target = "https://ops:hunter2@hooks.example.net/datawatch" },
			wantErr: "user name or password",
		},
		{
			name: "action on a webhook sink",
			mutate: func(s *Subscription) {
				s.Action = &SubscriptionAction{Type: SinkActionSendInput, Text: "hi"}
			},
			wantErr: "session sink",
		},
		{
			name:    "URL without a host",
			mutate:  func(s *Subscription) { s.Sink.Target = "https:///datawatch" },
			wantErr: "no host",
		},
		{
			name: "cloud metadata address refused even when listed",
			mutate: func(s *Subscription) {
				s.Sink.Target = "https://169.254.169.254/latest"
			},
			set:     func(s *WebhookSettings) { s.AllowHosts = []string{"169.254.169.254"} },
			wantErr: "webhook target",
		},
		{
			name: "secret on a comm sink refused",
			mutate: func(s *Subscription) {
				s.Sink = SubscriptionSink{Type: SinkComm, Target: "ops", SecretRef: webhookTestRef}
			},
			set:     func(s *WebhookSettings) { s.Enabled = false },
			wantErr: "only valid with a webhook sink",
		},
		{
			name: "comm sink ignores the webhook settings",
			mutate: func(s *Subscription) {
				s.Sink = SubscriptionSink{Type: SinkComm, Target: "ops"}
			},
			set: func(s *WebhookSettings) { s.Enabled = false },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := webhookEngineSub(webhookEngineTarget, false)
			set := WebhookSettings{Enabled: true, AllowHosts: []string{webhookEngineHost}}
			if tc.mutate != nil {
				tc.mutate(&sub)
			}
			if tc.set != nil {
				tc.set(&set)
			}
			err := ValidateSubscription(sub, time.Now())
			if err == nil {
				err = ValidateWebhookSubscriptionTarget(sub, set)
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
			for _, leak := range []string{"literal-secret-value", "hunter2"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("error text leaks %q: %v", leak, err)
				}
			}
		})
	}
}

// TestWebhookSubscriptionStoreRefusesCreateAndUpdateWhileDisabled proves the
// creation check runs on Add and on Update through the store.
func TestWebhookSubscriptionStoreRefusesCreateAndUpdateWhileDisabled(t *testing.T) {
	r := newWebhookRig(t, WebhookSettings{AllowHosts: []string{"127.0.0.1"}}, nil)
	target := newHookServer(t, 200).target()

	if _, err := r.store.Add(webhookEngineSub(target, false)); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("Add while disabled: error = %v, want a disabled refusal", err)
	}
	if n := len(r.store.List("")); n != 0 {
		t.Fatalf("store holds %d subscriptions after a refused Add, want 0", n)
	}

	comm, err := r.store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkComm, Target: "ops"},
	})
	if err != nil {
		t.Fatalf("Add comm: %v", err)
	}
	if _, err := r.store.Update(comm.ID, webhookEngineSub(target, false)); err == nil {
		t.Fatal("Update to a webhook sink while disabled was accepted")
	}
	if got, _ := r.store.Get(comm.ID); got.Sink.Type != SinkComm {
		t.Fatalf("refused Update changed the stored sink to %q", got.Sink.Type)
	}

	r.setSettings(func(s *WebhookSettings) { s.Enabled = true })
	if _, err := r.store.Update(comm.ID, webhookEngineSub(target, false)); err != nil {
		t.Fatalf("Update once enabled: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Engine delivery
// ---------------------------------------------------------------------------

// TestWebhookSubscriptionPostsSignedMatchingEventOnly: a non-matching event is
// never posted; a matching one is posted once, signed, with the envelope.
func TestWebhookSubscriptionPostsSignedMatchingEventOnly(t *testing.T) {
	srv := newHookServer(t, 200)
	r := newWebhookRig(t, webhookRigSettings(1), nil)
	sub, err := r.store.Add(webhookEngineSub(srv.target(), false))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// The subscriber's queue is FIFO, so once the matching event has posted,
	// the non-matching one before it has had its chance to post too.
	r.m.emit(LifecycleEvent{Kind: KindTaskSessionCleared, Level: LevelTask, PRDID: "prd-1", TaskID: "task-1"})
	r.m.emit(startedEvent())
	waitFor(t, "matching event delivered", func() bool { return srv.hits() == 1 })

	req := srv.requests()[0]
	if err := VerifyWebhookSignature([]byte(webhookTestSecret), req.header.Get(WebhookSignatureHeader), req.body, time.Now(), WebhookSignatureTolerance); err != nil {
		t.Errorf("signature does not verify: %v", err)
	}
	if got := req.header.Get(WebhookEventHeader); got != KindTaskSessionStarted {
		t.Errorf("event header = %q, want %q", got, KindTaskSessionStarted)
	}
	var env WebhookEnvelope
	if err := json.Unmarshal(req.body, &env); err != nil {
		t.Fatalf("envelope is not JSON: %v", err)
	}
	if env.Version != WebhookEnvelopeVersion || env.SubscriptionID != sub.ID {
		t.Errorf("envelope version/subscription = %d/%q, want %d/%q", env.Version, env.SubscriptionID, WebhookEnvelopeVersion, sub.ID)
	}
	if env.Event.Kind != KindTaskSessionStarted || env.Event.SessionID != "worker-1" {
		t.Errorf("envelope event = %q session %q", env.Event.Kind, env.Event.SessionID)
	}
	if env.DeliveryID == "" || env.DeliveryID != req.header.Get(WebhookDeliveryHeader) {
		t.Errorf("delivery id %q does not match header %q", env.DeliveryID, req.header.Get(WebhookDeliveryHeader))
	}
}

// TestWebhookSubscriptionOneShotRemovedOnlyAfterSuccess: a refused first
// delivery leaves the one-shot armed; the next event delivers and consumes it.
func TestWebhookSubscriptionOneShotRemovedOnlyAfterSuccess(t *testing.T) {
	srv := newHookServer(t, 503, 200)
	r := newWebhookRig(t, webhookRigSettings(1), nil)
	sub, err := r.store.Add(webhookEngineSub(srv.target(), true))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	r.m.emit(startedEvent())
	waitFor(t, "first attempt refused and alerted", func() bool { return srv.hits() == 1 && r.alertCount() == 1 })
	if _, ok := r.store.Get(sub.ID); !ok {
		t.Fatal("one-shot was removed after a refused delivery")
	}

	r.m.emit(startedEvent())
	waitFor(t, "one-shot removed after success", func() bool {
		_, ok := r.store.Get(sub.ID)
		return !ok
	})
	if hits := srv.hits(); hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
	if n := r.alertCount(); n != 1 {
		t.Errorf("alerts = %d, want 1 (only the refused delivery)", n)
	}
}

// TestWebhookSubscriptionFailingEndpointRaisesOneAlertAndStaysArmed: retries
// exhaust, one final failure record and one alert are written, and the
// one-shot stays armed for the next event.
func TestWebhookSubscriptionFailingEndpointRaisesOneAlertAndStaysArmed(t *testing.T) {
	srv := newHookServer(t, 500)
	r := newWebhookRig(t, webhookRigSettings(3), nil)
	sub, err := r.store.Add(webhookEngineSub(srv.target(), true))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	r.m.emit(startedEvent())
	waitFor(t, "alert raised", func() bool { return r.alertCount() == 1 })
	if hits := srv.hits(); hits != 3 {
		t.Errorf("hits = %d, want 3 attempts", hits)
	}
	failed := r.auditRecsFor(audit.ActionLifecycleWebhookFailed)
	if len(failed) != 1 || failed[0]["outcome"] != WebhookOutcomeFailed || failed[0]["attempt"] != 3 {
		t.Fatalf("final failure records = %v, want one failed record on attempt 3", failed)
	}
	alert := r.alertsSnapshot()[0]
	if !strings.Contains(alert.body, "127.0.0.1") {
		t.Errorf("alert does not name the host: %q", alert.body)
	}
	for _, leak := range []string{webhookTestQuery, webhookTestSecret} {
		if strings.Contains(alert.body, leak) {
			t.Errorf("alert leaks %q: %q", leak, alert.body)
		}
	}
	if _, ok := r.store.Get(sub.ID); !ok {
		t.Fatal("one-shot was removed after a failed delivery")
	}

	r.m.emit(startedEvent())
	waitFor(t, "second failure alerted", func() bool { return r.alertCount() == 2 })
	if _, ok := r.store.Get(sub.ID); !ok {
		t.Fatal("one-shot was removed after a second failed delivery")
	}
}

// TestWebhookSubscriptionStalledEndpointDoesNotDelaySession: a receiver that
// never answers holds only its own subscription's goroutine; a session
// subscription on the same event is still delivered.
func TestWebhookSubscriptionStalledEndpointDoesNotDelaySession(t *testing.T) {
	srv := newHookServer(t, 200)
	srv.handler = func(int, http.ResponseWriter, *http.Request) { <-srv.release }
	r := newWebhookRig(t, webhookRigSettings(1), sessionTable{"ops": 0})
	if _, err := r.store.Add(webhookEngineSub(srv.target(), false)); err != nil {
		t.Fatalf("Add webhook: %v", err)
	}
	if _, err := r.store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	}); err != nil {
		t.Fatalf("Add session: %v", err)
	}

	r.m.emit(startedEvent())
	waitFor(t, "webhook POST started and stalled", func() bool { return srv.hits() == 1 })
	waitFor(t, "session delivered while the webhook is stalled", func() bool { return r.rec.inputCount() == 1 })
	close(srv.release)
}

// TestWebhookSubscriptionDeleteAbortsInFlightDelivery: deleting a subscription
// cancels its delivery in progress; no retry follows.
func TestWebhookSubscriptionDeleteAbortsInFlightDelivery(t *testing.T) {
	srv := newHookServer(t, 200)
	srv.handler = func(int, http.ResponseWriter, *http.Request) { <-srv.release }
	r := newWebhookRig(t, webhookRigSettings(3), nil)
	sub, err := r.store.Add(webhookEngineSub(srv.target(), false))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	r.m.emit(startedEvent())
	waitFor(t, "webhook POST in flight", func() bool { return srv.hits() == 1 })
	if err := r.store.Delete(sub.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	waitFor(t, "in-flight delivery abandoned", func() bool { return r.auditOutcomeCount(WebhookOutcomeSkipped) == 1 })
	close(srv.release)
	if hits := srv.hits(); hits != 1 {
		t.Errorf("hits = %d after delete, want 1 (no retry)", hits)
	}
}

// TestWebhookSubscriptionDisabledSinkLeavesOneShotArmed: while the sink is
// disabled nothing is sent and the one-shot is not consumed; enabling it
// delivers the next event and then consumes the one-shot.
func TestWebhookSubscriptionDisabledSinkLeavesOneShotArmed(t *testing.T) {
	srv := newHookServer(t, 200)
	r := newWebhookRig(t, webhookRigSettings(1), nil)
	sub, err := r.store.Add(webhookEngineSub(srv.target(), true))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	r.setSettings(func(s *WebhookSettings) { s.Enabled = false })
	r.m.emit(startedEvent())
	// A bounded wait: the subscriber has had time to take the event.
	time.Sleep(100 * time.Millisecond)
	if hits := srv.hits(); hits != 0 {
		t.Fatalf("hits = %d while disabled, want 0", hits)
	}
	if _, ok := r.store.Get(sub.ID); !ok {
		t.Fatal("one-shot consumed while the sink was disabled")
	}

	r.setSettings(func(s *WebhookSettings) { s.Enabled = true })
	r.m.emit(startedEvent())
	waitFor(t, "one-shot removed after delivery", func() bool {
		_, ok := r.store.Get(sub.ID)
		return !ok
	})
	if hits := srv.hits(); hits != 1 {
		t.Errorf("hits = %d after enabling, want 1", hits)
	}
}

// TestWebhookSubscriptionWithoutSinkIsInert: an engine built without a webhook
// sink accepts webhook subscriptions and never delivers or panics.
func TestWebhookSubscriptionWithoutSinkIsInert(t *testing.T) {
	_, store, _ := newEngineForTest(t, &sessionPushRecorder{}, sessionTable{}, 0)
	installWebhookLiveCheck(store)
	sub, err := store.Add(webhookEngineSub("https://hooks.example.net/datawatch", true))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, ok := store.Get(sub.ID); !ok {
		t.Fatal("one-shot consumed by an engine with no webhook sink")
	}
}

// TestWebhookDeliveryScopeFollowsSubscription: an unrelated store change keeps
// a webhook subscription's delivery scope; changing or deleting the
// subscription ends it.
func TestWebhookDeliveryScopeFollowsSubscription(t *testing.T) {
	target := newHookServer(t, 200).target()
	r := newWebhookRig(t, webhookRigSettings(1), nil)
	sub, err := r.store.Add(webhookEngineSub(target, false))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	before, ok := r.eng.deliveryContext(sub.ID)
	if !ok {
		t.Fatal("no delivery scope for a live webhook subscription")
	}

	if _, err := r.store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkComm, Target: "ops"},
	}); err != nil {
		t.Fatalf("Add comm: %v", err)
	}
	same, _ := r.eng.deliveryContext(sub.ID)
	if same != before || before.Err() != nil {
		t.Fatal("an unrelated store change replaced or cancelled the delivery scope")
	}

	changed := webhookEngineSub(target, false)
	changed.Filter = LifecycleFilter{Kinds: []string{KindTaskSessionCleared}}
	if _, err := r.store.Update(sub.ID, changed); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if before.Err() == nil {
		t.Fatal("a changed subscription kept its old delivery scope")
	}
	after, ok := r.eng.deliveryContext(sub.ID)
	if !ok || after == before {
		t.Fatal("a changed subscription has no fresh delivery scope")
	}

	if err := r.store.Delete(sub.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := r.eng.deliveryContext(sub.ID); ok {
		t.Error("a deleted subscription still has a delivery scope")
	}
	if after.Err() == nil {
		t.Error("deleting a subscription did not cancel its delivery scope")
	}
}

// ---------------------------------------------------------------------------
// Audit redaction
// ---------------------------------------------------------------------------

// TestWebhookSubscriptionCreateAuditShowsHostOnly: the create record names the
// host and port, and nothing else from the URL, nor any secret value. A URL
// with userinfo is refused at creation (see the structure test), so the
// credential-like part here is the query string and the path.
func TestWebhookSubscriptionCreateAuditShowsHostOnly(t *testing.T) {
	_, store, e := newEngineForTest(t, &sessionPushRecorder{}, sessionTable{}, 0)
	installWebhookLiveCheck(store)
	var mu sync.Mutex
	var created []map[string]any
	e.auditHook = func(action string, details map[string]any) {
		if action == auditLifecycleSubCreate {
			mu.Lock()
			created = append(created, details)
			mu.Unlock()
		}
	}

	target := "https://hooks.example.net:8443/datawatch/hook?token=QUERYTOKEN42"
	if _, err := store.Add(webhookEngineSub(target, false)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(created) != 1 {
		t.Fatalf("create records = %d, want 1", len(created))
	}
	d := created[0]
	if d["target_host"] != "hooks.example.net:8443" {
		t.Errorf("target_host = %v, want hooks.example.net:8443", d["target_host"])
	}
	if d["sink"] != SinkWebhook {
		t.Errorf("sink = %v, want %q", d["sink"], SinkWebhook)
	}
	for k, v := range d {
		text := fmt.Sprint(v)
		for _, leak := range []string{"QUERYTOKEN42", "/datawatch", webhookTestRef, webhookTestSecret} {
			if strings.Contains(text, leak) {
				t.Errorf("audit field %s leaks %q", k, leak)
			}
		}
	}
}

// TestSubscriptionAuditDetailsTargetHostOnlyForWebhook: other sinks carry no
// target host in their audit record.
func TestSubscriptionAuditDetailsTargetHostOnlyForWebhook(t *testing.T) {
	d := subscriptionAuditDetails(Subscription{Sink: SubscriptionSink{Type: SinkSession, Target: "ops"}})
	if _, ok := d["target_host"]; ok {
		t.Errorf("session sink audit carries target_host: %v", d)
	}
}

// ---------------------------------------------------------------------------
// Sink enablement and persistence
// ---------------------------------------------------------------------------

func TestWebhookSinkEnabledReflectsSettings(t *testing.T) {
	var nilSink *WebhookSink
	if nilSink.Enabled() {
		t.Error("nil sink reports enabled")
	}
	if NewWebhookSink(WebhookSinkConfig{}).Enabled() {
		t.Error("sink without settings reports enabled")
	}
	on := NewWebhookSink(WebhookSinkConfig{Settings: func() WebhookSettings { return WebhookSettings{Enabled: true} }})
	if !on.Enabled() {
		t.Error("enabled settings report disabled")
	}
	off := NewWebhookSink(WebhookSinkConfig{Settings: func() WebhookSettings { return WebhookSettings{} }})
	if off.Enabled() {
		t.Error("disabled settings report enabled")
	}
}

// TestWebhookSubscriptionStorePersistsSecretRef: the reference round-trips
// through the store file, the file never holds the resolved value, and a file
// written before the field existed still loads.
func TestWebhookSubscriptionStorePersistsSecretRef(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subs.json")
	first, err := NewSubscriptionStore(path, nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	installWebhookLiveCheck(first)
	sub, err := first.Add(webhookEngineSub(webhookEngineTarget, false))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	if !strings.Contains(string(raw), `"secret_ref": "`+webhookTestRef+`"`) {
		t.Errorf("store file does not persist the secret reference under secret_ref")
	}
	if strings.Contains(string(raw), webhookTestSecret) {
		t.Errorf("store file holds the resolved secret")
	}

	reopened, err := NewSubscriptionStore(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := reopened.Get(sub.ID)
	if !ok {
		t.Fatal("subscription missing after reopen")
	}
	if got.Sink.SecretRef != webhookTestRef || got.Sink.Type != SinkWebhook || got.Sink.Target != webhookEngineTarget {
		t.Errorf("round-tripped sink = %+v", got.Sink)
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy.json")
	legacy := `[{"id":"sub-legacy","owner":"bob","filter":{"kinds":["task.session_started"]},` +
		`"sink":{"type":"session","target":"ops"},"created_at":"2026-10-20T09:00:00Z"}]`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	old, err := NewSubscriptionStore(legacyPath, nil)
	if err != nil {
		t.Fatalf("load legacy store: %v", err)
	}
	loaded, ok := old.Get("sub-legacy")
	if !ok {
		t.Fatal("legacy subscription not loaded")
	}
	if loaded.Sink.Type != SinkSession || loaded.Sink.SecretRef != "" {
		t.Errorf("legacy sink = %+v, want a session sink with no secret", loaded.Sink)
	}
}
