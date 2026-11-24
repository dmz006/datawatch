package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPRDCancelCLITakesOutcomeAndDecision(t *testing.T) {
	var query url.Values
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{"status":"cancelled"}`))
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	defer func() { serverURL = prev }()
	c := newAutonomousPRDCancelCmd()
	c.SetArgs([]string{"abcd0001", "--outcome", "superseded", "--via", "ff00", "--decision", "drop", "--reason", "replaced"})
	c.SetOut(&strings.Builder{})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, want := query.Encode(), "decision=drop&delivered_via=ff00&outcome=superseded&reason=replaced"; got != want {
		t.Errorf("query = %s, want %s", got, want)
	}
}
