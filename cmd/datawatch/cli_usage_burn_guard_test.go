// AskOnChange in the CLI usage burn-guard config: true and false are accepted,
// any other value is rejected with a clear message before anything is sent, and
// an accepted value is forwarded as a boolean in the PUT body.

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBurnGuardAskOnChangeCLIAcceptsTrueAndFalse(t *testing.T) {
	body, err := parseBurnGuardSettings([]string{"ask_on_change=true"})
	if err != nil || body["ask_on_change"] != true {
		t.Fatalf("true: body = %v, err = %v", body, err)
	}
	body, err = parseBurnGuardSettings([]string{"ask_on_change=false"})
	if err != nil || body["ask_on_change"] != false {
		t.Fatalf("false: body = %v, err = %v", body, err)
	}
}

func TestBurnGuardAskOnChangeCLIRejectsNonBool(t *testing.T) {
	for _, bad := range []string{"maybe", "2", ""} {
		_, err := parseBurnGuardSettings([]string{"ask_on_change=" + bad})
		if err == nil {
			t.Fatalf("ask_on_change=%q accepted, want a clear error", bad)
		}
		if !strings.Contains(err.Error(), "ask_on_change") || !strings.Contains(err.Error(), "true or false") {
			t.Fatalf("error = %q, want it to name ask_on_change and true/false", err)
		}
	}
}

func TestBurnGuardAskOnChangeCLISendsPUTBody(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(strings.NewReader(""))
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"config":{"ask_on_change":true}}`))
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	defer func() { serverURL = prev }()

	root := newUsageBurnGuardCmd()
	if _, _, err := root.Find([]string{"config"}); err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"config", "ask_on_change=true"})
	if err := runRunE(t, root); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/usage/burn-guard/config" {
		t.Fatalf("proxied %s %s", gotMethod, gotPath)
	}
	if gotBody["ask_on_change"] != true {
		t.Fatalf("PUT body = %v, want ask_on_change=true", gotBody)
	}
}

// TestBurnGuardProjectionGapCLISendsPUTBodyAndRefusesInvalid proves the CLI path for
// max_projection_gap_points: 0 and 25 are sent as numbers in the PUT body, and an
// invalid value is refused before any request reaches the daemon.
func TestBurnGuardProjectionGapCLISendsPUTBodyAndRefusesInvalid(t *testing.T) {
	var hits int
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"config":{}}`))
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	defer func() { serverURL = prev }()

	for _, good := range []string{"0", "25"} {
		gotBody = nil
		root := newUsageBurnGuardCmd()
		root.SetArgs([]string{"config", "max_projection_gap_points=" + good})
		if err := runRunE(t, root); err != nil {
			t.Fatalf("%s: %v", good, err)
		}
		want := map[string]float64{"0": 0, "25": 25}[good]
		if gotBody["max_projection_gap_points"] != want {
			t.Fatalf("%s: PUT body = %v, want %v", good, gotBody, want)
		}
	}

	hits = 0
	root := newUsageBurnGuardCmd()
	root.SetArgs([]string{"config", "max_projection_gap_points=101"})
	if err := runRunE(t, root); err == nil || !strings.Contains(err.Error(), "max_projection_gap_points") {
		t.Fatalf("101: err = %v, want a refusal naming the key", err)
	}
	if hits != 0 {
		t.Fatalf("an invalid value reached the daemon (%d request(s))", hits)
	}
}
