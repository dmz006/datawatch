// BL368 Phase 3 — MCP vision_describe tool unit tests.
//
// TC-1: no visioner → error result (not nil error)
// TC-2: missing image_path argument → error result
// TC-3: image_path file not found → error result
// TC-4: happy path → description in result text

package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// fakeMCPVisioner is a test double for VisionerMCP.
type fakeMCPVisioner struct {
	result string
	err    error
}

func (f *fakeMCPVisioner) Describe(_ context.Context, _ []byte, _, _ string) (string, error) {
	return f.result, f.err
}

func toolResultText(res *mcpsdk.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if txt, ok := res.Content[0].(mcpsdk.TextContent); ok {
		return txt.Text
	}
	return ""
}

// TC-1: no visioner → error message in result.
func TestBL368_VisionDescribe_NoVisioner_ReturnsError(t *testing.T) {
	s := &Server{}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"image_path": "/some/file.png"}

	res, err := s.handleVisionDescribe(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	text := toolResultText(res)
	if !strings.Contains(text, "vision not enabled") {
		t.Errorf("want 'vision not enabled', got %q", text)
	}
}

// TC-2: missing image_path argument → error message.
func TestBL368_VisionDescribe_MissingImagePath_ReturnsError(t *testing.T) {
	s := &Server{visioner: &fakeMCPVisioner{result: "desc"}}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{} // no image_path

	res, err := s.handleVisionDescribe(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	text := toolResultText(res)
	if !strings.Contains(text, "image_path is required") {
		t.Errorf("want 'image_path is required', got %q", text)
	}
}

// TC-3: image_path file not found → error message in result.
func TestBL368_VisionDescribe_FileNotFound_ReturnsError(t *testing.T) {
	s := &Server{visioner: &fakeMCPVisioner{result: "desc"}}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"image_path": "/nonexistent/ts-bl368-mcp.png"}

	res, err := s.handleVisionDescribe(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	text := toolResultText(res)
	if !strings.Contains(text, "Error reading image") {
		t.Errorf("want 'Error reading image', got %q", text)
	}
}

// TC-4: happy path — description returned in result.
func TestBL368_VisionDescribe_HappyPath_ReturnsDescription(t *testing.T) {
	dir := t.TempDir()
	imgFile := filepath.Join(dir, "test.png")
	if err := os.WriteFile(imgFile, []byte("\x89PNG\r\n\x1a\n"), 0600); err != nil {
		t.Fatal(err)
	}

	s := &Server{visioner: &fakeMCPVisioner{result: "a blue sky with clouds"}}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"image_path": imgFile}

	res, err := s.handleVisionDescribe(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	text := toolResultText(res)
	if text != "a blue sky with clouds" {
		t.Errorf("want description, got %q", text)
	}
}
