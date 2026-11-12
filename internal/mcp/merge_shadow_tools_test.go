package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const mergeShadowBody = `{"projects":[{"project":"a","plans":[],"comparisons":[],"counts":{"patch":{"matched":2,"different":1}}},{"project":"b","plans":[],"comparisons":[],"counts":{"minor":{"matched":0,"different":3}}}]}`

func TestMergeShadowMCP_Registered(t *testing.T) {
	for _, n := range mergeTestToolNames(t) {
		if n == "autonomous_merge_shadow_status" {
			return
		}
	}
	t.Error("autonomous_merge_shadow_status is not registered")
}

func TestMergeShadowMCP_ReturnsCounts(t *testing.T) {
	var path string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(mergeShadowBody))
	})
	res, err := s.handleAutonomousMergeShadowStatus(context.Background(), callReq(map[string]any{}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if path != mergeShadowPath {
		t.Errorf("path %q", path)
	}
	txt := resultText(t, res, err)
	if !strings.Contains(txt, `"matched":2`) || !strings.Contains(txt, `"different":3`) {
		t.Errorf("counts missing: %s", txt)
	}
}

func TestMergeShadowMCP_ProjectFilter(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(mergeShadowBody))
	})
	res, err := s.handleAutonomousMergeShadowStatus(context.Background(), callReq(map[string]any{"project": "b"}))
	txt := resultText(t, res, err)
	if strings.Contains(txt, `"project":"a"`) || !strings.Contains(txt, `"project":"b"`) {
		t.Errorf("filter wrong: %s", txt)
	}
}
