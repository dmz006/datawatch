package pi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeDoer struct {
	status int
	err    error
	req    *http.Request
	body   string
	block  bool
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.req = req
	b, _ := io.ReadAll(req.Body)
	f.body = string(b)
	if f.block {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader("x"))}, nil
}

func TestPreflightOK(t *testing.T) {
	f := &fakeDoer{status: 200}
	if err := Preflight(context.Background(), f, "http://node:1/v1/", "m1"); err != nil {
		t.Fatal(err)
	}
	if f.req.URL.String() != "http://node:1/v1/chat/completions" {
		t.Fatalf("url %s", f.req.URL)
	}
	if !strings.Contains(f.body, `"max_tokens":1`) || !strings.Contains(f.body, `"m1"`) {
		t.Fatalf("body %s", f.body)
	}
	if _, ok := f.req.Context().Deadline(); !ok {
		t.Fatal("no deadline")
	}
}

func TestPreflightFailures(t *testing.T) {
	cases := map[string]*fakeDoer{
		"error":  {err: errors.New("boom")},
		"non2xx": {status: 503},
	}
	for name, f := range cases {
		err := Preflight(context.Background(), f, "http://node:1/v1", "m1")
		if err == nil || !strings.Contains(err.Error(), "http://node:1/v1") || !strings.Contains(err.Error(), "m1") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestPreflightTimeout(t *testing.T) {
	f := &fakeDoer{block: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Preflight(ctx, f, "http://node:1/v1", "m1")
	if err == nil || !strings.Contains(err.Error(), "m1") {
		t.Fatalf("%v", err)
	}
}
