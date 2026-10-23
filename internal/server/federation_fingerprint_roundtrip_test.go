// In-process daemon-to-daemon roundtrip tests for the
// fingerprint-reconciliation design
// (docs/plans/2026-10-14-concurrency-and-budget-design.md §3c):
// two real daemons (A and B) — full handler + auth middleware on
// httptest servers — exchange fingerprint hashes over the wire with
// the same compute node behind two different names/addresses, or the
// same LLM billing group, reconcile PENDING matches, confirm/reject
// them through the admin API, and hold the wire to "only hashes
// cross the network". Every inter-daemon request/response body is
// byte-captured and asserted free of the GPU UUID, the node address
// and the billing label.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/fingerprint"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/session"
)

// ---------------------------------------------------------------------------
// Wire recorder — every byte that crosses between daemons lands here
// verbatim (method, path, auth header, request and response bodies),
// so the "no raw identity on the wire" rule is asserted on real bytes.
// ---------------------------------------------------------------------------

type wireExchange struct {
	method   string
	path     string
	auth     string
	reqBody  string
	status   int
	respBody string
}

type wireLog struct {
	mu        sync.Mutex
	exchanges []wireExchange
}

func (w *wireLog) record(e wireExchange) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.exchanges = append(w.exchanges, e)
}

func (w *wireLog) all() []wireExchange {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]wireExchange, len(w.exchanges))
	copy(out, w.exchanges)
	return out
}

func (w *wireLog) find(method, path string) []wireExchange {
	var out []wireExchange
	for _, e := range w.all() {
		if e.method == method && e.path == path {
			out = append(out, e)
		}
	}
	return out
}

// assertClean fails the test if any captured byte contains any of the
// forbidden raw-identity strings (GPU UUIDs, node addresses, billing
// labels, raw pair tokens aside — the pair secret is shared by both
// sides by design and is expected in the Authorization header).
func (w *wireLog) assertClean(t *testing.T, who string, forbidden ...string) {
	t.Helper()
	for _, e := range w.all() {
		raw := strings.Join([]string{e.method, e.path, e.auth, e.reqBody, e.respBody}, "\x00")
		for _, f := range forbidden {
			if f == "" {
				continue
			}
			if strings.Contains(raw, f) {
				t.Errorf("%s: wire traffic carries forbidden raw identity %q\n  %s %s -> %d\n  req:  %q\n  resp: %q",
					who, f, e.method, e.path, e.status, e.reqBody, e.respBody)
			}
		}
	}
}

func (w *wireLog) serve(handler http.Handler, t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		reqBody, _ := io.ReadAll(r.Body)
		if len(reqBody) > 0 {
			r.Body = io.NopCloser(bytes.NewReader(reqBody))
		}
		cw := &captureWriter{ResponseWriter: rw}
		handler.ServeHTTP(cw, r)
		status := cw.status
		if status == 0 {
			status = http.StatusOK // implicit 200 when WriteHeader was never called
		}
		w.record(wireExchange{
			method:   r.Method,
			path:     r.URL.Path,
			auth:     r.Header.Get("Authorization"),
			reqBody:  string(reqBody),
			status:   status,
			respBody: cw.buf.String(),
		})
	}))
}

type captureWriter struct {
	http.ResponseWriter
	buf    bytes.Buffer
	status int
}

func (c *captureWriter) WriteHeader(code int) {
	c.status = code
	c.ResponseWriter.WriteHeader(code)
}

func (c *captureWriter) Write(p []byte) (int, error) {
	c.buf.Write(p)
	return c.ResponseWriter.Write(p)
}

// ---------------------------------------------------------------------------
// fpDaemon — a real *Server (auth middleware + fingerprint routes) on an
// httptest server with a wire recorder, a multiserver store holding the
// peer the daemon knows about, a fingerprint match store and an exporter
// over its own compute registry + LLM entries.
// ---------------------------------------------------------------------------

type fpDaemon struct {
	name     string
	adminTok string
	srv      *Server
	store    *multiserver.Store
	matches  *fingerprint.MatchStore
	exporter *fingerprint.Exporter
	wire     *wireLog
	ts       *httptest.Server
}

