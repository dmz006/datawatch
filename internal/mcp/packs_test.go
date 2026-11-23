package mcp

import (
	"context"
	"net/http"
	"testing"
)

func TestPacksToolNames(t *testing.T) {
	s := &Server{}
	names := map[string]string{
		s.toolPacksList().Name: "packs_list", s.toolPacksSettingGet().Name: "packs_setting_get",
		s.toolPacksSettingSet().Name: "packs_setting_set", s.toolPacksInputsGet().Name: "packs_inputs_get",
		s.toolPacksInputsSet().Name: "packs_inputs_set", s.toolPacksInstall().Name: "packs_install",
		s.toolPacksAccept().Name: "packs_accept", s.toolPacksRemove().Name: "packs_remove",
	}
	for got, want := range names {
		if got != want {
			t.Errorf("tool name %q, want %q", got, want)
		}
	}
}

func TestPacksProxies(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		call   func(*Server) (any, error)
		method string
		path   string
		body   map[string]any
	}{
		{"list", func(s *Server) (any, error) { return s.handlePacksList(ctx, callReq(nil)) }, "GET", "/api/packs", nil},
		{"setting get", func(s *Server) (any, error) { return s.handlePacksSettingGet(ctx, callReq(nil)) }, "GET", "/api/packs/config", nil},
		{"setting set", func(s *Server) (any, error) {
			return s.handlePacksSettingSet(ctx, callReq(map[string]any{"enabled": true, "active": []any{"a"}}))
		}, "PUT", "/api/packs/config", map[string]any{"enabled": true, "active": []any{"a"}}},
		{"inputs get", func(s *Server) (any, error) {
			return s.handlePacksInputsGet(ctx, callReq(map[string]any{"level": "project", "key": "k", "pack": "p"}))
		}, "GET", "/api/packs/inputs", nil},
		{"inputs set", func(s *Server) (any, error) {
			return s.handlePacksInputsSet(ctx, callReq(map[string]any{"level": "global", "pack": "p", "inputs": map[string]any{"x": "1"}}))
		}, "PUT", "/api/packs/inputs", map[string]any{"inputs": map[string]any{"x": "1"}}},
		{"install", func(s *Server) (any, error) {
			return s.handlePacksInstall(ctx, callReq(map[string]any{"source_dir": "/src"}))
		}, "POST", "/api/packs/install", map[string]any{"source_dir": "/src"}},
		{"accept", func(s *Server) (any, error) {
			return s.handlePacksAccept(ctx, callReq(map[string]any{"name": "p", "version": "1.0.0"}))
		}, "POST", "/api/packs/accept", map[string]any{"name": "p", "version": "1.0.0"}},
		{"remove", func(s *Server) (any, error) {
			return s.handlePacksRemove(ctx, callReq(map[string]any{"name": "p", "version": "1.0.0"}))
		}, "DELETE", "/api/packs/p/1.0.0", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, rec := activateServer(t, http.StatusOK, `{"ok":true}`)
			if _, err := c.call(s); err != nil {
				t.Fatal(err)
			}
			if rec.method != c.method || rec.path != c.path {
				t.Fatalf("proxied %s %s, want %s %s", rec.method, rec.path, c.method, c.path)
			}
			for k, want := range c.body {
				if got := rec.body[k]; len(c.body) > 0 && got == nil {
					t.Errorf("body missing %q (want %v)", k, want)
				}
			}
		})
	}
}

func TestPacksValidation(t *testing.T) {
	ctx := context.Background()
	s, rec := activateServer(t, http.StatusOK, `{}`)
	bad := []func() (any, error){
		func() (any, error) { return s.handlePacksSettingSet(ctx, callReq(nil)) },
		func() (any, error) {
			return s.handlePacksInputsGet(ctx, callReq(map[string]any{"level": "bogus", "pack": "p"}))
		},
		func() (any, error) {
			return s.handlePacksInputsGet(ctx, callReq(map[string]any{"level": "project", "pack": "p"}))
		},
		func() (any, error) {
			return s.handlePacksInputsSet(ctx, callReq(map[string]any{"level": "global", "pack": "p", "inputs": map[string]any{"x": 1}}))
		},
		func() (any, error) { return s.handlePacksInstall(ctx, callReq(nil)) },
		func() (any, error) { return s.handlePacksAccept(ctx, callReq(map[string]any{"name": "p"})) },
		func() (any, error) {
			return s.handlePacksRemove(ctx, callReq(map[string]any{"name": "../x", "version": "1"}))
		},
	}
	for i, f := range bad {
		res, err := f()
		if err != nil || res == nil {
			t.Fatalf("case %d: err=%v", i, err)
		}
	}
	if rec.method != "" {
		t.Errorf("invalid input reached the backend: %s %s", rec.method, rec.path)
	}
}

func TestPacksBackendError(t *testing.T) {
	s, _ := activateServer(t, http.StatusConflict, "nope")
	if _, err := s.handlePacksAccept(context.Background(), callReq(map[string]any{"name": "p", "version": "1"})); err == nil {
		t.Fatal("expected error from non-2xx reply")
	}
}
