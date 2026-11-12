// lifecycle_webhook_surfaces_parity_test.go — proves that the webhook and peer
// lifecycle-subscription sinks are reachable and consistent on every operator
// surface: REST, MCP, CLI, the comm channel and the PWA.
//
// Strategy (modelled on plan_coverage_surfaces_parity_test.go): the shared
// contract literals are defined ONCE below. The REST layer is exercised
// SEMANTICALLY here through the real handlers, with the same live validator
// the daemon installs (autonomous.NewLifecycleSinkValidator), a real store, a
// real engine test-fire and httptest receivers. The other four surfaces are
// thin proxies over that REST contract, so for them the test checks the
// shared literals mechanically against each surface's source and the locale
// bundles. A rename or omission on any one surface fails this test.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// ── Shared contract (single source of truth for this test) ──────────

const (
	// Sink type names, as the REST body, the MCP sink_type argument, the CLI
	// --sink value, the comm sink= value and the PWA picker all spell them.
	pwSinkWebhook = "webhook"
	pwSinkPeer    = "peer"

	// REST sink object fields (the wire form every surface sends).
	pwFieldSinkType   = "type"
	pwFieldTarget     = "target"
	pwFieldSecretRef  = "secret_ref"
	pwFieldSinkObject = "sink"

	// REST test-fire answer fields.
	pwFieldTestStatus   = "status"
	pwFieldTestAttempts = "attempts"

	// MCP argument names.
	pwMCPSinkType   = "sink_type"
	pwMCPSinkTarget = "sink_target"
	pwMCPSecretRef  = "secret_ref"

	// CLI flag names (without the leading dashes).
	pwCLISinkFlag      = "sink"
	pwCLITargetFlag    = "target"
	pwCLISecretRefFlag = "secret-ref"

	// Comm verb key=value names.
	pwCommSinkKey      = "sink="
	pwCommTargetKey    = "target="
	pwCommSecretRefKey = "secret_ref="

	// The comm channel's owner identity in these tests.
	pwCommOwner = "test:grp1"

	// The reference a webhook carries, and the value it resolves to. The
	// value must never appear on any surface.
	pwRef         = "${secret:lifecycle-hook}"
	pwResolved    = "pw-signing-value-0123456789"
	pwLiteral     = "pw-literal-credential-value"
	pwPeerName    = "laptop"
	pwPeerToken   = "pw-peer-bearer-value"
	pwPeerForward = autonomous.ForwardedEventsPath
)

// pwNewLocaleKeys are the PWA strings added for the webhook and peer sinks. Every
// one must exist, non-empty, in all five locale bundles.
var pwNewLocaleKeys = []string{
	"lifecycle_sub_form_sink_webhook",
	"lifecycle_sub_form_sink_peer",
	"lifecycle_sub_form_target_webhook",
	"lifecycle_sub_form_target_peer",
	"lifecycle_sub_form_peer_select",
	"lifecycle_sub_form_peer_none",
	"lifecycle_sub_form_peer_load_failed",
	"lifecycle_sub_form_secret_ref",
	"lifecycle_sub_form_secret_help",
	"lifecycle_sub_form_err_webhook_url",
	"lifecycle_sub_form_err_secret_ref",
	"lifecycle_sub_form_err_peer",
	"lifecycle_sub_help_settings_failed",
	"lifecycle_sub_help_webhook_disabled",
	"lifecycle_sub_help_webhook_no_hosts",
	"lifecycle_sub_help_webhook_hosts",
	"lifecycle_sub_help_peer_disabled",
	"lifecycle_sub_help_peer",
	"lifecycle_sub_secret_ref",
	"lifecycle_sub_test_status",
	"lifecycle_sub_test_attempts",
}

// ── Receivers ──────────────────────────────────────────────────────

type pwReq struct {
	path string
	auth string
	body string
}

// pwRecv is an httptest receiver that answers with code and records requests.
type pwRecv struct {
	srv  *httptest.Server
	mu   sync.Mutex
	code int
	reqs []pwReq
}

