package router

import (
	"strings"
	"testing"
)

func TestParse_New(t *testing.T) {
	cmd := Parse("new: write unit tests")
	if cmd.Type != CmdNew {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdNew)
	}
	if cmd.Text != "write unit tests" {
		t.Errorf("Text = %q, want %q", cmd.Text, "write unit tests")
	}
	if cmd.ProjectDir != "" {
		t.Errorf("ProjectDir = %q, want empty", cmd.ProjectDir)
	}
}

func TestParse_NewWithProjectDir(t *testing.T) {
	cmd := Parse("new: /home/user/myproject: add a login page")
	if cmd.Type != CmdNew {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdNew)
	}
	if cmd.ProjectDir != "/home/user/myproject" {
		t.Errorf("ProjectDir = %q, want /home/user/myproject", cmd.ProjectDir)
	}
	if cmd.Text != "add a login page" {
		t.Errorf("Text = %q, want %q", cmd.Text, "add a login page")
	}
}

func TestParse_NewCaseInsensitive(t *testing.T) {
	cmd := Parse("NEW: some task")
	if cmd.Type != CmdNew {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdNew)
	}
	if cmd.Text != "some task" {
		t.Errorf("Text = %q, want %q", cmd.Text, "some task")
	}
}

func TestParse_NewStripsWhitespace(t *testing.T) {
	cmd := Parse("  new:   build the thing  ")
	if cmd.Type != CmdNew {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdNew)
	}
	if cmd.Text != "build the thing" {
		t.Errorf("Text = %q, want %q", cmd.Text, "build the thing")
	}
}

func TestParse_NewNoTask(t *testing.T) {
	cmd := Parse("new:")
	if cmd.Type != CmdNew {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdNew)
	}
	if cmd.Text != "" {
		t.Errorf("Text = %q, want empty for bare new:", cmd.Text)
	}
}

func TestParse_List(t *testing.T) {
	for _, input := range []string{"list", "LIST", "List"} {
		cmd := Parse(input)
		if cmd.Type != CmdList {
			t.Errorf("Parse(%q).Type = %q, want %q", input, cmd.Type, CmdList)
		}
	}
}

func TestParse_Status(t *testing.T) {
	cmd := Parse("status abc1")
	if cmd.Type != CmdStatus {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdStatus)
	}
	if cmd.SessionID != "abc1" {
		t.Errorf("SessionID = %q, want abc1", cmd.SessionID)
	}
}

func TestParse_Send(t *testing.T) {
	cmd := Parse("send abc1: yes, proceed with the changes")
	if cmd.Type != CmdSend {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdSend)
	}
	if cmd.SessionID != "abc1" {
		t.Errorf("SessionID = %q, want abc1", cmd.SessionID)
	}
	if cmd.Text != "yes, proceed with the changes" {
		t.Errorf("Text = %q, want %q", cmd.Text, "yes, proceed with the changes")
	}
}

func TestParse_SendMissingColon(t *testing.T) {
	cmd := Parse("send abc1 yes proceed")
	if cmd.Type != CmdUnknown {
		t.Errorf("Type = %q, want %q (missing colon)", cmd.Type, CmdUnknown)
	}
}

func TestParse_SendColonInMessage(t *testing.T) {
	// Colon in the message body should be preserved
	cmd := Parse("send abc1: use http://example.com")
	if cmd.Type != CmdSend {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdSend)
	}
	if !strings.Contains(cmd.Text, "http://example.com") {
		t.Errorf("Text = %q should contain URL", cmd.Text)
	}
}

func TestParse_Kill(t *testing.T) {
	cmd := Parse("kill abc1")
	if cmd.Type != CmdKill {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdKill)
	}
	if cmd.SessionID != "abc1" {
		t.Errorf("SessionID = %q, want abc1", cmd.SessionID)
	}
}

func TestParse_Tail_DefaultN(t *testing.T) {
	cmd := Parse("tail abc1")
	if cmd.Type != CmdTail {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdTail)
	}
	if cmd.SessionID != "abc1" {
		t.Errorf("SessionID = %q, want abc1", cmd.SessionID)
	}
	if cmd.TailN != 20 {
		t.Errorf("TailN = %d, want 20 (default)", cmd.TailN)
	}
}

func TestParse_Tail_WithN(t *testing.T) {
	cmd := Parse("tail abc1 50")
	if cmd.Type != CmdTail {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdTail)
	}
	if cmd.TailN != 50 {
		t.Errorf("TailN = %d, want 50", cmd.TailN)
	}
	if cmd.SessionID != "abc1" {
		t.Errorf("SessionID = %q, want abc1", cmd.SessionID)
	}
}

func TestParse_Attach(t *testing.T) {
	cmd := Parse("attach abc1")
	if cmd.Type != CmdAttach {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdAttach)
	}
	if cmd.SessionID != "abc1" {
		t.Errorf("SessionID = %q, want abc1", cmd.SessionID)
	}
}

func TestParse_History(t *testing.T) {
	cmd := Parse("history abc1")
	if cmd.Type != CmdHistory {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdHistory)
	}
	if cmd.SessionID != "abc1" {
		t.Errorf("SessionID = %q, want abc1", cmd.SessionID)
	}
}

func TestParse_Help(t *testing.T) {
	for _, input := range []string{"help", "HELP", "Help"} {
		cmd := Parse(input)
		if cmd.Type != CmdHelp {
			t.Errorf("Parse(%q).Type = %q, want %q", input, cmd.Type, CmdHelp)
		}
	}
}

func TestParse_Unknown(t *testing.T) {
	for _, text := range []string{"", "hello there", "123", "?", "do something", "quit", "exit"} {
		cmd := Parse(text)
		if cmd.Type != CmdUnknown {
			t.Errorf("Parse(%q).Type = %q, want %q", text, cmd.Type, CmdUnknown)
		}
	}
}

func TestHelpText(t *testing.T) {
	text := HelpText("myhost")

	if !strings.Contains(text, "myhost") {
		t.Error("HelpText should contain the hostname")
	}

	for _, keyword := range []string{"new:", "list", "status", "send", "kill", "tail", "attach", "history", "help"} {
		if !strings.Contains(text, keyword) {
			t.Errorf("HelpText missing keyword %q", keyword)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"hi", 2, "hi"},
		{"hi!", 2, "hi..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}
