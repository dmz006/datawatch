package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type defectCall struct {
	method, path, query string
	body                map[string]any
}

func defectFake(t *testing.T, reply string, calls *[]defectCall) *Router {
	t.Helper()
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&b)
		}
		*calls = append(*calls, defectCall{r.Method, r.URL.Path, r.URL.RawQuery, b})
		_, _ = w.Write([]byte(reply))
	})
	return setupRetryRouter(t, port)
}

func TestDefectIntakeCommPrintsRouteAndReason(t *testing.T) {
	var calls []defectCall
	r := defectFake(t, `{"route":"open_batch","batch_id":"b1","reason":"same area as open batch"}`, &calls)
	out := joinReplies(r.HandleTestMessage("autonomous defect-intake login button misaligned project=web kind=bug files=a.go,b.go priority=2"))
	if len(calls) != 1 || calls[0].method != http.MethodPost || calls[0].path != "/api/autonomous/defects/intake" {
		t.Fatalf("calls = %+v", calls)
	}
	b := calls[0].body
	if b["project"] != "web" || b["kind"] != "bug" || b["priority"] != float64(2) || b["description"] != "login button misaligned" {
		t.Errorf("body = %+v", b)
	}
	if b["id"] == "" || b["title"] == "" {
		t.Errorf("id/title missing: %+v", b)
	}
	if fs, _ := b["files"].([]any); len(fs) != 2 {
		t.Errorf("files = %v", b["files"])
	}
	for _, want := range []string{"route: open_batch", "reason: same area as open batch", "batch: b1"} {
		if !strings.Contains(out, want) {
			t.Errorf("reply missing %q: %q", want, out)
		}
	}
}

func TestDefectIntakeCommUsageNoNetwork(t *testing.T) {
	var calls []defectCall
	r := defectFake(t, `{}`, &calls)
	for _, m := range []string{"autonomous defect-intake", "autonomous defect-intake some text", "autonomous defect-intake x project=p priority=zz"} {
		if out := joinReplies(r.HandleTestMessage(m)); !strings.Contains(out, "usage") {
			t.Errorf("%q: want usage, got %q", m, out)
		}
	}
	if len(calls) != 0 {
		t.Errorf("made %d calls", len(calls))
	}
}

func TestDefectBatchesComm(t *testing.T) {
	var calls []defectCall
	r := defectFake(t, `{"batches":[{"id":"b1"}],"total":1}`, &calls)
	out := joinReplies(r.HandleTestMessage("autonomous defect-batches project=web"))
	if len(calls) != 1 || calls[0].method != http.MethodGet || calls[0].path != "/api/autonomous/defects/batches" || calls[0].query != "project=web" {
		t.Fatalf("calls = %+v", calls)
	}
	if !strings.Contains(out, "b1") {
		t.Errorf("reply = %q", out)
	}
	calls = nil
	r.HandleTestMessage("autonomous defect-batches")
	if len(calls) != 1 || calls[0].query != "" {
		t.Errorf("no-arg calls = %+v", calls)
	}
	calls = nil
	if out := joinReplies(r.HandleTestMessage("autonomous defect-batches web")); !strings.Contains(out, "usage") || len(calls) != 0 {
		t.Errorf("bare arg: out=%q calls=%d", out, len(calls))
	}
}

func TestDefectSplitComm(t *testing.T) {
	var calls []defectCall
	r := defectFake(t, `{"automaton_id":"a9","batch_id":"b1","story_id":"s2"}`, &calls)
	out := joinReplies(r.HandleTestMessage("autonomous defect-split b1 s2"))
	if len(calls) != 1 || calls[0].method != http.MethodPost || calls[0].path != "/api/autonomous/defects/batches/b1/split" || calls[0].body["story_id"] != "s2" {
		t.Fatalf("calls = %+v", calls)
	}
	if !strings.Contains(out, "a9") {
		t.Errorf("reply = %q", out)
	}
	calls = nil
	if out := joinReplies(r.HandleTestMessage("autonomous defect-split b1")); !strings.Contains(out, "usage") || len(calls) != 0 {
		t.Errorf("usage: out=%q calls=%d", out, len(calls))
	}
}

func TestDefectCommErrorSurfaces(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "defect queue is disabled", http.StatusConflict)
	})
	r := setupRetryRouter(t, port)
	out := joinReplies(r.HandleTestMessage("autonomous defect-intake bug project=p"))
	if !strings.Contains(out, "failed") || !strings.Contains(out, "disabled") {
		t.Errorf("reply = %q", out)
	}
}
