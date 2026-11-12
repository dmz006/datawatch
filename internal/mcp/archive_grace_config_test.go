package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// config_set lists autonomous.archive_grace_minutes and forwards it to PUT
// /api/config as a number.
func TestConfigSetAcceptsArchiveGrace(t *testing.T) {
	const key = "autonomous.archive_grace_minutes"
	if desc := (&Server{}).toolConfigSet().Description; !strings.Contains(desc, key) {
		t.Errorf("config_set description does not mention %s", key)
	}
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
	res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{"key": key, "value": "0"}))
	if err != nil {
		t.Fatal(err)
	}
	if text := automationText(t, res); !strings.Contains(text, "Set "+key+" = 0") {
		t.Errorf("config_set text = %q", text)
	}
	if want := `{"` + key + `": 0}`; gotBody != want {
		t.Errorf("daemon got %q, want %q", gotBody, want)
	}
}