func newPWRecv(t *testing.T) *pwRecv {
	t.Helper()
	rc := &pwRecv{code: http.StatusOK}
	rc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rc.mu.Lock()
		rc.reqs = append(rc.reqs, pwReq{path: r.URL.Path, auth: r.Header.Get("Authorization"), body: string(body)})
		code := rc.code
		rc.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(rc.srv.Close)
	return rc
}

func (rc *pwRecv) setCode(code int) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.code = code
}

func (rc *pwRecv) requests() []pwReq {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]pwReq(nil), rc.reqs...)
}

// ── Rig: the daemon's wiring, built from the same constructors ─────

type pwRig struct {
	s       *Server
	store   *autonomous.SubscriptionStore
	hook    *pwRecv
	peer    *pwRecv
	webhook autonomous.WebhookSettings
	forward autonomous.ForwardSettings
}

// newPWRig wires the webhook and peer sinks the way main.go does: the same
// live validator on the store, a webhook sink and a peer forwarder behind a
// real engine, and the engine's test firer on the store. The webhook receiver
// is allow-listed by its exact host:port.
func newPWRig(t *testing.T) *pwRig {
	t.Helper()
	rig := &pwRig{hook: newPWRecv(t), peer: newPWRecv(t)}
	hookURL, err := url.Parse(rig.hook.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	rig.webhook = autonomous.DefaultWebhookSettings()
	rig.webhook.Enabled = true
	rig.webhook.AllowHosts = []string{hookURL.Host} // exact host:port of the receiver
	rig.forward = autonomous.DefaultForwardSettings()
	rig.forward.Enabled = true

	lookup := func(name string) (autonomous.PeerRef, bool) {
		if name != pwPeerName {
			return autonomous.PeerRef{}, false
		}
		return autonomous.PeerRef{
			Name:               pwPeerName,
			URL:                rig.peer.srv.URL,
			Token:              pwPeerToken,
			Federated:          true,
			HasAutonomousWrite: true,
		}, true
	}

	webhookSink := autonomous.NewWebhookSink(autonomous.WebhookSinkConfig{
		Settings: func() autonomous.WebhookSettings { return rig.webhook },
		ResolveSecret: func(ref string) (string, error) {
			if ref != pwRef {
				return "", fmt.Errorf("no such secret %s", ref)
			}
			return pwResolved, nil
		},
		Sleep: func(time.Duration) {}, // retries run at full speed in tests
	})
	forwarder := autonomous.NewPeerForwarder(autonomous.PeerForwarderConfig{
		LookupPeer: lookup,
		LocalHost:  func() string { return "parity-local" },
		Settings:   func() autonomous.ForwardSettings { return rig.forward },
		NewClient: func(_ bool, timeout time.Duration) *http.Client {
			return &http.Client{Timeout: timeout}
		},
		Sleep: func(time.Duration) {},
	})

	store, err := autonomous.NewSubscriptionStore(filepath.Join(t.TempDir(), "lifecycle_subscriptions.json"), nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	store.SetSinkValidator(autonomous.NewLifecycleSinkValidator(
		func() autonomous.WebhookSettings { return rig.webhook },
		func() autonomous.ForwardSettings { return rig.forward },
		lookup,
	))
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	eng := autonomous.NewLifecycleSubscriptionEngine(mgr, store, autonomous.SessionPushConfig{
		Webhook:   webhookSink,
		Forwarder: forwarder,
	})
	store.SetTestFirer(eng.FireTest)

	rig.store = store
	rig.s = &Server{autonomousSubs: store}
	return rig
}

// ── Surface-shaped wire bodies ─────────────────────────────────────

// pwSinkBody is the sink object every surface sends. secret is set only for
// the webhook sink; no other sink carries the key.
func pwSinkBody(sinkType, target, secret string) map[string]any {
	sink := map[string]any{pwFieldSinkType: sinkType, pwFieldTarget: target}
	if secret != "" {
		sink[pwFieldSecretRef] = secret
	}
	return sink
}

// pwSubBody is one subscription body: an Automaton-level kind filter and a sink.
func pwSubBody(kind string, sink map[string]any) map[string]any {
	return map[string]any{
		"filter":          map[string]any{"kinds": []string{kind}},
		pwFieldSinkObject: sink,
	}
}

// pwCommSubBody is the body the comm channel sends: owned by the channel.
func pwCommSubBody(kind string, sink map[string]any) map[string]any {
	body := pwSubBody(kind, sink)
	body["owner"] = pwCommOwner
	body["coalesce"] = false
	return body
}

// pwDecodeSub decodes one subscription record from a response body.
func pwDecodeSub(t *testing.T, body string) autonomous.Subscription {
	t.Helper()
	var sub autonomous.Subscription
	if err := json.Unmarshal([]byte(body), &sub); err != nil {
		t.Fatalf("decode subscription %q: %v", body, err)
	}
	return sub
}

// pwList returns the listing for an optional owner filter.
func pwList(t *testing.T, s *Server, owner string) (string, []autonomous.Subscription) {
	t.Helper()
	path := "/api/autonomous/subscriptions"
	if owner != "" {
		path += "?owner=" + owner
	}
	rr := doSubs(t, s, http.MethodGet, path, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d body=%s", path, rr.Code, rr.Body.String())
	}
	var subs []autonomous.Subscription
	if err := json.Unmarshal(rr.Body.Bytes(), &subs); err != nil {
		t.Fatalf("decode listing: %v", err)
	}
	return rr.Body.String(), subs
}

// ── Semantic checks through the real REST handlers ─────────────────

func TestLifecycleWebhookSurfaces_RESTContract(t *testing.T) {
	t.Run("create webhook and peer, list never leaks the value", func(t *testing.T) {
		rig := newPWRig(t)
		before, _ := pwList(t, rig.s, "")

		rr := doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions",
			pwSubBody("task.completed", pwSinkBody(pwSinkWebhook, rig.hook.srv.URL+"/hook", pwRef)))
		if rr.Code != http.StatusCreated {
			t.Fatalf("create webhook: want 201, got %d body=%s", rr.Code, rr.Body.String())
		}
		hook := pwDecodeSub(t, rr.Body.String())
		if hook.Sink.Type != pwSinkWebhook || hook.Sink.SecretRef != pwRef {
			t.Fatalf("created webhook sink = %+v", hook.Sink)
		}

		rr = doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions",
			pwSubBody("prd.blocked", pwSinkBody(pwSinkPeer, pwPeerName, "")))
		if rr.Code != http.StatusCreated {
			t.Fatalf("create peer: want 201, got %d body=%s", rr.Code, rr.Body.String())
		}

		raw, subs := pwList(t, rig.s, "")
		if len(subs) != 2 {
			t.Fatalf("listing has %d subscriptions, want 2 (before: %s)", len(subs), before)
		}
		if !strings.Contains(raw, pwRef) {
			t.Errorf("listing must show the reference text %q", pwRef)
		}
		if strings.Contains(raw, pwResolved) {
			t.Fatalf("listing leaked the resolved signing value")
		}
	})

	t.Run("refusals are 400 with an operator message and never echo the secret", func(t *testing.T) {
		rig := newPWRig(t)
		count := func() int { _, subs := pwList(t, rig.s, ""); return len(subs) }
		start := count()

		cases := []struct {
			name    string
			body    map[string]any
			wantMsg string
			mustNot string
		}{
			{"host not on the allow-list",
				pwSubBody("task.completed", pwSinkBody(pwSinkWebhook, "https://elsewhere.example.net/hook", pwRef)),
				"allow-list", ""},
			{"literal secret in place of the reference",
				pwSubBody("task.completed", pwSinkBody(pwSinkWebhook, rig.hook.srv.URL+"/hook", pwLiteral)),
				"single ${secret:name}", pwLiteral},
			{"secret reference on a peer sink",
				pwSubBody("prd.blocked", pwSinkBody(pwSinkPeer, pwPeerName, pwRef)),
				"only valid with a webhook sink", ""},
			{"peer that is not registered",
				pwSubBody("prd.blocked", pwSinkBody(pwSinkPeer, "ghost", "")),
				"not a registered", ""},
		}
		for _, c := range cases {
			rr := doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions", c.body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("%s: want 400, got %d body=%s", c.name, rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), c.wantMsg) {
				t.Errorf("%s: message %q lacks %q", c.name, rr.Body.String(), c.wantMsg)
			}
			if c.mustNot != "" && strings.Contains(rr.Body.String(), c.mustNot) {
				t.Errorf("%s: answer echoed the secret %q", c.name, c.mustNot)
			}
		}

		rig.webhook.Enabled = false
		rr := doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions",
			pwSubBody("task.completed", pwSinkBody(pwSinkWebhook, rig.hook.srv.URL+"/hook", pwRef)))
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "webhook delivery is disabled") {
			t.Fatalf("webhook while disabled: %d %s", rr.Code, rr.Body.String())
		}
		rig.webhook.Enabled = true

		rig.forward.Enabled = false
		rr = doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions",
			pwSubBody("prd.blocked", pwSinkBody(pwSinkPeer, pwPeerName, "")))
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "peer forwarding is disabled") {
			t.Fatalf("peer while forwarding disabled: %d %s", rr.Code, rr.Body.String())
		}
		rig.forward.Enabled = true

		if got := count(); got != start {
			t.Fatalf("refused creates left %d records, want %d", got, start)
		}
	})

	t.Run("update keeps, switches and refuses", func(t *testing.T) {
		rig := newPWRig(t)
		rr := doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions",
			pwSubBody("task.completed", pwSinkBody(pwSinkWebhook, rig.hook.srv.URL+"/hook", pwRef)))
		if rr.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
		}
		id := pwDecodeSub(t, rr.Body.String()).ID

		rr = doSubs(t, rig.s, http.MethodPut, "/api/autonomous/subscriptions/"+id,
			pwSubBody("prd.blocked", pwSinkBody(pwSinkWebhook, rig.hook.srv.URL+"/hook", pwRef)))
		if rr.Code != http.StatusOK {
			t.Fatalf("update kinds: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		if got := pwDecodeSub(t, rr.Body.String()).Sink.SecretRef; got != pwRef {
			t.Errorf("update dropped the reference: %q", got)
		}

		rr = doSubs(t, rig.s, http.MethodPut, "/api/autonomous/subscriptions/"+id,
			pwSubBody("prd.blocked", pwSinkBody(pwSinkWebhook, rig.hook.srv.URL+"/hook", pwLiteral)))
		if rr.Code != http.StatusBadRequest || strings.Contains(rr.Body.String(), pwLiteral) {
			t.Fatalf("update with a literal: %d %s", rr.Code, rr.Body.String())
		}

		rr = doSubs(t, rig.s, http.MethodPut, "/api/autonomous/subscriptions/"+id,
			pwSubBody("prd.blocked", pwSinkBody(pwSinkPeer, pwPeerName, "")))
		if rr.Code != http.StatusOK {
			t.Fatalf("switch to peer: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		if sub := pwDecodeSub(t, rr.Body.String()); sub.Sink.Type != pwSinkPeer || sub.Sink.SecretRef != "" {
			t.Errorf("switched sink = %+v, want a peer with no reference", sub.Sink)
		}
	})

	t.Run("test fire delivers through the webhook and the peer and reports the outcome", func(t *testing.T) {
		rig := newPWRig(t)
		rr := doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions",
			pwSubBody("task.completed", pwSinkBody(pwSinkWebhook, rig.hook.srv.URL+"/hook", pwRef)))
		hookID := pwDecodeSub(t, rr.Body.String()).ID
		rr = doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions",
			pwSubBody("prd.blocked", pwSinkBody(pwSinkPeer, pwPeerName, "")))
		peerID := pwDecodeSub(t, rr.Body.String()).ID

		// Webhook: one POST, accepted.
		rr = doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions/"+hookID+"/test", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("webhook test: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		got := pwTestAnswer(t, rr.Body.String())
		if !got.OK || got.Sink != pwSinkWebhook || got.Status != http.StatusOK || got.Attempts != 1 {
			t.Fatalf("webhook outcome = %+v, want ok, webhook, 200, 1 attempt", got)
		}
		reqs := rig.hook.requests()
		if len(reqs) != 1 || !strings.Contains(reqs[0].body, `"test":true`) {
			t.Fatalf("webhook receiver got %+v, want one labelled test delivery", reqs)
		}

		// Peer: one forwarded envelope, bearer authenticated.
		rr = doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions/"+peerID+"/test", nil)
		got = pwTestAnswer(t, rr.Body.String())
		if !got.OK || got.Sink != pwSinkPeer || got.Status != http.StatusOK || got.Attempts != 1 {
			t.Fatalf("peer outcome = %+v, want ok, peer, 200, 1 attempt", got)
		}
		peerReqs := rig.peer.requests()
		if len(peerReqs) != 1 || peerReqs[0].path != pwPeerForward || peerReqs[0].auth != "Bearer "+pwPeerToken {
			t.Fatalf("peer receiver got %+v, want one bearer-authenticated forward to %s", peerReqs, pwPeerForward)
		}

		// A permanent refusal from the receiver reports its status, not a retry.
		rig.hook.setCode(http.StatusBadRequest)
		rr = doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions/"+hookID+"/test", nil)
		got = pwTestAnswer(t, rr.Body.String())
		if got.OK || got.Status != http.StatusBadRequest || got.Attempts != 1 || got.Error == "" {
			t.Fatalf("refused outcome = %+v, want a failed 400 after one attempt with an error", got)
		}
		if strings.Contains(rr.Body.String(), pwResolved) {
			t.Fatalf("test answer carried the signing value")
		}
	})
}

