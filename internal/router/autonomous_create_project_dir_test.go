// Comm-channel parity test: `autonomous create <spec>` (and the `prd`
// alias) must POST to /api/autonomous/prds with a body that carries only
// the spec — never a project_dir defaulted from the process cwd — so an
// operator's comm-channel create is indistinguishable from the MCP
// autonomous_prd_create path, which also sends an empty project_dir.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capturedCreateRequest records what the fake daemon received for the
// outbound autonomous-create call.
type capturedCreateRequest struct {
	method string
	path   string
	body   string
}

// fakeDaemonCreate stands in for the local daemon loopback and captures
// the request sent by the router's commJSON path.
func fakeDaemonCreate(t *testing.T, cap *capturedCreateRequest) (int, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"prd_test123","spec":"ok"}`))
	}))
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return port, srv.Close
}

// checkCreateBody decodes the captured JSON body and asserts the spec is
// present and unaltered and that project_dir is absent (or, if it happens
// to be serialized, empty) — it must never be defaulted to the process
// cwd.
func checkCreateBody(t *testing.T, body, wantSpec string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("body is not valid JSON: %v (body=%q)", err, body)
	}
	if got, _ := m["spec"].(string); got != wantSpec {
		t.Errorf("body spec = %q want %q (body=%q)", got, wantSpec, body)
	}
	v, ok := m["project_dir"]
	if !ok {
		return // desired: key absent
	}
	s, isStr := v.(string)
	if !isStr || s != "" {
		t.Errorf("body project_dir = %v want absent or empty string (body=%q)", v, body)
	}
}

func TestAutonomousCreate_PostsSpecOnly_NoProjectDir(t *testing.T) {
	const spec = "Add a search-icon toggle to the sessions list"
	cap := &capturedCreateRequest{}
	port, closeFn := fakeDaemonCreate(t, cap)
	defer closeFn()

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	r.HandleTestMessage("autonomous create " + spec)

	// The command text from autonomous_test.go's parser fixtures must map
	// to exactly one POST against the daemon's create endpoint.
	if cap.method != http.MethodPost {
		t.Fatalf("method = %q want POST", cap.method)
	}
	if cap.path != "/api/autonomous/prds" {
		t.Fatalf("path = %q want /api/autonomous/prds", cap.path)
	}
	checkCreateBody(t, cap.body, spec)
}

// The `prd` alias routes to the same dispatcher and must produce the
// same body.
func TestPRDAlias_Create_PostsSpecOnly_NoProjectDir(t *testing.T) {
	const spec = "Add a search-icon toggle to the sessions list"
	cap := &capturedCreateRequest{}
	port, closeFn := fakeDaemonCreate(t, cap)
	defer closeFn()

	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)

	r.HandleTestMessage("prd create " + spec)

	if cap.method != http.MethodPost {
		t.Fatalf("method = %q want POST", cap.method)
	}
	if cap.path != "/api/autonomous/prds" {
		t.Fatalf("path = %q want /api/autonomous/prds", cap.path)
	}
	checkCreateBody(t, cap.body, spec)
}
