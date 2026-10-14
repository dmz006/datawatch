package server

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// fixedGzipSettings returns a settings func for newAPIGzipHandler.
func fixedGzipSettings(enabled bool, min int) func() (bool, int) {
	return func() (bool, int) { return enabled, min }
}

func jsonHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "999") // must be dropped when compressing
		_, _ = io.WriteString(w, body)
	}
}

func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return string(out)
}

func serveGzip(h http.Handler, method, path, acceptEncoding string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func largeJSON(n int) string {
	return `{"data":"` + strings.Repeat("abcdefgh", n/8) + `"}`
}

func TestAPIGzip_SmallBodyNotCompressed(t *testing.T) {
	body := `{"ok":true}`
	h := newAPIGzipHandler(fixedGzipSettings(true, 1024), jsonHandler(body))
	rr := serveGzip(h, http.MethodGet, "/api/info", "gzip")
	if ce := rr.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("small body compressed: Content-Encoding=%q", ce)
	}
	if rr.Body.String() != body {
		t.Errorf("body=%q want %q", rr.Body.String(), body)
	}
	if rr.Header().Get("Content-Length") != "999" {
		t.Errorf("headers must be untouched below threshold, Content-Length=%q", rr.Header().Get("Content-Length"))
	}
	if rr.Header().Get("Vary") != "Accept-Encoding" {
		t.Errorf("Vary=%q", rr.Header().Get("Vary"))
	}
}

func TestAPIGzip_LargeJSONCompressedRoundTrips(t *testing.T) {
	body := largeJSON(64 * 1024)
	h := newAPIGzipHandler(fixedGzipSettings(true, 1024), jsonHandler(body))
	rr := serveGzip(h, http.MethodGet, "/api/sessions", "gzip, deflate, br")
	if rr.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding=%q", rr.Header().Get("Content-Encoding"))
	}
	if rr.Header().Get("Content-Length") != "" {
		t.Errorf("Content-Length must be removed when compressing")
	}
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type=%q", rr.Header().Get("Content-Type"))
	}
	if rr.Body.Len() >= len(body) {
		t.Errorf("compressed %d >= raw %d", rr.Body.Len(), len(body))
	}
	if got := gunzip(t, rr.Body.Bytes()); got != body {
		t.Errorf("round-trip mismatch (len %d vs %d)", len(got), len(body))
	}
}

func TestAPIGzip_ManySmallWritesCrossThreshold(t *testing.T) {
	var want strings.Builder
	h := newAPIGzipHandler(fixedGzipSettings(true, 100), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusCreated)
		for i := 0; i < 50; i++ {
			_, _ = io.WriteString(w, `{"i":1},`)
		}
	}))
	for i := 0; i < 50; i++ {
		want.WriteString(`{"i":1},`)
	}
	rr := serveGzip(h, http.MethodPost, "/api/x", "gzip")
	if rr.Code != http.StatusCreated {
		t.Errorf("status=%d", rr.Code)
	}
	if rr.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("+json type not compressed")
	}
	if got := gunzip(t, rr.Body.Bytes()); got != want.String() {
		t.Errorf("got %q", got)
	}
}

func TestAPIGzip_NoAcceptEncodingIdenticalBody(t *testing.T) {
	body := largeJSON(8192)
	h := newAPIGzipHandler(fixedGzipSettings(true, 1024), jsonHandler(body))
	rr := serveGzip(h, http.MethodGet, "/api/sessions", "")
	if rr.Header().Get("Content-Encoding") != "" {
		t.Fatalf("compressed without Accept-Encoding")
	}
	if rr.Body.String() != body {
		t.Errorf("body changed")
	}
	if rr.Header().Get("Vary") != "Accept-Encoding" {
		t.Errorf("Vary must be set even without Accept-Encoding: %q", rr.Header().Get("Vary"))
	}
}

func TestAPIGzip_NonJSONTypeNotCompressed(t *testing.T) {
	body := strings.Repeat("\x89PNG", 2048)
	h := newAPIGzipHandler(fixedGzipSettings(true, 16), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, body)
	}))
	rr := serveGzip(h, http.MethodGet, "/api/img", "gzip")
	if rr.Header().Get("Content-Encoding") != "" || rr.Body.String() != body {
		t.Errorf("binary type must pass through untouched")
	}
}

