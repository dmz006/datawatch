package router

import (
	"net/http"
	"strings"
	"testing"
)

const mergeStateField = `"merge_state":"finished_not_merged"`

func TestMergeStateCommGetShowsLine(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"p1","status":"completed",` + mergeStateField + `}`))
	})
	got := strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous get p1"), "\n")
	if !strings.Contains(got, "branch work not merged into the base branch (merge_state: finished_not_merged)") {
		t.Errorf("get reply lacks the merge state line: %s", got)
	}
}

func TestMergeStateCommGetOmitsLineWithoutField(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"p1","status":"completed"}`))
	})
	got := strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous get p1"), "\n")
	if strings.Contains(got, "branch work not merged") {
		t.Errorf("no merge_state field, yet reply shows one: %s", got)
	}
}

func TestMergeStateCommListShowsLine(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"prds":[{"id":"p1","title":"Alpha","status":"completed",` + mergeStateField + `},{"id":"p2","title":"Beta"}]}`))
	})
	got := strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous list"), "\n")
	if !strings.Contains(got, "Alpha (p1): branch work not merged into the base branch (merge_state: finished_not_merged)") {
		t.Errorf("list reply lacks the merge state line: %s", got)
	}
	if strings.Contains(got, "Beta (p2): branch work not merged") {
		t.Errorf("row without the field is marked: %s", got)
	}
}
