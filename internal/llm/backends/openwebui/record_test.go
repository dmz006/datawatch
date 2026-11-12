package openwebui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSendRecordsRequest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}],\"usage\":{\"input_tokens\":9,\"output_tokens\":3}}\n\ndata: [DONE]\n\n")) //nolint:errcheck
	}))
	defer srv.Close()

	ch := make(chan RequestRecord, 1)
	SetRecorder(func(r RequestRecord) { ch <- r })
	defer SetRecorder(nil)
	t0 := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	calls := 0
	recordNow = func() time.Time { calls++; return t0.Add(time.Duration(calls-1) * 3 * time.Second) }
	defer func() { recordNow = time.Now }()

	b := &InteractiveBackend{model: "m1", baseURL: srv.URL}
	if err := b.sendAndStream(context.Background(), "cs-test", "hello", false); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-ch:
		if r.Model != "m1" || r.TokensIn != 9 || r.TokensOut != 3 || r.StatusCode != 200 || r.Err != nil || r.Duration != 3*time.Second || !r.Start.Equal(t0) {
			t.Errorf("record %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no record")
	}
}
