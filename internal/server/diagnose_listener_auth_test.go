// diagnose: per-listener auth posture checks.
//
// runDiagnose adds one listener_<name> check per entry of
// config.ListenerAuthStates. Every one of them is informational
// (OK=true whatever the posture) and carries a single parseable
// detail string: address=, enabled=, auth_required=, loopback=,
// acknowledged=, exposed=. The pre-existing rest_auth and
// mcp_sse_auth checks keep their names and detail text.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// listenerChecks runs GET /api/diagnose against s and indexes the
// response's checks by name.
func listenerChecks(t *testing.T, s *Server) map[string]DiagnoseCheck {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/diagnose", nil)
	rr := httptest.NewRecorder()
	s.handleDiagnose(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("diagnose status=%d body=%s", rr.Code, rr.Body.String())
	}
	var res DiagnoseResult
	if err := json.NewDecoder(rr.Body).Decode(&res); err != nil {
		t.Fatalf("decode diagnose response: %v", err)
	}
	byName := make(map[string]DiagnoseCheck, len(res.Checks))
	for _, c := range res.Checks {
		byName[c.Name] = c
	}
	return byName
}

// detailField extracts one key=value pair from a listener detail string.
func detailField(t *testing.T, detail, key string) string {
	t.Helper()
	re := regexp.MustCompile(`(^|\s)` + regexp.QuoteMeta(key) + `=(\S+)`)
	m := re.FindStringSubmatch(detail)
	if m == nil {
		t.Fatalf("detail %q missing %s=", detail, key)
	}
	return m[2]
}

// listenerTestServer returns a server whose config has the REST and
// mcp_sse listeners enabled on the given host, with the server token
// set when token != "".
func listenerTestServer(t *testing.T, host string, token string) *Server {
	t.Helper()
	s := bl90Server(t)
	s.cfg.Server.Enabled = true
	s.cfg.Server.Host = host
	s.cfg.Server.Port = 8443
	s.cfg.Server.Token = token
	s.cfg.MCP.SSEEnabled = true
	s.cfg.MCP.SSEHost = host
	s.cfg.MCP.SSEPort = 14900
	if token != "" {
		s.token = token // live token, as the daemon holds it
	}
	return s
}

func TestListenerAuth_ChecksExistForEveryListener(t *testing.T) {
	s := listenerTestServer(t, "0.0.0.0", "")
	checks := listenerChecks(t, s)
	want := config.ListenerAuthStates(s.cfg)
	if len(want) == 0 {
		t.Fatal("ListenerAuthStates returned no listeners")
	}
	for _, st := range want {
		c, ok := checks["listener_"+st.Name]
		if !ok {
			t.Errorf("missing listener_%s check; got %v", st.Name, diagnoseCheckNames(checks))
			continue
		}
		if c.OK != true {
			t.Errorf("listener_%s must be OK=true whatever the posture, got %+v", st.Name, c)
		}
	}
}

func TestListenerAuth_OKStaysTrueOpenAndProtected(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token string
	}{
		{"open", ""},
		{"protected", "secret-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := listenerTestServer(t, "0.0.0.0", tc.token)
			checks := listenerChecks(t, s)
			names := 0
			for name, c := range checks {
				if !strings.HasPrefix(name, "listener_") {
					continue
				}
				names++
				if !c.OK {
					t.Errorf("%s: %s must stay OK=true in the %s case, got %+v", tc.name, name, tc.name, c)
				}
			}
			if names == 0 {
				t.Error("no listener_ checks in diagnose output")
			}
		})
	}
}