func newFPDaemon(t *testing.T, name, adminTok string, nodes []compute.Node, hostInfo map[string]map[string]any, llms []inference.LLM) *fpDaemon {
	t.Helper()
	dir := t.TempDir()

	store, err := multiserver.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("%s: store: %v", name, err)
	}
	matches, err := fingerprint.NewMatchStore(dir + "-matches.json")
	if err != nil {
		t.Fatalf("%s: match store: %v", name, err)
	}
	groups, err := federation.NewGroupStore(dir + "-groups")
	if err != nil {
		t.Fatalf("%s: group store: %v", name, err)
	}
	mgr, err := session.NewManager(name, dir+"-sesstok", "/bin/echo", 0)
	if err != nil {
		t.Fatalf("%s: session manager: %v", name, err)
	}

	resolver := &fingerprint.IdentityResolver{
		Nodes: func() []compute.Node { return nodes },
		HostInfo: func(observerPeer string) map[string]any {
			return hostInfo[observerPeer]
		},
	}
	exporter := &fingerprint.Exporter{
		Nodes: resolver,
		LLMs:  func() []inference.LLM { return llms },
	}

	s := &Server{
		manager:  mgr,
		hostname: name,
		token:    adminTok,
	}
	s.serverStore = store
	s.fedGroupStore = groups
	s.SetFingerprintExporter(exporter)
	s.SetFingerprintMatchStore(matches)

	mux := http.NewServeMux()
	mux.Handle("/api/federation/fingerprints",
		s.fedAuthMiddleware(http.HandlerFunc(s.handleFederationFingerprints)))
	mux.Handle("/api/federation/fingerprint-matches",
		s.fedAuthMiddleware(http.HandlerFunc(s.handleFingerprintMatches)))
	mux.Handle("/api/federation/fingerprint-matches/",
		s.fedAuthMiddleware(http.HandlerFunc(s.handleFingerprintMatchDecision)))

	wl := &wireLog{}
	d := &fpDaemon{
		name:     name,
		adminTok: adminTok,
		srv:      s,
		store:    store,
		matches:  matches,
		exporter: exporter,
		wire:     wl,
	}
	d.ts = wl.serve(mux, t)
	t.Cleanup(d.ts.Close)
	return d
}

// addPeer registers a peer entry the way POST /api/federation/peers
// would: federated, full-control caps (which include every federation
// capability) and an explicit FingerprintSync opt-in.
func (d *fpDaemon) addPeer(t *testing.T, name, url, tok string, fingerprintSync bool) {
	t.Helper()
	err := d.store.Add(&multiserver.Entry{
		Name:            name,
		URL:             url,
		Token:           tok,
		Enabled:         true,
		Federated:       true,
		Capabilities:    []string{"full-control"},
		FingerprintSync: fingerprintSync,
	})
	if err != nil {
		t.Fatalf("%s: add peer %s: %v", d.name, name, err)
	}
}

// peerEntries mirrors cmd/datawatch/fingerprint_wiring.go.
func (d *fpDaemon) peerEntries() []multiserver.Entry {
	out := []multiserver.Entry{}
	for _, e := range d.store.ListFederated() {
		out = append(out, *e)
	}
	return out
}

func (d *fpDaemon) syncer() *fingerprint.Syncer {
	return &fingerprint.Syncer{
		Peers:   d.peerEntries,
		Local:   d.exporter,
		Matches: d.matches,
	}
}

func (d *fpDaemon) url() string { return d.ts.URL }

func (d *fpDaemon) closePeer() { d.ts.Close() }

// gate mirrors the wiring in cmd/datawatch/fingerprint_wiring.go: the
// per-node local held/limit comes from capacityState().
func (d *fpDaemon) gate(localHeld func(pool string) (int, int)) *fingerprint.PeerGate {
	return &fingerprint.PeerGate{
		Matches: d.matches,
		Peers: func() []fingerprint.PeerRef {
			out := []fingerprint.PeerRef{}
			for _, e := range d.store.ListFederated() {
				out = append(out, fingerprint.PeerRef{Name: e.Name, URL: e.URL, Token: e.Token, OptedIn: e.FingerprintSync})
			}
			return out
		},
		LocalHeld: localHeld,
	}
}

