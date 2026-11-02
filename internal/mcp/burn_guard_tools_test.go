package mcp

import (
	"context"
	"net/http"
	"regexp"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestBurnGuardToolsProxyRest(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		call   func(*Server) error
		method string
		path   string
	}{
		{"status", func(s *Server) error { _, e := s.handleBurnGuardStatus(ctx, callReq(nil)); return e }, http.MethodGet, "/api/usage/burn-guard"},
		{"run", func(s *Server) error { _, e := s.handleBurnGuardRun(ctx, callReq(nil)); return e }, http.MethodPost, "/api/usage/burn-guard/run"},
		{"config_get", func(s *Server) error { _, e := s.handleBurnGuardConfigGet(ctx, callReq(nil)); return e }, http.MethodGet, "/api/usage/burn-guard/config"},
	} {
		s, rec := usageCalcServer(t, http.StatusOK, `{"state":"idle"}`)
		if err := tc.call(s); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if rec.method != tc.method || rec.path != tc.path {
			t.Errorf("%s proxied %s %s, want %s %s", tc.name, rec.method, rec.path, tc.method, tc.path)
		}
	}
}

func TestBurnGuardConfigSetForwardsOnlyGivenKeys(t *testing.T) {
	s, rec := usageCalcServer(t, http.StatusOK, `{"ok":true}`)
	res, err := s.handleBurnGuardConfigSet(context.Background(), callReq(map[string]any{
		"margin_points": 4.0, "ladder": []any{"lower_inflight"}, "unrelated": "x",
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPut || rec.path != "/api/usage/burn-guard/config" {
		t.Fatalf("proxied %s %s", rec.method, rec.path)
	}
	if len(rec.body) != 2 || rec.body["margin_points"] != 4.0 || rec.body["ladder"] == nil {
		t.Errorf("body = %v", rec.body)
	}
}

func TestBurnGuardConfigSetNeedsASetting(t *testing.T) {
	s, rec := usageCalcServer(t, http.StatusOK, `{}`)
	res, err := s.handleBurnGuardConfigSet(context.Background(), callReq(nil))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want a tool error, got err=%v res=%+v", err, res)
	}
	if rec.method != "" {
		t.Errorf("an empty call reached the server")
	}
}

func TestBurnGuardToolsRegisteredAndCapped(t *testing.T) {
	s := &Server{}
	tools := map[string]string{
		s.toolBurnGuardStatus().Name:    federation.CapUsageRead,
		s.toolBurnGuardRun().Name:       federation.CapUsageWrite,
		s.toolBurnGuardConfigGet().Name: federation.CapUsageRead,
		s.toolBurnGuardConfigSet().Name: federation.CapUsageWrite,
	}
	trackerID := regexp.MustCompile(`\b(BL|B|G|GH|SEC)-?[0-9]+`)
	for name, cap := range tools {
		if got := federation.MCPToolCap[name]; got != cap {
			t.Errorf("%s: capability = %q, want %q", name, got, cap)
		}
	}
	for _, d := range []string{s.toolBurnGuardStatus().Description, s.toolBurnGuardRun().Description, s.toolBurnGuardConfigGet().Description, s.toolBurnGuardConfigSet().Description} {
		if trackerID.MatchString(d) {
			t.Errorf("description holds a tracker identifier: %s", d)
		}
	}
}
