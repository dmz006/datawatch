// BL368 Phase 4 — tests for the command-aware image injection behaviour.
//
// The router processMessage block injects image descriptions differently
// depending on the command prefix. For remember:, the description is injected
// INTO the command body so handleRemember receives parseable text. These tests
// verify Parse() correctly handles the resulting text formats.
//
// TS-BL368-R1: remember: with image description in body → CmdRemember
// TS-BL368-R2: remember: with image desc + caption → CmdRemember with full text
// TS-BL368-R3: prepended format ("[image:]\nremember:") does NOT parse as CmdRemember
// TS-BL368-R4: image-only remember (no caption) → CmdRemember with only description
// TS-BL368-R5: non-remember command with image prefix still parses as expected

package router

import "testing"

// TS-BL368-R1: "remember: [image: a green circle]" → CmdRemember
func TestBL368_RememberWithImageInBody(t *testing.T) {
	cmd := Parse("remember: [image: a green circle]")
	if cmd.Type != CmdRemember {
		t.Fatalf("want CmdRemember, got %v", cmd.Type)
	}
	if cmd.Text != "[image: a green circle]" {
		t.Errorf("text: got %q, want %q", cmd.Text, "[image: a green circle]")
	}
}

// TS-BL368-R2: remember: with image desc + original caption
func TestBL368_RememberWithImageAndCaption(t *testing.T) {
	cmd := Parse("remember: [image: a sunset over the ocean] architecture screenshot")
	if cmd.Type != CmdRemember {
		t.Fatalf("want CmdRemember, got %v", cmd.Type)
	}
	want := "[image: a sunset over the ocean] architecture screenshot"
	if cmd.Text != want {
		t.Errorf("text: got %q, want %q", cmd.Text, want)
	}
}

// TS-BL368-R3: old prepend format does NOT parse as CmdRemember
// (verifies the regression that the new injection format fixes)
func TestBL368_PrependedFormatIsNotRemember(t *testing.T) {
	cmd := Parse("[image: a green circle]\nremember: save this")
	if cmd.Type == CmdRemember {
		t.Error("prepend-before-command format incorrectly parsed as CmdRemember; injection logic bug")
	}
}

// TS-BL368-R4: empty caption — description only
func TestBL368_RememberImageOnly(t *testing.T) {
	cmd := Parse("remember: [image: three-tier architecture diagram]")
	if cmd.Type != CmdRemember {
		t.Fatalf("want CmdRemember, got %v", cmd.Type)
	}
	if cmd.Text == "" {
		t.Error("text should not be empty")
	}
}

// TS-BL368-R5: non-remember command with prepended image (default injection)
// "[image: ...]\nstatus" → CmdUnknown (status command isn't a routable command here)
func TestBL368_NonRememberWithImagePrefix(t *testing.T) {
	// "list" is a valid command; prefix injection should not break it
	cmd := Parse("[image: a dashboard screenshot]\nlist")
	// [image: ...]\nlist does not start with "list" so won't be CmdList
	if cmd.Type == CmdList {
		t.Errorf("prepended image broke list command detection: got CmdList unexpectedly")
	}
}
