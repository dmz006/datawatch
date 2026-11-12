// Tests for the test-fire path of the webhook and peer sinks: the outcome
// (attempts, last status, error) the REST, MCP, CLI and comm surfaces report,
// the disabled and unwired refusals, and the shared live validator that the
// daemon and the parity test both install. Receivers are httptest servers.

package autonomous

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// testFireReceiver is a webhook receiver that answers the scripted statuses in
// order (the last one repeats) and keeps every request body it was sent.
type testFireReceiver struct {
	srv      *httptest.Server
	mu       sync.Mutex
	statuses []int
	next     int
	bodies   []string
}

func newTestFireReceiver(t *testing.T, statuses ...int) *testFireReceiver {
	t.Helper()
	rc := &testFireReceiver{statuses: statuses}
	rc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rc.mu.Lock()
		rc.bodies = append(rc.bodies, string(body))
		code := http.StatusOK
		if len(rc.statuses) > 0 {
			i := rc.next
			if i >= len(rc.statuses) {
				i = len(rc.statuses) - 1
			}
			code = rc.statuses[i]
			rc.next++
		}
		rc.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(rc.srv.Close)
	return rc
}

func (rc *testFireReceiver) requestBodies() []string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]string(nil), rc.bodies...)
}

// testFireWebhookSub is a webhook subscription aimed at the receiver. The
// receiver's host:port is allow-listed by the rig's settings (127.0.0.1).
func testFireWebhookSub(store *SubscriptionStore, target string) (Subscription, error) {
	return store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkWebhook, Target: target, SecretRef: webhookTestRef},
	})
}

func TestSubscriptionFireTestWebhookReportsAttemptsAndStatus(t *testing.T) {
	rc := newTestFireReceiver(t, http.StatusInternalServerError, http.StatusOK)
	r := newWebhookRig(t, webhookRigSettings(3), sessionTable{})
	sub, err := testFireWebhookSub(r.store, rc.srv.URL+"/hook")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	res, err := r.eng.FireTest(sub.ID)
	if err != nil || !res.OK {
		t.Fatalf("FireTest: res=%+v err=%v", res, err)
	}
	if res.Sink != SinkWebhook || res.Attempts != 2 || res.Status != http.StatusOK || res.Error != "" {
		t.Fatalf("outcome = %+v, want sink webhook, 2 attempts, status 200, no error", res)
	}
	bodies := rc.requestBodies()
	if len(bodies) != 2 {
		t.Fatalf("receiver saw %d requests, want 2", len(bodies))
	}
	if !strings.Contains(bodies[1], `"test":true`) {
		t.Errorf("test delivery body lacks the test marker: %s", bodies[1])
	}
	if _, ok := r.store.Get(sub.ID); !ok {
		t.Fatal("a test delivery removed the subscription")
	}
}

func TestSubscriptionFireTestWebhookPermanentRefusalReportsStatus(t *testing.T) {
	rc := newTestFireReceiver(t, http.StatusBadRequest)
	r := newWebhookRig(t, webhookRigSettings(4), sessionTable{})
	sub, err := testFireWebhookSub(r.store, rc.srv.URL+"/hook")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	res, err := r.eng.FireTest(sub.ID)
	if err == nil || res.OK {
		t.Fatalf("FireTest: res=%+v err=%v, want a failed delivery", res, err)
	}
	if res.Status != http.StatusBadRequest || res.Attempts != 1 {
		t.Errorf("outcome = status %d attempts %d, want 400 after 1 attempt (4xx is not retried)", res.Status, res.Attempts)
	}
	if res.Error == "" || strings.Contains(res.Error, webhookTestSecret) {
		t.Errorf("error %q must be set and must not carry the signing secret", res.Error)
	}
	if got := len(rc.requestBodies()); got != 1 {
		t.Errorf("receiver saw %d requests, want 1", got)
	}
}

