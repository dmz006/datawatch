package router_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/router"
)

func newModelRouter(t *testing.T) (*router.Router, *map[string]any, *int) {
	t.Helper()
	got := map[string]any{}
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, rq *http.Request) {
		hits++
		b, _ := io.ReadAll(rq.Body)
		got = map[string]any{}
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
	r := router.NewRouter("h", "g", nil, nil, 10, nil)
	r.SetWebPort(port)
	return r, &got, &hits
}

func TestNewModelPrefix_Parse(t *testing.T) {
	cmd := router.Parse("new: model=sonnet: do X")
	if cmd.Type != router.CmdNew || cmd.Model != "sonnet" || cmd.Text != "do X" || cmd.ModelErr != "" {
		t.Fatalf("cmd = %+v", cmd)
	}
}

func TestNewModelPrefix_ForwardedBody(t *testing.T) {
	r, got, hits := newModelRouter(t)
	r.HandleTestMessage("new: model=sonnet: do X")
	if *hits != 1 || (*got)["model"] != "sonnet" || (*got)["task"] != "do X" {
		t.Fatalf("hits=%d body=%v", *hits, *got)
	}
	r.HandleTestMessage("new: priority=3: do Y")
	if *hits != 2 {
		t.Fatalf("hits=%d", *hits)
	}
	if _, ok := (*got)["model"]; ok {
		t.Errorf("model present without prefix: %v", *got)
	}
}

func TestNewModelPrefix_EmptyRejected(t *testing.T) {
	r, _, hits := newModelRouter(t)
	cmd := router.Parse("new: model=: X")
	if cmd.ModelErr == "" || cmd.Model != "" {
		t.Fatalf("cmd = %+v", cmd)
	}
	out := r.HandleTestMessage("new: model=: X")
	if *hits != 0 || len(out) == 0 || !strings.Contains(out[0], "model=") {
		t.Fatalf("hits=%d out=%v", *hits, out)
	}
}
