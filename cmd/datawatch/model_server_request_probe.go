package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// modelServerRequestState is the tri-state answer to "is the model server
// active for the session's model right now?" (i.e. is there an in-flight
// request for it). The probe is the runner-side half of a liveness signal
// for the one-shot verifier wait loop: a confirmed active request counts
// as progress; an unknown result means "no evidence either way".
type modelServerRequestState int

// The tri-state values. Unknown is the fail-safe one: any probe failure
// (host not set, connection error, non-200, undecodable body) reports it,
// and the caller must never use it as evidence of inactivity nor as an
// active request — it must not by itself end a session.
const (
	modelServerRequestUnknown modelServerRequestState = iota
	modelServerRequestIdle
	modelServerRequestActive
)

// modelServerRequestProbeTimeout bounds a single probe round; it mirrors
// the daemon's existing Ollama status client (internal/stats) which also
// uses a 5-second http.Client.
const modelServerRequestProbeTimeout = 5 * time.Second

// probeModelServerRequest reports whether the Ollama host has the
// session's model loaded and serving — i.e. there is an active request
// for that model — by asking the same status endpoints the daemon's
// existing Ollama status client uses (GET /api/tags for "reachable and
// answering", then GET /api/ps for the loaded-model list).
//
// host is the Ollama base URL (cfg.Ollama.Host); model is the session
// model string, with or without the "ollama/" prefix ("" matches any
// loaded model). Every failure mode reports modelServerRequestUnknown,
// so the watchdog never ends — and never spares — a session on a failed
// probe.
func probeModelServerRequest(host, model string) modelServerRequestState {
	base := strings.TrimRight(strings.TrimSpace(host), "/")
	if base == "" {
		return modelServerRequestUnknown
	}
	client := &http.Client{Timeout: modelServerRequestProbeTimeout}

	// The server must answer 200 on /api/tags before any reading from
	// it is trusted; anything else is unknown, not idle.
	if _, ok := getJSONOK(client, base+"/api/tags"); !ok {
		return modelServerRequestUnknown
	}

	psBody, ok := getJSONOK(client, base+"/api/ps")
	if !ok {
		return modelServerRequestUnknown
	}
	var ps struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(psBody, &ps); err != nil {
		return modelServerRequestUnknown
	}
	if len(ps.Models) == 0 {
		return modelServerRequestIdle // server reachable, nothing loaded: known idle
	}
	want := strings.TrimPrefix(strings.TrimSpace(model), "ollama/")
	for _, m := range ps.Models {
		if want == "" || strings.EqualFold(m.Name, want) {
			return modelServerRequestActive
		}
	}
	return modelServerRequestIdle // something else is loaded; this model is not serving
}

// getJSONOK issues GET url and reports the 200 body. A non-200 status, a
// transport error, or a body read error all report false — the caller
// treats that as "cannot confirm", never as evidence either way.
func getJSONOK(client *http.Client, url string) ([]byte, bool) {
	resp, err := client.Get(url) //nolint:gosec // url is built from the operator's own config
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, false
	}
	return body, true
}
