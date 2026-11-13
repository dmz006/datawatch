package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

const specCheckReplyDirty = `{"id":"p1","spec_check":[{"rule":"names_tracker_id","phrase":"BL123","severity":"warning","correctable":true,"suggestion":"remove 'BL123'"}]}`
const specCheckReplyClean = `{"id":"p1","spec_check":[]}`

func specCheckTexts(res *mcpsdk.CallToolResult) []string {
	var out []string
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			out = append(out, tc.Text)
		}
	}
	return out
}

func TestSpecCheckMCPCreateAndReviseShowFindings(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(specCheckReplyDirty))
	})
	calls := map[string]func() (*mcpsdk.CallToolResult, error){
		"create": func() (*mcpsdk.CallToolResult, error) {
			return s.handleAutonomousPRDCreate(context.Background(), callReq(map[string]any{"spec": "Fix BL123"}))
		},
		"revise": func() (*mcpsdk.CallToolResult, error) {
			return s.handleAutonomousPRDRequestRevision(context.Background(), callReq(map[string]any{"id": "p1", "note": "n"}))
		},
	}
	for name, call := range calls {
		res, err := call()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		texts := specCheckTexts(res)
		if len(texts) != 2 {
			t.Fatalf("%s: want JSON text plus the findings text, got %v", name, texts)
		}
		if !strings.Contains(texts[0], `"spec_check"`) {
			t.Fatalf("%s: JSON text lacks spec_check: %s", name, texts[0])
		}
		if !strings.Contains(texts[1], "BL123") || !strings.Contains(texts[1], "daemon can fix") {
			t.Fatalf("%s: findings text = %q", name, texts[1])
		}
	}
}

func TestSpecCheckMCPCleanSpecShowsNone(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(specCheckReplyClean))
	})
	res, err := s.handleAutonomousPRDCreate(context.Background(), callReq(map[string]any{"spec": "Add a retry"}))
	if err != nil {
		t.Fatal(err)
	}
	if texts := specCheckTexts(res); len(texts) != 1 {
		t.Fatalf("clean spec must add no findings text, got %v", texts)
	}
	res, err = s.handleAutonomousPRDRequestRevision(context.Background(), callReq(map[string]any{"id": "p1"}))
	if err != nil {
		t.Fatal(err)
	}
	if texts := specCheckTexts(res); len(texts) != 1 {
		t.Fatalf("clean revise must add no findings text, got %v", texts)
	}
}