func (d *fpDaemon) adminCall(t *testing.T, method, path string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, d.url()+path, nil)
	if err != nil {
		t.Fatalf("%s: %s %s: %v", d.name, method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+d.adminTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %s %s: %v", d.name, method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// One physical GPU node seen by daemon A as "shared-a" (LAN address) and by
// daemon B as "shared-b" (tailnet address): the fingerprint of the SAME node
// must reconcile both ways, transition PENDING -> confirmed -> rejected
// through the API, and never re-open once decided.
func TestFingerprintRoundTripPendingThenConfirm(t *testing.T) {
	sharedUUID := "GPU-4a1f9c0e-8d2b-4e33-a5c1-7f6d3b2a1e90"
	soloA := "GPU-0aa11bb2-2222-3333-4444-555566667777"
	soloB := "GPU-9dd88ee7-7777-6666-5555-444433332222"
	const (
		addrSharedA = "http://10.20.30.40:8000"
		addrSharedB = "http://10.20.31.40:8000"
		addrSoloA   = "http://10.20.30.42:8000"
		addrSoloB   = "http://10.20.31.42:8000"
	)
	pairTok := "fp-rt1-pair-ab-S3cr3t"

	a := newFPDaemon(t, "a", "fp-rt1-admin-a",
		[]compute.Node{
			{Name: "shared-a", Address: addrSharedA, ObserverPeer: "obs-a-shared"},
			{Name: "solo-a", Address: addrSoloA, ObserverPeer: "obs-a-solo"},
		},
		map[string]map[string]any{
			"obs-a-shared": {"gpu_uuids": []string{sharedUUID}},
			"obs-a-solo":   {"gpu_uuids": []string{soloA}},
		},
		nil)
	b := newFPDaemon(t, "b", "fp-rt1-admin-b",
		[]compute.Node{
			{Name: "shared-b", Address: addrSharedB, ObserverPeer: "obs-b-shared"},
			{Name: "solo-b", Address: addrSoloB, ObserverPeer: "obs-b-solo"},
		},
		map[string]map[string]any{
			"obs-b-shared": {"gpu_uuids": []string{sharedUUID}},
			"obs-b-solo":   {"gpu_uuids": []string{soloB}},
		},
		nil)

	a.addPeer(t, "b", b.url(), pairTok, true)
	b.addPeer(t, "a", a.url(), pairTok, true)

	forbidden := []string{sharedUUID, soloA, soloB, addrSharedA, addrSharedB, addrSoloA, addrSoloB}

	// A round-trips first: exact one PENDING match for the shared node,
	// keyed (b, KindNode, shared-a, shared-b).
	a.syncer().SyncOnce(context.Background())
	ml := a.matches.List(fingerprint.StatePending)
	if len(ml) != 1 {
		t.Fatalf("daemon a expected exactly one pending match after sync, got %d: %+v", len(ml), ml)
	}
	m := ml[0]
	if m.Peer != "b" || m.Kind != fingerprint.KindNode || m.LocalName != "shared-a" || m.PeerName != "shared-b" {
		t.Fatalf("unexpected match: %+v", m)
	}
	matchID := m.ID

	// B round-trips the same shared node back the other way.
	b.syncer().SyncOnce(context.Background())
	if bl := b.matches.List(fingerprint.StatePending); len(bl) != 1 || bl[0].Peer != "a" || bl[0].LocalName != "shared-b" || bl[0].PeerName != "shared-a" {
		t.Fatalf("daemon b expected one pending (a, shared-b): %+v", bl)
	}

	// Admin API: list, confirm, reject — each transition reflected both in
	// the response and in the underlying store.
	status, body := a.adminCall(t, http.MethodGet, "/api/federation/fingerprint-matches")
	if status != http.StatusOK {
		t.Fatalf("list: status %d body %s", status, body)
	}
	var listedEnv struct {
		Matches []map[string]any `json:"matches"`
	}
	if err := json.Unmarshal(body, &listedEnv); err != nil || len(listedEnv.Matches) != 1 {
		t.Fatalf("list body: %q (%v)", body, err)
	}
	if listedEnv.Matches[0]["state"] != fingerprint.StatePending || listedEnv.Matches[0]["id"] != matchID {
		t.Fatalf("unexpected listed match: %v", listedEnv.Matches[0])
	}

	status, body = a.adminCall(t, http.MethodPost, "/api/federation/fingerprint-matches/"+matchID+"/confirm")
	if status != http.StatusOK {
		t.Fatalf("confirm: status %d body %s", status, body)
	}
	got, ok := a.matches.Get(matchID)
	if !ok || got.State != fingerprint.StateConfirmed {
		t.Fatalf("after confirm: match not in store: %+v", got)
	}

	status, body = a.adminCall(t, http.MethodPost, "/api/federation/fingerprint-matches/"+matchID+"/reject")
	if status != http.StatusOK {
		t.Fatalf("reject: status %d body %s", status, body)
	}
	got, ok = a.matches.Get(matchID)
	if !ok || got.State != fingerprint.StateRejected {
		t.Fatalf("after reject: match not in store: %+v", got)
	}
	if pl := a.matches.List(fingerprint.StatePending); len(pl) != 0 {
		t.Fatalf("pending should be empty after reject, got %+v", pl)
	}

	// A second sync round-trip must NOT re-open the decided match and must
	// not open a second one for the same node.
	for _, d := range []*fpDaemon{a, b} {
		d.syncer().SyncOnce(context.Background())
	}
	if pl := a.matches.List(fingerprint.StatePending); len(pl) != 0 {
		t.Fatalf("re-sync re-opened a pending match: %+v", pl)
	}
	all := a.matches.List("")
	if len(all) != 1 || all[0].ID != matchID || all[0].State != fingerprint.StateRejected {
		t.Fatalf("re-sync changed the match set: %+v", all)
	}

	// No raw identity on the wire — on EITHER daemon, including the admin
	// list/confirm/reject responses.
	a.wire.assertClean(t, "a", forbidden...)
	b.wire.assertClean(t, "b", forbidden...)
}

// The same LLM billing group behind two differently-named LLM entries must
// reconcile as exactly one PENDING billing match — and only that match: the
// two different GPU UUIDs on either side must not produce node matches.
func TestFingerprintRoundTripBillingGroup(t *testing.T) {
	const (
		uuidA = "GPU-b111aa22-aaaa-bbbb-cccc-dddd00000001"
		uuidB = "GPU-b111aa22-aaaa-bbbb-cccc-dddd00000002"
		addrA = "http://192.168.7.11:11434"
		addrB = "http://192.168.7.12:11434"
	)
	const billingLabel = "team-x-billing"
	pairTok := "fp-rt2-pair-ab-S3cr3t"

	a := newFPDaemon(t, "a", "fp-rt2-admin-a",
		[]compute.Node{{Name: "solo-a", Address: addrA, ObserverPeer: "obs-a-solo"}},
		map[string]map[string]any{
			"obs-a-solo": {"gpu_uuids": []string{uuidA}},
		},
		[]inference.LLM{
			{Name: "llm-a-main", BillingGroup: billingLabel},
			{Name: "llm-a-ungrouped", BillingGroup: ""}, // ungrouped: never a match source
		})
	b := newFPDaemon(t, "b", "fp-rt2-admin-b",
		[]compute.Node{{Name: "solo-b", Address: addrB, ObserverPeer: "obs-b-solo"}},
		map[string]map[string]any{
			"obs-b-solo": {"gpu_uuids": []string{uuidB}},
		},
		[]inference.LLM{{Name: "llm-b-main", BillingGroup: billingLabel}})

	a.addPeer(t, "b", b.url(), pairTok, true)
	b.addPeer(t, "a", a.url(), pairTok, true)

	a.syncer().SyncOnce(context.Background())
	b.syncer().SyncOnce(context.Background())

	check := func(t *testing.T, d *fpDaemon, peer, local, peerName string) {
		t.Helper()
		pending := d.matches.List(fingerprint.StatePending)
		if len(pending) != 1 {
			t.Fatalf("daemon %s expected exactly one pending billing match, got %+v", d.name, pending)
		}
		m := pending[0]
		if m.Peer != peer || m.Kind != fingerprint.KindBilling || m.LocalName != local || m.PeerName != peerName {
			t.Fatalf("daemon %s unexpected billing match: %+v", d.name, m)
		}
		for _, p := range pending {
			if p.Kind == fingerprint.KindNode {
				t.Fatalf("daemon %s got a node-kind match, none expected: %+v", d.name, p)
			}
		}
	}
	check(t, a, "b", "llm-a-main", "llm-b-main")
	check(t, b, "a", "llm-b-main", "llm-a-main")

	a.wire.assertClean(t, "a", uuidA, uuidB, addrA, addrB, billingLabel)
	b.wire.assertClean(t, "b", uuidA, uuidB, addrA, addrB, billingLabel)
}

// B's operator has NOT opted A into the exchange: A's pull of B's
// fingerprints is refused with 403, no match is recorded on either side,
// and the admit gate passes because no confirmed match can ever exist.
func TestFingerprintRoundTripOptOutDefault(t *testing.T) {
	const (
		sharedUUID = "GPU-cc00dd11-eeee-ffff-0000-111122223333"
		addrA      = "http://172.18.0.21:8000"
		addrB      = "http://172.18.0.22:8000"
	)
	pairTok := "fp-rt3-pair-ab-S3cr3t"

	a := newFPDaemon(t, "a", "fp-rt3-admin-a",
		[]compute.Node{{Name: "shared-a", Address: addrA, ObserverPeer: "obs-a"}},
		map[string]map[string]any{"obs-a": {"gpu_uuids": []string{sharedUUID}}}, nil)
	b := newFPDaemon(t, "b", "fp-rt3-admin-b",
		[]compute.Node{{Name: "shared-b", Address: addrB, ObserverPeer: "obs-b"}},
		map[string]map[string]any{"obs-b": {"gpu_uuids": []string{sharedUUID}}}, nil)

	// A's operator opted B in (so A's syncer actually rings); B's operator
	// has NOT opted A in — the default.
	a.addPeer(t, "b", b.url(), pairTok, true)
	b.addPeer(t, "a", a.url(), pairTok, false)

	// A's pull is refused: 403 on B, with a clear error body.
	a.syncer().SyncOnce(context.Background())
	refs := b.wire.find(http.MethodGet, "/api/federation/fingerprints")
	if len(refs) != 1 {
		t.Fatalf("expected exactly one refused fingerprints request on b, got %d: %+v", len(refs), refs)
	}
	if refs[0].status != http.StatusForbidden {
		t.Fatalf("expected 403 from unopted-in b, got %d: %q", refs[0].status, refs[0].respBody)
	}
	if !strings.Contains(refs[0].respBody, "not enabled") {
		t.Fatalf("expected refusal body to explain the opt-out, got %q", refs[0].respBody)
	}

	// No match anywhere on either side.
	if ml := a.matches.List(""); len(ml) != 0 {
		t.Fatalf("daemon a must have no matches: %+v", ml)
	}
	if ml := b.matches.List(""); len(ml) != 0 {
		t.Fatalf("daemon b must have no matches: %+v", ml)
	}

	// Gate admits — it has no confirmed match to act on, so it does not
	// even phone home to B's capacity endpoint.
	g := a.gate(func(pool string) (int, int) { return 3, 4 })
	if reason := g.Reason("shared-a"); reason != "" {
		t.Fatalf("gate must admit with no confirmed match, got reason %q", reason)
	}
	if caps := b.wire.find(http.MethodGet, "/api/capacity"); len(caps) != 0 {
		t.Fatalf("gate must not query b at all: %+v", caps)
	}

	// Even the refused exchange must not leak raw identity.
	a.wire.assertClean(t, "a", sharedUUID, addrA, addrB)
	b.wire.assertClean(t, "b", sharedUUID, addrA, addrB)
}

// B goes down AFTER the pair is confirmed: the gate must still admit
// (fail open), and the failure must be logged once per episode, not on
// every subsequent Reason() call.
func TestFingerprintRoundTripPeerDownFailsOpen(t *testing.T) {
	const (
		sharedUUID = "GPU-ee22ff33-9999-8888-7777-666655554444"
		addrA      = "http://10.9.9.21:8000"
		addrB      = "http://10.9.9.22:8000"
	)
	pairTok := "fp-rt4-pair-ab-S3cr3t"

	a := newFPDaemon(t, "a", "fp-rt4-admin-a",
		[]compute.Node{{Name: "shared-a", Address: addrA, ObserverPeer: "obs-a"}},
		map[string]map[string]any{"obs-a": {"gpu_uuids": []string{sharedUUID}}}, nil)
	b := newFPDaemon(t, "b", "fp-rt4-admin-b",
		[]compute.Node{{Name: "shared-b", Address: addrB, ObserverPeer: "obs-b"}},
		map[string]map[string]any{"obs-b": {"gpu_uuids": []string{sharedUUID}}}, nil)

	a.addPeer(t, "b", b.url(), pairTok, true)
	b.addPeer(t, "a", a.url(), pairTok, true)

	// Reconcile and confirm through the real admin API.
	a.syncer().SyncOnce(context.Background())
	ml := a.matches.List(fingerprint.StatePending)
	if len(ml) != 1 {
		t.Fatalf("expected one pending match, got %+v", ml)
	}
	status, body := a.adminCall(t, http.MethodPost, "/api/federation/fingerprint-matches/"+ml[0].ID+"/confirm")
	if status != http.StatusOK {
		t.Fatalf("confirm: status %d body %s", status, body)
	}

	// B goes down.
	b.closePeer()

	stdout := &strings.Builder{}
	old := log.Writer()
	log.SetOutput(stdout)
	defer log.SetOutput(old)

	// Gate holds a confirmed match on the now-unreachable peer, and local
	// held (3) + limit (4) would be over the limit if even one peer slot
	// counted — a blocking misimplementation would produce a reason here.
	g := a.gate(func(pool string) (int, int) { return 3, 4 })
	if reason := g.Reason("shared-a"); reason != "" {
		t.Fatalf("gate must FAIL OPEN while the peer is down, got %q", reason)
	}
	const failMarker = "shared-pool check failed"
	first := strings.Count(stdout.String(), failMarker)
	// A second call in the same failure episode: still open…
	if reason := g.Reason("shared-a"); reason != "" {
		t.Fatalf("gate must stay open, got %q", reason)
	}
	second := strings.Count(stdout.String(), failMarker)
	if second != first {
		t.Fatalf("failure must be logged ONCE per episode: first=%d second=%d log=%q", first, second, stdout.String())
	}
	if second != 1 {
		t.Fatalf("expected exactly one 'shared-pool check failed' log line, got %d: %q", second, stdout.String())
	}

	a.wire.assertClean(t, "a", sharedUUID, addrA, addrB)
	b.wire.assertClean(t, "b", sharedUUID, addrA, addrB)
}

// A third daemon C holds the same physical GPU as A and B, but the pair
// key A's side has for C differs from the one C's side has for A (and from
// A<->B's): the digests derived under their respective pair keys are
// different, so C's fingerprints can never match A's, and the exchange
// with C is refused outright.
func TestFingerprintRoundTripWrongPairKey(t *testing.T) {
	const (
		sharedUUID = "GPU-33ee44ff-1111-2222-3333-444455550000"
		addrA      = "http://198.51.100.11:8000"
		addrC      = "http://198.51.100.12:8000"
	)
	pairAB := "fp-rt5-pair-ab-S3cr3t"
	// The A<->C pair key is SPLIT: the two sides hold different tokens,
	// so every digest derived from "the pair key" differs between them.
	pairACFromA := "fp-rt5-pair-ac-a-side"
	pairACFromC := "fp-rt5-pair-ac-c-side"

	a := newFPDaemon(t, "a", "fp-rt5-admin-a",
		[]compute.Node{{Name: "shared-a", Address: addrA, ObserverPeer: "obs-a"}},
		map[string]map[string]any{"obs-a": {"gpu_uuids": []string{sharedUUID}}}, nil)
	b := newFPDaemon(t, "b", "fp-rt5-admin-b",
		[]compute.Node{{Name: "shared-b", Address: "http://198.51.100.21:8000", ObserverPeer: "obs-b"}},
		map[string]map[string]any{"obs-b": {"gpu_uuids": []string{sharedUUID}}}, nil)
	c := newFPDaemon(t, "c", "fp-rt5-admin-c",
		[]compute.Node{{Name: "shared-c", Address: addrC, ObserverPeer: "obs-c"}},
		map[string]map[string]any{"obs-c": {"gpu_uuids": []string{sharedUUID}}}, nil)

	// A<->B: correct shared key — this pair reconciles.
	a.addPeer(t, "b", b.url(), pairAB, true)
	b.addPeer(t, "a", a.url(), pairAB, true)
	// A<->C: WRONG pair key (mismatched tokens; C's side is "c-side", A's
	// side "a-side") — this pair must never match.
	a.addPeer(t, "c", c.url(), pairACFromA, true)
	c.addPeer(t, "a", a.url(), pairACFromC, true)

	forbidden := []string{sharedUUID, addrA, addrC, "http://198.51.100.21:8000"}

	a.syncer().SyncOnce(context.Background())
	c.syncer().SyncOnce(context.Background())

	// A reconciled with B (1 pending, peer b) but NEVER with C.
	pending := a.matches.List(fingerprint.StatePending)
	if len(pending) != 1 {
		t.Fatalf("daemon a expected exactly one pending match (with b), got %+v", pending)
	}
	for _, m := range a.matches.List("") {
		if m.Peer == "c" {
			t.Fatalf("daemon a must never match c on a wrong pair key: %+v", m)
		}
	}
	if ml := c.matches.List(""); len(ml) != 0 {
		t.Fatalf("daemon c must have no matches at all (exchange refused): %+v", ml)
	}

	// The root cause, shown directly: same GPU, same kind, different pair
	// key -> different digests. Nothing derived under one pair key can
	// ever equal anything derived under another.
	val := "gpu:" + sharedUUID
	keyAB, err := fingerprint.PairKey(pairAB)
	if err != nil {
		t.Fatalf("PairKey(ab): %v", err)
	}
	keyCA, err := fingerprint.PairKey(pairACFromC)
	if err != nil {
		t.Fatalf("PairKey(ac): %v", err)
	}
	hAB, err := fingerprint.Hash(keyAB, fingerprint.KindNode, val)
	if err != nil {
		t.Fatalf("Hash(ab): %v", err)
	}
	hCA, err := fingerprint.Hash(keyCA, fingerprint.KindNode, val)
	if err != nil {
		t.Fatalf("Hash(ac): %v", err)
	}
	if hAB == hCA {
		t.Fatalf("fingerprints under different pair keys must differ, both = %s", hAB)
	}

	// A<->B used the correct shared pair key: A's pull of B reached B and
	// was served (200 recorded on B's side), reconciling exactly one match.
	if refs := b.wire.find(http.MethodGet, "/api/federation/fingerprints"); len(refs) != 1 || refs[0].status != http.StatusOK {
		t.Fatalf("daemon b should have served exactly one exchange (to a), got %+v", refs)
	}
	// A<->C used SPLIT tokens: each side's pull of the other was refused
	// with 401 (unknown token on the responding side) — a.wire holds C's
	// pull of A, c.wire holds A's pull of C.
	inA := a.wire.find(http.MethodGet, "/api/federation/fingerprints")
	for _, e := range inA {
		if e.status != http.StatusUnauthorized {
			t.Fatalf("wrong pair key: daemon a served C a %d: %+v", e.status, e)
		}
	}
	inC := c.wire.find(http.MethodGet, "/api/federation/fingerprints")
	for _, e := range inC {
		if e.status != http.StatusUnauthorized {
			t.Fatalf("wrong pair key: daemon c served A a %d: %+v", e.status, e)
		}
	}

	a.wire.assertClean(t, "a", forbidden...)
	b.wire.assertClean(t, "b", forbidden...)
	c.wire.assertClean(t, "c", forbidden...)
}
