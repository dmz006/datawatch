package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSendAndStreamRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"content":"hi"}}` + "\n" + `{"done":true,"prompt_eval_count":11,"eval_count":4}` + "\n")) //nolint:errcheck
	}))
	defer srv.Close()
	ch := make(chan RequestRecord, 1)
	SetRecorder(func(r RequestRecord) { ch <- r })
	defer SetRecorder(nil)
	t0 := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	calls := 0
	recordNow = func() time.Time { calls++; return t0.Add(time.Duration(calls-1) * 2 * time.Second) }
	defer func() { recordNow = time.Now }()

	b := &Backend{model: "m1", binary: "ollama", host: srv.URL}
	if err := b.sendAndStream(context.Background(), "cs-test", "hello", false); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-ch:
		if r.Model != "m1" || r.TokensIn != 11 || r.TokensOut != 4 || r.StatusCode != 200 || r.Err != nil || r.Duration != 2*time.Second || !r.Start.Equal(t0) {
			t.Errorf("record %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no record")
	}
}