// pwTestAnswer is the test-fire answer as every surface reads it.
type pwTestAnswerBody struct {
	OK       bool   `json:"ok"`
	Sink     string `json:"sink"`
	Error    string `json:"error"`
	Status   int    `json:"status"`
	Attempts int    `json:"attempts"`
}

func pwTestAnswer(t *testing.T, body string) pwTestAnswerBody {
	t.Helper()
	var got pwTestAnswerBody
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode test answer %q: %v", body, err)
	}
	return got
}

// TestLifecycleWebhookSurfaces_CrossSurfaceVisibility checks that a subscription
// created over one surface's wire shape is listed and edited from the others.
// The comm channel lists by owner; the CLI and MCP list everything.
func TestLifecycleWebhookSurfaces_CrossSurfaceVisibility(t *testing.T) {
	rig := newPWRig(t)

	// Created over the comm shape: owned by the channel.
	rr := doSubs(t, rig.s, http.MethodPost, "/api/autonomous/subscriptions",
		pwCommSubBody("task.completed", pwSinkBody(pwSinkWebhook, rig.hook.srv.URL+"/hook", pwRef)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create over the comm shape: %d %s", rr.Code, rr.Body.String())
	}
	id := pwDecodeSub(t, rr.Body.String()).ID

	// Visible to the CLI/MCP shape (unfiltered) and to the comm shape (owner).
	for _, owner := range []string{"", pwCommOwner} {
		_, subs := pwList(t, rig.s, owner)
		found := false
		for _, s := range subs {
			if s.ID == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("subscription %s not visible with owner filter %q", id, owner)
		}
	}

	// Edited over the CLI/MCP shape (no owner, no coalesce key), then read back
	// over the comm shape: the new kinds and the reference both survive.
	rr = doSubs(t, rig.s, http.MethodPut, "/api/autonomous/subscriptions/"+id,
		pwSubBody("prd.blocked", pwSinkBody(pwSinkWebhook, rig.hook.srv.URL+"/hook", pwRef)))
	if rr.Code != http.StatusOK {
		t.Fatalf("edit over the CLI/MCP shape: %d %s", rr.Code, rr.Body.String())
	}
	_, subs := pwList(t, rig.s, pwCommOwner)
	for _, s := range subs {
		if s.ID == id {
			if s.Owner != pwCommOwner || s.Sink.SecretRef != pwRef || len(s.Filter.Kinds) != 1 || s.Filter.Kinds[0] != "prd.blocked" {
				t.Fatalf("edited record read over the comm shape = %+v", s)
			}
			return
		}
	}
	t.Fatalf("edited subscription %s not visible to the comm channel", id)
}

// TestLifecycleWebhookSurfaces_SourceContract checks each proxy surface's source
// and the PWA bundles against the shared literals. A rename or omission on any
// one of them fails here.
func TestLifecycleWebhookSurfaces_SourceContract(t *testing.T) {
	t.Run("MCP tool schema", func(t *testing.T) {
		readRel(t, "internal/mcp/autonomous_subscription_tools.go",
			`WithString("`+pwMCPSecretRef+`"`,
			`WithString("`+pwMCPSinkType+`"`,
			`WithString("`+pwMCPSinkTarget+`"`,
			// The sink names sit inside the Go string that describes the tool, so they appear escaped.
			`\"`+pwSinkWebhook+`\"`,
			`\"`+pwSinkPeer+`\"`,
			"subscriptionSinkBody(req)",
		)
		readRel(t, "internal/mcp/autonomous_subscription_update_tools.go",
			`WithString("`+pwMCPSecretRef+`"`,
			"subscriptionSinkBody(req)",
		)
	})

	t.Run("CLI flags and sink list", func(t *testing.T) {
		readRel(t, "cmd/datawatch/cli_autonomous_subscriptions.go",
			`"`+pwCLISecretRefFlag+`"`,
			`StringVar(&sinkType, "`+pwCLISinkFlag+`"`,
			`StringVar(&target, "`+pwCLITargetFlag+`"`,
			`"`+pwSinkWebhook+`":`, // gofmt aligns the map values, so match the key only
			`"`+pwSinkPeer+`":`,
			"applyLifecycleSecretRef(",
		)
		readRel(t, "cmd/datawatch/cli_autonomous_subscription_update.go",
			`"`+pwCLISecretRefFlag+`"`,
			"applyLifecycleSecretRef(",
		)
	})

	t.Run("comm verbs", func(t *testing.T) {
		readRel(t, "internal/router/autonomous_subscriptions.go",
			`"`+pwCommSecretRefKey+`"`,
			`"`+pwCommSinkKey+`"`,
			`"`+pwCommTargetKey+`"`,
			`"`+pwSinkWebhook+`"`,
			`"`+pwSinkPeer+`"`,
		)
		readRel(t, "internal/router/autonomous_subscription_update.go",
			`case "`+pwFieldSecretRef+`":`,
			`case "sink":`,
			`case "target":`,
			`json:"`+pwFieldTestStatus+`"`,
			`json:"`+pwFieldTestAttempts+`"`,
		)
	})

	t.Run("PWA picker, reference and test outcome", func(t *testing.T) {
		readRel(t, "internal/server/web/app.js",
			`'`+pwSinkWebhook+`'`,
			`'`+pwSinkPeer+`'`,
			`body.sink.`+pwFieldSecretRef+` = secretRef`,
			`lifecycle_sub_form_secret_ref`,
			`typeof res.`+pwFieldTestStatus+` === 'number'`,
			`typeof res.`+pwFieldTestAttempts+` === 'number'`,
		)
	})

	t.Run("locale bundles carry every new string", func(t *testing.T) {
		langs := []string{"en", "de", "es", "fr", "ja"}
		for _, lang := range langs {
			raw, err := os.ReadFile(filepath.Join("web", "locales", lang+".json"))
			if err != nil {
				t.Fatalf("read %s locale: %v", lang, err)
			}
			var bundle map[string]string
			if err := json.Unmarshal(raw, &bundle); err != nil {
				t.Fatalf("%s locale is not a flat string map: %v", lang, err)
			}
			for _, key := range pwNewLocaleKeys {
				if strings.TrimSpace(bundle[key]) == "" {
					t.Errorf("%s locale lacks %q", lang, key)
				}
			}
		}
	})

	t.Run("daemon answers carry the outcome fields", func(t *testing.T) {
		readRel(t, "internal/autonomous/lifecycle_subscription_test_fire.go",
			`json:"`+pwFieldTestStatus+`,omitempty"`,
			`json:"`+pwFieldTestAttempts+`,omitempty"`,
		)
	})
}
