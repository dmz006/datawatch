package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// identityStandIn is an httptest stand-in for the daemon that records the
// last request and answers with a fixed status and body.
type identityStandIn struct {
	method, path, body string
}

func newIdentityStandIn(t *testing.T, status int, answer string) (*Server, *identityStandIn) {
	t.Helper()
	rec := &identityStandIn{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.method, rec.path, rec.body = r.Method, r.URL.Path, string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return &Server{webPort: bl427Port(t, srv)}, rec
}

func identityReq(args map[string]any) mcpsdk.CallToolRequest {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

func identityResultText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestIdentityBackups_Tool(t *testing.T) {
	s, rec := newIdentityStandIn(t, 200, `{"backups":[],"keep":10}`)
	res, err := s.handleIdentityBackups(t.Context(), identityReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodGet || rec.path != "/api/identity/backups" {
		t.Errorf("request = %s %s", rec.method, rec.path)
	}
	if !strings.Contains(identityResultText(t, res), `"keep":10`) {
		t.Errorf("result = %q", identityResultText(t, res))
	}
}

func TestIdentityRestore_Tool(t *testing.T) {
	t.Run("named backup is sent", func(t *testing.T) {
		s, rec := newIdentityStandIn(t, 200, `{"role":"operator"}`)
		res, err := s.handleIdentityRestore(t.Context(), identityReq(map[string]any{"backup": "identity-20261029T101500.000Z.yaml"}))
		if err != nil || res.IsError {
			t.Fatalf("err=%v res=%+v", err, res)
		}
		if rec.method != http.MethodPost || rec.path != "/api/identity/restore" {
			t.Errorf("request = %s %s", rec.method, rec.path)
		}
		var sent map[string]string
		if err := json.Unmarshal([]byte(rec.body), &sent); err != nil || sent["backup"] != "identity-20261029T101500.000Z.yaml" {
			t.Errorf("body = %q", rec.body)
		}
	})

	t.Run("no backup argument sends latest", func(t *testing.T) {
		s, rec := newIdentityStandIn(t, 200, `{"role":"operator"}`)
		if res, err := s.handleIdentityRestore(t.Context(), identityReq(map[string]any{})); err != nil || res.IsError {
			t.Fatalf("err=%v res=%+v", err, res)
		}
		var sent map[string]string
		if err := json.Unmarshal([]byte(rec.body), &sent); err != nil || sent["backup"] != "latest" {
			t.Errorf("body = %q, want backup latest", rec.body)
		}
	})

	t.Run("daemon error reaches the result", func(t *testing.T) {
		s, _ := newIdentityStandIn(t, 404, `{"error":"backup not found"}`)
		res, err := s.handleIdentityRestore(t.Context(), identityReq(map[string]any{"backup": "nope"}))
		if err != nil || !res.IsError {
			t.Fatalf("err=%v res=%+v", err, res)
		}
		if txt := identityResultText(t, res); !strings.Contains(txt, "404") || !strings.Contains(txt, "backup not found") {
			t.Errorf("result = %q", txt)
		}
	})
}

func TestIdentityReset_Tool(t *testing.T) {
	s, rec := newIdentityStandIn(t, 200, `{}`)
	res, err := s.handleIdentityReset(t.Context(), identityReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/identity/reset" {
		t.Errorf("request = %s %s", rec.method, rec.path)
	}
	if strings.TrimSpace(rec.body) != "" {
		t.Errorf("reset sent a body: %q", rec.body)
	}
	desc := s.toolIdentityReset().Description
	for _, need := range []string{"on purpose", "backup", "restore_identity"} {
		if !strings.Contains(desc, need) {
			t.Errorf("reset_identity description lacks %q: %s", need, desc)
		}
	}
}

func TestSetIdentity_WipeRefusalReachesCaller(t *testing.T) {
	refusal := `{"error":"refusing to replace the saved identity with an empty one; to clear it on purpose use POST /api/identity/reset","code":"identity_wipe_refused","fields_kept":["role"]}`
	for name, call := range map[string]func(*Server, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error){
		"set_identity": func(s *Server, r mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleIdentitySet(t.Context(), r)
		},
		"update_identity": func(s *Server, r mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return s.handleIdentityUpdate(t.Context(), r)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, rec := newIdentityStandIn(t, 409, refusal)
			res, err := call(s, identityReq(map[string]any{}))
			if err != nil {
				t.Fatalf("handler returned a Go error instead of a tool result: %v", err)
			}
			if !res.IsError {
				t.Fatalf("refusal not flagged as an error: %+v", res)
			}
			txt := identityResultText(t, res)
			for _, need := range []string{"409", "identity_wipe_refused", "refusing to replace", "reset_identity", "restore_identity"} {
				if !strings.Contains(txt, need) {
					t.Errorf("result lacks %q: %s", need, txt)
				}
			}
			if rec.body != "{}" {
				t.Errorf("empty call sent body %q", rec.body)
			}
		})
	}
}

func TestIdentityTools_DescriptionsAndGuidance(t *testing.T) {
	s := &Server{}
	set := s.toolIdentitySet().Description
	if strings.Contains(set, "Empty fields are cleared") {
		t.Errorf("set_identity still says empty fields are cleared: %s", set)
	}
	for _, need := range []string{"refused", "reset_identity"} {
		if !strings.Contains(set, need) {
			t.Errorf("set_identity description lacks %q: %s", need, set)
		}
	}
	res, err := s.handleIdentityConfigure(t.Context(), identityReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range []string{"restore_identity", "identity_backups", "reset_identity"} {
		if !strings.Contains(identityResultText(t, res), need) {
			t.Errorf("configure_identity guidance lacks %q", need)
		}
	}
}