func TestAPIGzip_SniffedContentTypeCompressed(t *testing.T) {
	// Handler sets no Content-Type: net/http would sniff text/plain; the
	// middleware sniffs the same before compressing.
	body := strings.Repeat("plain text line\n", 400)
	h := newAPIGzipHandler(fixedGzipSettings(true, 64), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	rr := serveGzip(h, http.MethodGet, "/api/logs", "gzip")
	if rr.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("sniffed text/plain not compressed")
	}
	if !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("Content-Type=%q", rr.Header().Get("Content-Type"))
	}
	if gunzip(t, rr.Body.Bytes()) != body {
		t.Errorf("round-trip mismatch")
	}
}

func TestAPIGzip_PreEncodedUntouched(t *testing.T) {
	var pre bytes.Buffer
	zw := gzip.NewWriter(&pre)
	_, _ = zw.Write([]byte(largeJSON(8192)))
	_ = zw.Close()
	h := newAPIGzipHandler(fixedGzipSettings(true, 16), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(pre.Bytes())
	}))
	rr := serveGzip(h, http.MethodGet, "/api/x", "gzip")
	if !bytes.Equal(rr.Body.Bytes(), pre.Bytes()) {
		t.Fatalf("pre-encoded body was re-encoded")
	}
	if rr.Header().Values("Content-Encoding")[0] != "gzip" || len(rr.Header().Values("Content-Encoding")) != 1 {
		t.Errorf("Content-Encoding=%v", rr.Header().Values("Content-Encoding"))
	}
}