func TestSubscriptionFireTestWebhookDisabledSendsNothing(t *testing.T) {
	rc := newTestFireReceiver(t)
	r := newWebhookRig(t, webhookRigSettings(3), sessionTable{})
	sub, err := testFireWebhookSub(r.store, rc.srv.URL+"/hook")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	r.setSettings(func(s *WebhookSettings) { s.Enabled = false })

	res, err := r.eng.FireTest(sub.ID)
	if err == nil || res.OK {
		t.Fatalf("FireTest while disabled: res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Error, "autonomous.notify.webhook.enabled") {
		t.Errorf("disabled error must name the setting to enable: %q", res.Error)
	}
	if res.Attempts != 0 || res.Status != 0 {
		t.Errorf("disabled sink reported attempts %d status %d, want none", res.Attempts, res.Status)
	}
	if got := len(rc.requestBodies()); got != 0 {
		t.Errorf("disabled sink sent %d requests, want 0", got)
	}
}

func TestSubscriptionFireTestWebhookUnwiredSinkUnavailable(t *testing.T) {
	store, err := NewSubscriptionStore(filepath.Join(t.TempDir(), "subs.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	store.SetSinkValidator(func(Subscription) error { return nil })
	sub, err := store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkWebhook, Target: "https://hooks.example.net/x", SecretRef: webhookTestRef},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := NewLifecycleSubscriptionEngine(nil, store, SessionPushConfig{})
	res, err := e.FireTest(sub.ID)
	if err == nil || res.OK || !strings.Contains(err.Error(), "sink not available") {
		t.Fatalf("unwired webhook: res=%+v err=%v", res, err)
	}
}

func TestSubscriptionFireTestPeerForwardsOnceWithOutcome(t *testing.T) {
	peer := newFwdPeer(t, http.StatusOK)
	rig := newFwdRig(t, nil)
	rig.addPeer(fwdPeerRef(peer.srv.URL))
	eng, store, _ := newFwdEngine(t, rig)
	sub, err := store.Add(fwdSub(fwdPeerName))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	res, err := eng.FireTest(sub.ID)
	if err != nil || !res.OK {
		t.Fatalf("FireTest: res=%+v err=%v", res, err)
	}
	if res.Sink != SinkPeer || res.Attempts != 1 || res.Status != http.StatusOK {
		t.Fatalf("outcome = %+v, want sink peer, 1 attempt, status 200", res)
	}
	reqs := peer.requests()
	if len(reqs) != 1 {
		t.Fatalf("peer saw %d requests, want 1", len(reqs))
	}
	if !strings.Contains(string(reqs[0].body), `"test":true`) {
		t.Errorf("forwarded test envelope lacks the test marker: %s", reqs[0].body)
	}
	if _, ok := store.Get(sub.ID); !ok {
		t.Fatal("a test forward removed the subscription")
	}
}

func TestSubscriptionFireTestPeerDisabledSendsNothing(t *testing.T) {
	peer := newFwdPeer(t)
	rig := newFwdRig(t, nil)
	rig.addPeer(fwdPeerRef(peer.srv.URL))
	eng, store, _ := newFwdEngine(t, rig)
	sub, err := store.Add(fwdSub(fwdPeerName))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rig.setEnabled(false)

	res, err := eng.FireTest(sub.ID)
	if err == nil || res.OK {
		t.Fatalf("FireTest while disabled: res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Error, "autonomous.notify.forwarding.enabled") {
		t.Errorf("disabled error must name the setting to enable: %q", res.Error)
	}
	if got := len(peer.requests()); got != 0 {
		t.Errorf("disabled forwarder sent %d requests, want 0", got)
	}
}

func TestSubscriptionFireTestPeerUnknownPeerRefusedBeforeSend(t *testing.T) {
	peer := newFwdPeer(t)
	rig := newFwdRig(t, nil) // no peer registered under the target name
	eng, store, _ := newFwdEngine(t, rig)
	sub, err := store.Add(fwdSub("ghost-peer"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	res, err := eng.FireTest(sub.ID)
	if err == nil || res.OK {
		t.Fatalf("FireTest to unknown peer: res=%+v err=%v", res, err)
	}
	if res.Attempts != 0 || res.Status != 0 {
		t.Errorf("refused before send but reported attempts %d status %d", res.Attempts, res.Status)
	}
	if got := len(peer.requests()); got != 0 {
		t.Errorf("unknown peer produced %d requests, want 0", got)
	}
}

func TestSubscriptionFireTestPeerUnwiredForwarderUnavailable(t *testing.T) {
	store, err := NewSubscriptionStore(filepath.Join(t.TempDir(), "subs.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	store.SetSinkValidator(func(Subscription) error { return nil })
	sub, err := store.Add(fwdSub(fwdPeerName))
	if err != nil {
		t.Fatal(err)
	}
	e := NewLifecycleSubscriptionEngine(nil, store, SessionPushConfig{})
	res, err := e.FireTest(sub.ID)
	if err == nil || res.OK || !strings.Contains(err.Error(), "sink not available") {
		t.Fatalf("unwired peer: res=%+v err=%v", res, err)
	}
}

func TestNewLifecycleSinkValidatorChecksLiveSettings(t *testing.T) {
	webhookOn := func() WebhookSettings {
		return WebhookSettings{Enabled: true, AllowHosts: []string{"hooks.example.net"}}
	}
	webhookOff := func() WebhookSettings { return WebhookSettings{AllowHosts: []string{"hooks.example.net"}} }
	forwardOn := func() ForwardSettings { return ForwardSettings{Enabled: true} }
	forwardOff := func() ForwardSettings { return ForwardSettings{} }
	lookup := func(name string) (PeerRef, bool) {
		if name != fwdPeerName {
			return PeerRef{}, false
		}
		return fwdPeerRef("https://peer.example.net"), true
	}
	webhookSub := Subscription{Sink: SubscriptionSink{Type: SinkWebhook, Target: "https://hooks.example.net/x", SecretRef: webhookTestRef}}
	peerSub := fwdSub(fwdPeerName)
	commSub := Subscription{Sink: SubscriptionSink{Type: SinkComm, Target: "all"}}

	cases := []struct {
		name    string
		sub     Subscription
		webhook func() WebhookSettings
		forward func() ForwardSettings
		wantErr string
	}{
		{"webhook accepted when enabled and allow-listed", webhookSub, webhookOn, forwardOff, ""},
		{"webhook refused while disabled", webhookSub, webhookOff, forwardOff, "autonomous.notify.webhook.enabled"},
		{"peer accepted when forwarding enabled", peerSub, webhookOff, forwardOn, ""},
		{"peer refused while forwarding disabled", peerSub, webhookOff, forwardOff, "autonomous.notify.forwarding.enabled"},
		{"comm sink ignores both settings", commSub, webhookOff, forwardOff, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			check := NewLifecycleSinkValidator(c.webhook, c.forward, lookup)
			err := check(c.sub)
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("unexpected refusal: %v", err)
			case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
				t.Fatalf("err = %v, want one naming %q", err, c.wantErr)
			}
		})
	}
}

func TestValidatePeerNameDoesNotEchoInput(t *testing.T) {
	const pasted = "ghp_pasted credential!"
	err := ValidatePeerName(pasted)
	if err == nil {
		t.Fatal("a name with a space and a bang must be refused")
	}
	if strings.Contains(err.Error(), "ghp_pasted") {
		t.Fatalf("peer name error echoes the input: %v", err)
	}
	if ValidatePeerName("") == nil {
		t.Fatal("empty peer name must be refused")
	}
}

func TestWebhookDeliverReportZeroWhenDisabled(t *testing.T) {
	sink := NewWebhookSink(WebhookSinkConfig{Settings: func() WebhookSettings { return WebhookSettings{} }})
	rep, err := sink.DeliverReport(context.Background(), webhookEngineSub("https://127.0.0.1/x", false), LifecycleEvent{})
	if err != nil || rep != (DeliveryReport{}) {
		t.Fatalf("disabled DeliverReport = %+v, %v; want zero report and nil", rep, err)
	}
}
