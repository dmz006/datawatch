package mcp

import (
	"context"
	"encoding/json"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// sweepText is a nil-safe text view of a tool result for error messages.
func sweepText(res *mcpsdk.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(mcpsdk.TextContent); ok {
		return tc.Text
	}
	return ""
}

// mcpFakeSweepSessions is a TagSweepSessionStore over one session carrying a
// stale system-tag list, recording every write it receives (used by
// TestTagSweepMCPTool to check dry-run vs real writes).
type mcpFakeSweepSessions struct {
	tags   []string
	writes map[string][]string
}

func (f *mcpFakeSweepSessions) ListTagSweepSessions() []autonomous.TagSweepSession {
	return []autonomous.TagSweepSession{{ID: "s1", SystemTags: f.tags, Placement: autonomous.Placement{}}}
}

func (f *mcpFakeSweepSessions) SetSystemTags(id string, tags []string) error {
	if f.writes == nil {
		f.writes = map[string][]string{}
	}
	f.writes[id] = tags
	return nil
}

func TestTagSweepMCPDefinition(t *testing.T) {
	s := &Server{}
	tool := s.toolAutonomousTagSweep()
	if tool.Name != "autonomous_tag_sweep" {
		t.Fatalf("tool name = %q, want autonomous_tag_sweep", tool.Name)
	}
	schema, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal input schema: %v", err)
	}
	var in struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &in); err != nil {
		t.Fatalf("input schema: %v", err)
	}
	p, ok := in.Properties["dry_run"]
	if !ok || p.Type != "boolean" {
		t.Errorf("input schema does not declare dry_run as a boolean: %s", schema)
	}
}

func TestTagSweepMCPTool(t *testing.T) {
	s, mgr := newTagMCPServer(t)
	fake := &mcpFakeSweepSessions{tags: []string{"lane:gpu"}}
	mgr.SetTagSweepSessions(fake)
	ctx := context.Background()

	// dry_run=true: re-derives, reports the would-be change, writes nothing.
	res, err := s.handleAutonomousTagSweep(ctx, tagReq(map[string]any{"dry_run": true}))
	if err != nil || res.IsError {
		t.Fatalf("dry_run: %v %s", err, sweepText(res))
	}
	var dry autonomous.SweepResult
	if err := json.Unmarshal([]byte(tagResultText(t, res)), &dry); err != nil {
		t.Fatalf("decode dry-run reply: %v", err)
	}
	if dry.Sessions.Examined != 1 || dry.Sessions.Changed != 1 || dry.Sessions.Unchanged != 0 {
		t.Errorf("dry_run sessions = %+v, want examined/changed 1", dry.Sessions)
	}
	if len(fake.writes) != 0 {
		t.Errorf("dry run wrote: %v", fake.writes)
	}

	// Real run (dry_run absent): writes the re-derived set through.
	res, err = s.handleAutonomousTagSweep(ctx, tagReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("sweep: %v %s", err, sweepText(res))
	}
	var real autonomous.SweepResult
	if err := json.Unmarshal([]byte(tagResultText(t, res)), &real); err != nil {
		t.Fatalf("decode sweep reply: %v", err)
	}
	if real.Sessions.Examined != 1 || real.Sessions.Changed != 1 {
		t.Errorf("sweep sessions = %+v, want examined/changed 1", real.Sessions)
	}
	if _, ok := fake.writes["s1"]; !ok {
		t.Fatalf("sweep did not write session tags: %v", fake.writes)
	}

	// Second run is a no-op: the session now holds the derived set.
	fake.tags = fake.writes["s1"]
	res, err = s.handleAutonomousTagSweep(ctx, tagReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("second sweep: %v %s", err, sweepText(res))
	}
	var again autonomous.SweepResult
	if err := json.Unmarshal([]byte(tagResultText(t, res)), &again); err != nil {
		t.Fatalf("decode second reply: %v", err)
	}
	if again.Sessions.Unchanged != 1 || again.Sessions.Changed != 0 {
		t.Errorf("second sweep sessions = %+v, want unchanged 1 changed 0", again.Sessions)
	}
}
