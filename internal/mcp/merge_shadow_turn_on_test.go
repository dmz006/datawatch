package mcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMergeShadowMCPKeepsTheTurnOnReportUnderAProjectFilter(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"project":"a"},{"project":"b"}],"turn_on":{"project":"b","lines":["report for b"]}}`))
	})
	for project, want := range map[string]bool{"b": true, "a": false} {
		res, err := s.handleAutonomousMergeShadowStatus(context.Background(), callReq(map[string]any{"project": project}))
		if txt := resultText(t, res, err); strings.Contains(txt, "report for b") != want {
			t.Errorf("filter %s: %s", project, txt)
		}
	}
}

func TestShippedRecordMCPSendsHandChecks(t *testing.T) {
	var body string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		_, _ = w.Write([]byte(`{}`))
	})
	res, err := s.handleAutonomousShippedRecord(context.Background(), callReq(map[string]any{"id": "a1", "merge_commit": "abc", "source": "merge_step", "hand_checks": "lint=pass, build=skip"}))
	resultText(t, res, err)
	if !strings.Contains(body, `"hand_checks":{"build":"skip","lint":"pass"}`) {
		t.Errorf("body %s", body)
	}
	res, err = s.handleAutonomousShippedRecord(context.Background(), callReq(map[string]any{"id": "a1", "merge_commit": "abc", "hand_checks": "lint=yes"}))
	if err != nil || !res.IsError {
		t.Errorf("an unknown result was accepted: %v %+v", err, res)
	}
	if _, ok := s.toolAutonomousShippedRecord().InputSchema.Properties["hand_checks"]; !ok {
		t.Error("the tool does not declare hand_checks")
	}
}
