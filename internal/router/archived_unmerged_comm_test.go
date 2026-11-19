package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const aumListBody = `{"archived_unmerged":[{"id":"done-1","title":"Parser rewrite","former_status":"completed","branch":"work/parser","ahead":3,"state":"ahead"},{"id":"bad-2","title":"Cache layer","branch":"work/cache","state":"unknown","detail":"git failed"}],"total":2}`

func aumDaemon(t *testing.T, reqs *[]subsDaemonRequest, status int, body string) (int, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reqs = append(*reqs, subsDaemonRequest{method: r.Method, path: r.URL.Path, rawquery: r.URL.RawQuery})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	port := 0
	idx := strings.LastIndex(srv.URL, ":")
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return port, srv.Close
}

func TestArchivedUnmergedCommListTitleFirst(t *testing.T) {
	var reqs []subsDaemonRequest
	port, stop := aumDaemon(t, &reqs, 200, aumListBody)
	defer stop()
	out := joinReplies(subsRouter(t, "grp1", port).HandleTestMessage("autonomous archived-unmerged"))
	if len(reqs) != 1 || reqs[0].method != "GET" || reqs[0].path != "/api/autonomous/archived-unmerged" {
		t.Fatalf("requests = %v", reqs)
	}
	for _, want := range []string{"Parser rewrite (done-1)", "3 commits ahead", "was completed", "Cache layer (bad-2)", "comparison failed: git failed", "carry-forward <id>"} {
		if !strings.Contains(out, want) {
			t.Errorf("reply lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Parser rewrite") > strings.Index(out, "done-1") {
		t.Errorf("title must come before the id:\n%s", out)
	}
}

func TestArchivedUnmergedCommCarryForward(t *testing.T) {
	var reqs []subsDaemonRequest
	port, stop := aumDaemon(t, &reqs, 200, `{"id":"draft-1","title":"Parser rewrite","status":"draft","git":{"branch":"work/parser"}}`)
	defer stop()
	out := joinReplies(subsRouter(t, "grp1", port).HandleTestMessage("autonomous archived-unmerged carry-forward done-1 force"))
	if len(reqs) != 1 || reqs[0].method != "POST" || reqs[0].path != "/api/autonomous/archived-unmerged/done-1/carry-forward" {
		t.Fatalf("requests = %v", reqs)
	}
	if !strings.Contains(out, "Parser rewrite (draft-1)") || !strings.Contains(out, "work/parser") {
		t.Errorf("reply = %s", out)
	}
}

func TestArchivedUnmergedCommRefusals(t *testing.T) {
	r := bareSubsRouter()
	for _, in := range []string{"autonomous archived-unmerged carry-forward", "autonomous archived-unmerged bogus", "autonomous archived-unmerged carry-forward a b"} {
		out := joinReplies(r.HandleTestMessage(in))
		if !strings.Contains(out, "usage") && !strings.Contains(out, "unknown option") {
			t.Errorf("%q: reply = %s", in, out)
		}
	}
	var reqs []subsDaemonRequest
	port, stop := aumDaemon(t, &reqs, 409, "carry forward: branch work/x no longer exists")
	defer stop()
	out := joinReplies(subsRouter(t, "grp1", port).HandleTestMessage("autonomous archived-unmerged carry-forward x"))
	if !strings.Contains(out, "failed") || !strings.Contains(out, "no longer exists") {
		t.Errorf("daemon refusal not shown: %s", out)
	}
}

func TestArchivedUnmergedCommBadSettingRefused(t *testing.T) {
	var wrote []string
	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(key, value string) error { wrote = append(wrote, key+"="+value); return nil })
	for _, bad := range []string{"maybe", "1", ""} {
		if err := r.configureWrite(archiveKeepUnmergedKey, bad); err == nil || !strings.Contains(err.Error(), "archive_keep_unmerged") {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
	if len(wrote) != 0 {
		t.Fatalf("refused value was written: %v", wrote)
	}
	for _, ok := range []string{"true", "false"} {
		if err := r.configureWrite(archiveKeepUnmergedKey, ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	if len(wrote) != 2 {
		t.Fatalf("wrote %v", wrote)
	}
}
