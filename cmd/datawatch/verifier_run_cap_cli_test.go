package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// verifierRunCapCLIServer starts a fake daemon that records every PUT /api/config
// body. serverURL is pointed at it for the duration of the test.
func verifierRunCapCLIServer(t *testing.T) *[]string {
	t.Helper()
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return &bodies
}

func TestVerifierRunCapCLISetZero(t *testing.T) {
	bodies := verifierRunCapCLIServer(t)
	if err := runConfigSet(&config.Config{}, verifierRunCapConfigKey, "0"); err != nil {
		t.Fatalf("set 0: unexpected error: %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(*bodies))
	}
	want := `{"autonomous.verifier_max_run_seconds": 0}`
	if (*bodies)[0] != want {
		t.Errorf("body = %s, want %s", (*bodies)[0], want)
	}
}

func TestVerifierRunCapCLISetDefault(t *testing.T) {
	bodies := verifierRunCapCLIServer(t)
	if err := runConfigSet(&config.Config{}, verifierRunCapConfigKey, "2700"); err != nil {
		t.Fatalf("set 2700: unexpected error: %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(*bodies))
	}
	want := `{"autonomous.verifier_max_run_seconds": 2700}`
	if (*bodies)[0] != want {
		t.Errorf("body = %s, want %s", (*bodies)[0], want)
	}
}

func TestVerifierRunCapCLIRefusesBadValues(t *testing.T) {
	for _, value := range []string{"-1", "2.5", "abc", ""} {
		t.Run(value, func(t *testing.T) {
			bodies := verifierRunCapCLIServer(t)
			err := runConfigSet(&config.Config{}, verifierRunCapConfigKey, value)
			if err == nil {
				t.Fatalf("set %q: want error, got nil", value)
			}
			if !strings.Contains(err.Error(), verifierRunCapConfigKey) {
				t.Errorf("error %q does not name key %s", err, verifierRunCapConfigKey)
			}
			if len(*bodies) != 0 {
				t.Errorf("refused value still sent %d request(s)", len(*bodies))
			}
		})
	}
}

func TestVerifierRunCapCLIParseNegativeNamesKey(t *testing.T) {
	_, err := parseVerifierRunCapValue("-1")
	if err == nil || !strings.Contains(err.Error(), verifierRunCapConfigKey) {
		t.Fatalf("parse -1: err = %v, want error naming %s", err, verifierRunCapConfigKey)
	}
}
