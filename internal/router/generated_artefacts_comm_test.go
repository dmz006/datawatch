package router

import (
	"net/http"
	"strings"
	"testing"
)

func genArtRouter(t *testing.T, reqs *[]capturedDaemonRequest, body string) (*Router, func()) {
	port, closeFn := fakeDaemonPlanCoverage(t, reqs, body)
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	return r, closeFn
}

func TestGeneratedArtefactsComm_Requests(t *testing.T) {
	cases := []struct {
		msg, method, path, body string
	}{
		{"autonomous generated-artefacts", http.MethodGet, "/api/autonomous/generated-artefacts", ""},
		{"autonomous generated-artefacts set gate_report off", http.MethodPut, "/api/config", `"autonomous.generated_artefacts.gate_report":false`},
		{"autonomous generated-artefacts set parity_draft on", http.MethodPut, "/api/config", `"autonomous.generated_artefacts.parity_draft":true`},
		{"autonomous change-record a1", http.MethodGet, "/api/autonomous/prds/a1/change-record", ""},
		{"autonomous regenerate a1 gate_report", http.MethodPost, "/api/autonomous/prds/a1/generated/gate_report/regenerate", ""},
	}
	for _, tc := range cases {
		var reqs []capturedDaemonRequest
		r, closeFn := genArtRouter(t, &reqs, `{"ok":true}`)
		r.HandleTestMessage(tc.msg)
		closeFn()
		if len(reqs) != 1 || reqs[0].method != tc.method || reqs[0].path != tc.path || !strings.Contains(reqs[0].body, tc.body) {
			t.Errorf("%q: requests = %v, want %s %s body~%q", tc.msg, reqs, tc.method, tc.path, tc.body)
		}
	}
}

func TestGeneratedArtefactsComm_UsageAndErrors(t *testing.T) {
	usage := map[string]string{
		"autonomous generated-artefacts set gate_report":       "usage: autonomous generated-artefacts",
		"autonomous generated-artefacts set gate_report maybe": "must be on or off",
		"autonomous generated-artefacts bogus x y":             "usage: autonomous generated-artefacts",
		"autonomous change-record":                             "usage: autonomous change-record",
		"autonomous regenerate a1":                             "usage: autonomous regenerate",
	}
	for msg, want := range usage {
		var reqs []capturedDaemonRequest
		r, closeFn := genArtRouter(t, &reqs, `{}`)
		out := strings.Join(r.HandleTestMessage(msg), "\n")
		closeFn()
		if !strings.Contains(out, want) || len(reqs) != 0 {
			t.Errorf("%q: reply %q reqs %v, want usage %q and no request", msg, out, reqs, want)
		}
	}
	var reqs []capturedDaemonRequest
	r, closeFn := genArtRouter(t, &reqs, "STATUS:404:no change data")
	out := strings.Join(r.HandleTestMessage("autonomous regenerate a1 gate_report"), "\n")
	closeFn()
	if !strings.Contains(out, "failed") || !strings.Contains(out, "no change data") {
		t.Errorf("error reply = %q", out)
	}
}