func TestListenerAuth_ExposedOpen(t *testing.T) {
	s := listenerTestServer(t, "0.0.0.0", "")
	checks := listenerChecks(t, s)
	for _, name := range []string{"listener_rest", "listener_mcp_sse"} {
		c, ok := checks[name]
		if !ok {
			t.Fatalf("missing %s check", name)
		}
		if got := detailField(t, c.Detail, "exposed"); got != "true" {
			t.Errorf("%s: exposed=%q, want true (open listener); detail=%q", name, got, c.Detail)
		}
		if got := detailField(t, c.Detail, "auth_required"); got != "false" {
			t.Errorf("%s: auth_required=%q, want false; detail=%q", name, got, c.Detail)
		}
		if got := detailField(t, c.Detail, "loopback"); got != "false" {
			t.Errorf("%s: loopback=%q, want false; detail=%q", name, got, c.Detail)
		}
	}
}

func TestListenerAuth_ExposedProtected(t *testing.T) {
	s := listenerTestServer(t, "0.0.0.0", "secret-token")
	checks := listenerChecks(t, s)
	for _, name := range []string{"listener_rest", "listener_mcp_sse"} {
		c, ok := checks[name]
		if !ok {
			t.Fatalf("missing %s check", name)
		}
		for key, want := range map[string]string{
			"auth_required": "true",
			"exposed":       "false",
		} {
			if got := detailField(t, c.Detail, key); got != want {
				t.Errorf("%s: %s=%q, want %q; detail=%q", name, key, got, want, c.Detail)
			}
		}
	}
}

func TestListenerAuth_DetailFieldsComplete(t *testing.T) {
	s := listenerTestServer(t, "0.0.0.0", "")
	checks := listenerChecks(t, s)
	c, ok := checks["listener_rest"]
	if !ok {
		t.Fatal("missing listener_rest check")
	}
	for _, key := range []string{"address", "enabled", "auth_required", "loopback", "acknowledged", "exposed"} {
		if !strings.Contains(c.Detail, key+"=") {
			t.Errorf("listener_rest detail missing %s=: %q", key, c.Detail)
		}
	}
	if got := detailField(t, c.Detail, "address"); got != "0.0.0.0:8443" {
		t.Errorf("address=%q, want 0.0.0.0:8443; detail=%q", got, c.Detail)
	}
}

func TestListenerAuth_ExistingRestAndMCPAuthChecksUnchanged(t *testing.T) {
	cases := []struct {
		token  string
		detail string
	}{
		{"", "auth_required=false"},
		{"secret-token", "auth_required=true"},
	}
	for _, tc := range cases {
		t.Run("token=\""+tc.token+"\"", func(t *testing.T) {
			s := listenerTestServer(t, "127.0.0.1", tc.token)
			checks := listenerChecks(t, s)
			for _, name := range []string{"rest_auth", "mcp_sse_auth"} {
				c, ok := checks[name]
				if !ok {
					t.Fatalf("missing %s check", name)
				}
				if c.Detail != tc.detail {
					t.Errorf("%s detail = %q, want %q", name, c.Detail, tc.detail)
				}
				if !c.OK {
					t.Errorf("%s must stay OK=true, got %+v", name, c)
				}
			}
		})
	}
}

// TestListenerAuth_OverallOKUnchangedByPosture pins the spec that
// the listener checks never flip the composite: it must report the
// same overall ok value for an open and a token-protected listener.
func TestListenerAuth_OverallOKUnchangedByPosture(t *testing.T) {
	okOf := func(t *testing.T, host, token string) bool {
		t.Helper()
		s := listenerTestServer(t, host, token)
		req := httptest.NewRequest(http.MethodGet, "/api/diagnose", nil)
		rr := httptest.NewRecorder()
		s.handleDiagnose(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("diagnose status=%d", rr.Code)
		}
		var res DiagnoseResult
		_ = json.NewDecoder(rr.Body).Decode(&res)
		return res.OK
	}
	if a, b := okOf(t, "0.0.0.0", ""), okOf(t, "0.0.0.0", "secret-token"); a != b {
		t.Errorf("overall ok changed with listener posture: open=%v protected=%v", a, b)
	}
}

func diagnoseCheckNames(m map[string]DiagnoseCheck) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