func TestAPIGzip_ContentDispositionAndDownloadPathUntouched(t *testing.T) {
	body := largeJSON(8192)
	attach := newAPIGzipHandler(fixedGzipSettings(true, 16), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="x.json"`)
		_, _ = io.WriteString(w, body)
	}))
	if rr := serveGzip(attach, http.MethodGet, "/api/export", "gzip"); rr.Header().Get("Content-Encoding") != "" || rr.Body.String() != body {
		t.Errorf("attachment compressed")
	}
	dl := newAPIGzipHandler(fixedGzipSettings(true, 16), jsonHandler(body))
	if rr := serveGzip(dl, http.MethodGet, "/api/files/download?path=/x", "gzip"); rr.Header().Get("Content-Encoding") != "" || rr.Body.String() != body {
		t.Errorf("/api/files/download compressed")
	}
}

func TestAPIGzip_HEADAndStatusPassThrough(t *testing.T) {
	h := newAPIGzipHandler(fixedGzipSettings(true, 0), jsonHandler(largeJSON(4096)))
	rr := serveGzip(h, http.MethodHead, "/api/sessions", "gzip")
	if rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("HEAD compressed")
	}
	for _, code := range []int{http.StatusNoContent, http.StatusNotModified} {
		code := code
		h := newAPIGzipHandler(fixedGzipSettings(true, 0), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
		}))
		rr := serveGzip(h, http.MethodGet, "/api/x", "gzip")
		if rr.Code != code || rr.Header().Get("Content-Encoding") != "" || rr.Body.Len() != 0 {
			t.Errorf("%d: code=%d ce=%q len=%d", code, rr.Code, rr.Header().Get("Content-Encoding"), rr.Body.Len())
		}
	}
}

func TestAPIGzip_EmptyBodyWithMinZero(t *testing.T) {
	h := newAPIGzipHandler(fixedGzipSettings(true, 0), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
	}))
	rr := serveGzip(h, http.MethodPost, "/api/x", "gzip")
	if rr.Code != http.StatusAccepted || rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("empty body: code=%d ce=%q", rr.Code, rr.Header().Get("Content-Encoding"))
	}
	// min 0: a single byte already exceeds the threshold.
	h = newAPIGzipHandler(fixedGzipSettings(true, 0), jsonHandler("1"))
	rr = serveGzip(h, http.MethodGet, "/api/x", "gzip")
	if rr.Header().Get("Content-Encoding") != "gzip" || gunzip(t, rr.Body.Bytes()) != "1" {
		t.Errorf("min 0 should compress any non-empty body")
	}
}

func TestAPIGzip_ConfigToggleOff(t *testing.T) {
	body := largeJSON(8192)
	h := newAPIGzipHandler(fixedGzipSettings(false, 0), jsonHandler(body))
	rr := serveGzip(h, http.MethodGet, "/api/sessions", "gzip")
	if rr.Header().Get("Content-Encoding") != "" || rr.Body.String() != body {
		t.Errorf("disabled middleware altered the response")
	}
	if rr.Header().Get("Vary") != "" {
		t.Errorf("Vary set while disabled: %q", rr.Header().Get("Vary"))
	}
}

func TestAPIGzip_LiveConfigToggleAndThreshold(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Server.APIGzip = true
	s.cfg.Server.APIGzipMinBytes = 1024
	body := largeJSON(2048) // ~2 KB
	h := s.apiGzipMiddleware(jsonHandler(body))

	if rr := serveGzip(h, http.MethodGet, "/api/x", "gzip"); rr.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip at 1024 threshold")
	}
	// Raise threshold above the body via the same path PUT /api/config uses.
	applyConfigPatch(s.cfg, map[string]interface{}{"server.api_gzip_min_bytes": float64(1 << 20)})
	if rr := serveGzip(h, http.MethodGet, "/api/x", "gzip"); rr.Header().Get("Content-Encoding") != "" || rr.Body.String() != body {
		t.Errorf("threshold change not applied at request time")
	}
	applyConfigPatch(s.cfg, map[string]interface{}{"server.api_gzip_min_bytes": float64(0)})
	if rr := serveGzip(h, http.MethodGet, "/api/x", "gzip"); rr.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("lowered threshold not applied")
	}
	applyConfigPatch(s.cfg, map[string]interface{}{"server.api_gzip": false})
	if rr := serveGzip(h, http.MethodGet, "/api/x", "gzip"); rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("toggle off not applied at request time")
	}
}

func TestAPIGzip_SSENotCompressedAndFlushes(t *testing.T) {
	gotFirst := make(chan struct{})
	release := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Error("wrapped writer is not an http.Flusher")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: one\n\n")
		f.Flush()
		<-release
		_, _ = io.WriteString(w, "data: "+strings.Repeat("x", 4096)+"\n\n")
		f.Flush()
	})
	srv := httptest.NewServer(newAPIGzipHandler(fixedGzipSettings(true, 0), inner))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/events", nil)
	req.Header.Set("Accept-Encoding", "gzip") // explicit: transport won't decode
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("SSE compressed")
	}
	br := bufio.NewReader(resp.Body)
	go func() {
		line, _ := br.ReadString('\n')
		if line == "data: one\n" {
			close(gotFirst)
		}
	}()
	select {
	case <-gotFirst:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("first SSE event not flushed to client")
	}
	close(release)
}

func TestAPIGzip_FlushBeforeThresholdCommitsUncompressed(t *testing.T) {
	part1 := `{"partial":`
	part2 := `"` + strings.Repeat("y", 4096) + `"}`
	h := newAPIGzipHandler(fixedGzipSettings(true, 1024), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, part1)
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, part2)
	}))
	rr := serveGzip(h, http.MethodGet, "/api/stream", "gzip")
	if rr.Header().Get("Content-Encoding") != "" {
		t.Fatalf("flushed response later compressed")
	}
	if !rr.Flushed {
		t.Errorf("Flush did not reach the underlying writer")
	}
	if rr.Body.String() != part1+part2 {
		t.Errorf("body mismatch")
	}
}

func TestAPIGzip_FlushAfterCompressionStarts(t *testing.T) {
	body := largeJSON(4096)
	h := newAPIGzipHandler(fixedGzipSettings(true, 16), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
		w.(http.Flusher).Flush()
	}))
	rr := serveGzip(h, http.MethodGet, "/api/x", "gzip")
	if rr.Header().Get("Content-Encoding") != "gzip" || gunzip(t, rr.Body.Bytes()) != body {
		t.Errorf("flush after compression broke the stream")
	}
}

func TestAPIGzip_HijackPassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("wrapped writer is not an http.Hijacker")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 8\r\nConnection: close\r\n\r\nhijacked")
		_ = buf.Flush()
	})
	srv := httptest.NewServer(newAPIGzipHandler(fixedGzipSettings(true, 0), inner))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/raw", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "hijacked" || resp.Header.Get("Content-Encoding") != "" {
		t.Errorf("hijacked response altered: %q ce=%q", b, resp.Header.Get("Content-Encoding"))
	}
}

func TestAPIGzip_UpgradeAndProxyRequestsBypass(t *testing.T) {
	var sawWrapped atomic.Bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(*apiGzipWriter); ok {
			sawWrapped.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, largeJSON(4096))
	})
	h := newAPIGzipHandler(fixedGzipSettings(true, 0), inner)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/x/ws", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if sawWrapped.Load() || rr.Header().Get("Content-Encoding") != "" || rr.Header().Get("Vary") != "" {
		t.Errorf("upgrade request was wrapped")
	}
	rr = serveGzip(h, http.MethodGet, "/api/proxy/peer-a/api/sessions", "gzip")
	if sawWrapped.Load() || rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("proxy path was wrapped")
	}
}

func TestAddVaryAcceptEncoding(t *testing.T) {
	h := http.Header{}
	h.Set("Vary", "Origin, accept-encoding")
	addVaryAcceptEncoding(h)
	if len(h.Values("Vary")) != 1 {
		t.Errorf("duplicated Vary: %v", h.Values("Vary"))
	}
	h = http.Header{"Vary": {"Origin"}}
	addVaryAcceptEncoding(h)
	if got := h.Values("Vary"); len(got) != 2 || got[1] != "Accept-Encoding" {
		t.Errorf("Vary=%v", got)
	}
}

func TestAPIGzipCompressibleType(t *testing.T) {
	cases := map[string]bool{
		"application/json":                true,
		"application/json; charset=utf-8": true,
		"Application/JSON":                true,
		"application/problem+json":        true,
		"text/plain; charset=utf-8":       true,
		"application/yaml":                true,
		"text/event-stream":               false,
		"application/octet-stream":        false,
		"text/html":                       false,
		"image/png":                       false,
		"application/x-ndjson":            false,
		"text/+json":                      false,
	}
	for ct, want := range cases {
		if got := apiGzipCompressibleType(ct); got != want {
			t.Errorf("%q: got %v want %v", ct, got, want)
		}
	}
}

// TestHandleProxy_PeerGzipResponseDecoded runs a peer daemon whose /api/*
// is wrapped by apiGzipMiddleware and checks the local /api/proxy/ path
// returns plain, decodable JSON with no stray Content-Encoding, both
// directly and when the local daemon's own API middleware wraps it.
func TestHandleProxy_PeerGzipResponseDecoded(t *testing.T) {
	peerBody := `[` + strings.TrimSuffix(strings.Repeat(`{"id":"abc","task":"`+strings.Repeat("t", 200)+`"},`, 50), ",") + `]`
	var peerCompressed atomic.Bool
	peerAPI := http.NewServeMux()
	peerAPI.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, peerBody)
	})
	peerGz := newAPIGzipHandler(fixedGzipSettings(true, 1024), peerAPI)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &headerSpy{ResponseWriter: w}
		peerGz.ServeHTTP(rec, r)
		if rec.Header().Get("Content-Encoding") == "gzip" {
			peerCompressed.Store(true)
		}
	}))
	defer peer.Close()

	s := newTestServer(t, nil, nil)
	s.cfg = &config.Config{Servers: []config.RemoteServerConfig{
		{Name: "peer-a", URL: peer.URL, Enabled: true},
	}}
	s.cfg.Server.APIGzip = true
	s.cfg.Server.APIGzipMinBytes = 1024

	check := func(name string, h http.Handler) {
		t.Helper()
		rr := serveGzip(h, http.MethodGet, "/api/proxy/peer-a/api/sessions", "gzip, deflate")
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%s", name, rr.Code, rr.Body.String())
		}
		if ce := rr.Header().Get("Content-Encoding"); ce != "" {
			t.Errorf("%s: proxied response carries Content-Encoding=%q", name, ce)
		}
		var v []map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
			t.Fatalf("%s: proxied body not plain JSON: %v", name, err)
		}
		if len(v) != 50 {
			t.Errorf("%s: got %d records", name, len(v))
		}
	}
	check("direct", http.HandlerFunc(s.handleProxy))
	check("wrapped", s.apiGzipMiddleware(http.HandlerFunc(s.handleProxy)))
	if !peerCompressed.Load() {
		t.Errorf("peer never compressed: the transport did not request gzip, test is not exercising the decode path")
	}
}

// TestFetchRemoteSessions_PeerGzip confirms the federation fan-out client
// decodes a gzip-compressing peer's /api/sessions transparently.
func TestFetchRemoteSessions_PeerGzip(t *testing.T) {
	peerBody := `[` + strings.TrimSuffix(strings.Repeat(`{"id":"s1","task":"`+strings.Repeat("t", 300)+`"},`, 20), ",") + `]`
	var sawGzipAccept atomic.Bool
	peer := httptest.NewServer(newAPIGzipHandler(fixedGzipSettings(true, 64), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if acceptsGzip(r) {
			sawGzipAccept.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, peerBody)
	})))
	defer peer.Close()
	sessions, err := fetchRemoteSessions(peer.URL, "tok")
	if err != nil {
		t.Fatalf("fetchRemoteSessions: %v", err)
	}
	if len(sessions) != 20 {
		t.Errorf("got %d sessions", len(sessions))
	}
	if !sawGzipAccept.Load() {
		t.Errorf("transport did not request gzip; decode path untested")
	}
}

func TestApplyConfigPatch_APIGzipKeys(t *testing.T) {
	cfg := config.DefaultConfig()
	if !cfg.Server.APIGzip || cfg.Server.APIGzipMinBytes != 1024 {
		t.Fatalf("defaults: api_gzip=%v min=%d", cfg.Server.APIGzip, cfg.Server.APIGzipMinBytes)
	}
	// CLI config set / MCP config_set / comm configure all funnel into
	// applyConfigPatch with JSON-decoded (float64 / bool / string) values.
	applyConfigPatch(cfg, map[string]interface{}{"server.api_gzip": false, "server.api_gzip_min_bytes": float64(4096)})
	if cfg.Server.APIGzip || cfg.Server.APIGzipMinBytes != 4096 {
		t.Errorf("patch: api_gzip=%v min=%d", cfg.Server.APIGzip, cfg.Server.APIGzipMinBytes)
	}
	// MCP config_set / comm configure may deliver the bool as a string.
	applyConfigPatch(cfg, map[string]interface{}{"server.api_gzip": "true", "server.api_gzip_min_bytes": 512})
	if !cfg.Server.APIGzip || cfg.Server.APIGzipMinBytes != 512 {
		t.Errorf("string bool / int: api_gzip=%v min=%d", cfg.Server.APIGzip, cfg.Server.APIGzipMinBytes)
	}
	applyConfigPatch(cfg, map[string]interface{}{"server.api_gzip_min_bytes": -5})
	if cfg.Server.APIGzipMinBytes != 0 {
		t.Errorf("negative not clamped: %d", cfg.Server.APIGzipMinBytes)
	}
}

func TestHandleGetConfig_APIGzipKeys(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Server.APIGzip = true
	s.cfg.Server.APIGzipMinBytes = 2048
	rr := httptest.NewRecorder()
	s.handleConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var v map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	srv, _ := v["server"].(map[string]any)
	if srv["api_gzip"] != true || srv["api_gzip_min_bytes"] != float64(2048) {
		t.Errorf("server map: api_gzip=%v api_gzip_min_bytes=%v", srv["api_gzip"], srv["api_gzip_min_bytes"])
	}
}

func TestHandleInfo_AdvertisesAPIGzip(t *testing.T) {
	s := bl90Server(t)
	caps := func() []string {
		rr := httptest.NewRecorder()
		s.handleInfo(rr, httptest.NewRequest(http.MethodGet, "/api/info", nil))
		var v struct {
			Capabilities []string `json:"capabilities"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v.Capabilities
	}
	has := func(list []string) bool {
		for _, c := range list {
			if c == "api.gzip" {
				return true
			}
		}
		return false
	}
	s.cfg.Server.APIGzip = true
	if c := caps(); !has(c) {
		t.Errorf("enabled: capabilities=%v missing api.gzip", c)
	}
	s.cfg.Server.APIGzip = false
	if c := caps(); has(c) {
		t.Errorf("disabled: capabilities=%v still lists api.gzip", c)
	}
}

func TestAPIGzipSettings_NilAndNegative(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Server.APIGzip = true
	s.cfg.Server.APIGzipMinBytes = -10
	if on, min := s.apiGzipSettings(); !on || min != 0 {
		t.Errorf("negative: (%v,%d)", on, min)
	}
	s.cfg = nil
	if on, min := s.apiGzipSettings(); !on || min != defaultAPIGzipMinBytes {
		t.Errorf("nil cfg: (%v,%d)", on, min)
	}
}

// headerSpy lets a test observe the headers a wrapped handler sent.
type headerSpy struct{ http.ResponseWriter }
