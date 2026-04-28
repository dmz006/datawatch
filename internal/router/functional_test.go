package router

import (
	"testing"
)

// Additional parser tests for commands not covered in commands_test.go.
// These cover memory, KG, pipeline, and config commands that exercise
// the same code path as /api/test/message (the functional test hook).

func TestParse_MemoriesStats(t *testing.T) {
	cmd := Parse("memories stats")
	if cmd.Type != CmdMemories {
		t.Errorf("expected CmdMemories, got %v", cmd.Type)
	}
	if cmd.Text != "stats" {
		t.Errorf("expected Text='stats', got %q", cmd.Text)
	}
}

func TestParse_MemoriesExport(t *testing.T) {
	cmd := Parse("memories export")
	if cmd.Type != CmdMemories {
		t.Errorf("expected CmdMemories, got %v", cmd.Type)
	}
	if cmd.Text != "export" {
		t.Errorf("expected Text='export', got %q", cmd.Text)
	}
}

func TestParse_MemoriesReindex(t *testing.T) {
	cmd := Parse("memories reindex")
	// memories reindex may parse as CmdMemories with text or CmdMemReindex
	if cmd.Type != CmdMemories && cmd.Type != CmdMemReindex {
		t.Errorf("expected CmdMemories or CmdMemReindex, got %v", cmd.Type)
	}
}

func TestParse_MemoriesTunnels(t *testing.T) {
	cmd := Parse("memories tunnels")
	if cmd.Type != CmdMemories {
		t.Errorf("expected CmdMemories, got %v", cmd.Type)
	}
	// May be "tunnels" or "__tunnels__" depending on parser
	if cmd.Text != "tunnels" && cmd.Text != "__tunnels__" {
		t.Errorf("expected tunnels text, got %q", cmd.Text)
	}
}

func TestParse_Remember(t *testing.T) {
	cmd := Parse("remember: CI needs Go 1.24")
	if cmd.Type != CmdRemember {
		t.Errorf("expected CmdRemember, got %v", cmd.Type)
	}
	if cmd.Text != "CI needs Go 1.24" {
		t.Errorf("expected 'CI needs Go 1.24', got %q", cmd.Text)
	}
}

func TestParse_Recall(t *testing.T) {
	cmd := Parse("recall: CI requirements")
	if cmd.Type != CmdRecall {
		t.Errorf("expected CmdRecall, got %v", cmd.Type)
	}
}

// v5.27.0 — mempalace alignment chat parsing.
func TestParse_MemPin(t *testing.T) {
	cmd := Parse("memory pin 42")
	if cmd.Type != CmdMemPin {
		t.Errorf("expected CmdMemPin, got %v", cmd.Type)
	}
	if cmd.Text != "42" {
		t.Errorf("text = %q want '42'", cmd.Text)
	}
	cmd2 := Parse("memory pin 42 off")
	if cmd2.Type != CmdMemPin {
		t.Errorf("expected CmdMemPin, got %v", cmd2.Type)
	}
	if cmd2.Text != "42 off" {
		t.Errorf("text = %q want '42 off'", cmd2.Text)
	}
}

func TestParse_MemSweep(t *testing.T) {
	cmd := Parse("memory sweep")
	if cmd.Type != CmdMemSweep {
		t.Errorf("expected CmdMemSweep, got %v", cmd.Type)
	}
	cmd2 := Parse("memory sweep apply 30")
	if cmd2.Type != CmdMemSweep {
		t.Errorf("expected CmdMemSweep, got %v", cmd2.Type)
	}
	if cmd2.Text != "apply 30" {
		t.Errorf("text = %q want 'apply 30'", cmd2.Text)
	}
}

func TestParse_MemSpell(t *testing.T) {
	cmd := Parse("memory spellcheck protocoll daemon")
	if cmd.Type != CmdMemSpell {
		t.Errorf("expected CmdMemSpell, got %v", cmd.Type)
	}
	if cmd.Text != "protocoll daemon" {
		t.Errorf("text = %q want 'protocoll daemon'", cmd.Text)
	}
}

func TestParse_MemExtract(t *testing.T) {
	cmd := Parse("memory extract Postgres depends on libpq")
	if cmd.Type != CmdMemExtract {
		t.Errorf("expected CmdMemExtract, got %v", cmd.Type)
	}
}

func TestParse_MemSchema(t *testing.T) {
	cmd := Parse("memory schema")
	if cmd.Type != CmdMemSchema {
		t.Errorf("expected CmdMemSchema, got %v", cmd.Type)
	}
}

func TestParse_Learnings(t *testing.T) {
	cmd := Parse("learnings")
	if cmd.Type != CmdLearnings {
		t.Errorf("expected CmdLearnings, got %v", cmd.Type)
	}
}

func TestParse_LearningsSearch(t *testing.T) {
	cmd := Parse("learnings search: JWT")
	if cmd.Type != CmdLearnings {
		t.Errorf("expected CmdLearnings, got %v", cmd.Type)
	}
}

func TestParse_Research(t *testing.T) {
	cmd := Parse("research: database schema")
	if cmd.Type != CmdResearch {
		t.Errorf("expected CmdResearch, got %v", cmd.Type)
	}
}

func TestParse_KGQuery(t *testing.T) {
	cmd := Parse("kg query Alice")
	if cmd.Type != CmdKG {
		t.Errorf("expected CmdKG, got %v", cmd.Type)
	}
}

func TestParse_KGAdd(t *testing.T) {
	cmd := Parse("kg add Alice works_on datawatch")
	if cmd.Type != CmdKG {
		t.Errorf("expected CmdKG, got %v", cmd.Type)
	}
}

func TestParse_KGInvalidate(t *testing.T) {
	cmd := Parse("kg invalidate Alice works_on datawatch")
	if cmd.Type != CmdKG {
		t.Errorf("expected CmdKG, got %v", cmd.Type)
	}
}

func TestParse_KGTimeline(t *testing.T) {
	cmd := Parse("kg timeline Alice")
	if cmd.Type != CmdKG {
		t.Errorf("expected CmdKG, got %v", cmd.Type)
	}
}

func TestParse_KGStats(t *testing.T) {
	cmd := Parse("kg stats")
	if cmd.Type != CmdKG {
		t.Errorf("expected CmdKG, got %v", cmd.Type)
	}
}

func TestParse_PipelineStart(t *testing.T) {
	cmd := Parse("pipeline: task1 -> task2 -> task3")
	if cmd.Type != CmdPipeline {
		t.Errorf("expected CmdPipeline, got %v", cmd.Type)
	}
}

func TestParse_PipelineStatus(t *testing.T) {
	cmd := Parse("pipeline status")
	if cmd.Type != CmdPipeline {
		t.Errorf("expected CmdPipeline, got %v", cmd.Type)
	}
}

func TestParse_PipelineCancel(t *testing.T) {
	cmd := Parse("pipeline cancel pipe-123")
	if cmd.Type != CmdPipeline {
		t.Errorf("expected CmdPipeline, got %v", cmd.Type)
	}
}

func TestParse_Configure(t *testing.T) {
	cmd := Parse("configure detection.prompt_debounce=5")
	if cmd.Type != CmdConfigure {
		t.Errorf("expected CmdConfigure, got %v", cmd.Type)
	}
}

func TestParse_ConfigureList(t *testing.T) {
	cmd := Parse("configure list")
	if cmd.Type != CmdConfigure {
		t.Errorf("expected CmdConfigure, got %v", cmd.Type)
	}
}

func TestParse_Copy(t *testing.T) {
	cmd := Parse("copy")
	if cmd.Type != CmdCopy {
		t.Errorf("expected CmdCopy, got %v", cmd.Type)
	}
}

func TestParse_CopyWithID(t *testing.T) {
	cmd := Parse("copy abc123")
	if cmd.Type != CmdCopy {
		t.Errorf("expected CmdCopy, got %v", cmd.Type)
	}
}

func TestParse_Prompt(t *testing.T) {
	cmd := Parse("prompt")
	if cmd.Type != CmdPrompt {
		t.Errorf("expected CmdPrompt, got %v", cmd.Type)
	}
}

func TestParse_Forget(t *testing.T) {
	cmd := Parse("forget 42")
	if cmd.Type != CmdForget {
		t.Errorf("expected CmdForget, got %v", cmd.Type)
	}
}

func TestParse_Stats(t *testing.T) {
	cmd := Parse("stats")
	if cmd.Type != CmdStats {
		t.Errorf("expected CmdStats, got %v", cmd.Type)
	}
}

func TestParse_Alerts(t *testing.T) {
	cmd := Parse("alerts")
	if cmd.Type != CmdAlerts {
		t.Errorf("expected CmdAlerts, got %v", cmd.Type)
	}
}

func TestParse_Version(t *testing.T) {
	cmd := Parse("version")
	if cmd.Type != CmdVersion {
		t.Errorf("expected CmdVersion, got %v", cmd.Type)
	}
}

func TestParse_Restart(t *testing.T) {
	cmd := Parse("restart")
	if cmd.Type != CmdRestart {
		t.Errorf("expected CmdRestart, got %v", cmd.Type)
	}
}

func TestParse_Schedule(t *testing.T) {
	cmd := Parse("schedule add abc123 10:00: run tests")
	if cmd.Type != CmdSchedule {
		t.Errorf("expected CmdSchedule, got %v", cmd.Type)
	}
}

func TestParse_UpdateCheck(t *testing.T) {
	cmd := Parse("update check")
	if cmd.Type != CmdUpdateCheck {
		t.Errorf("expected CmdUpdateCheck, got %v", cmd.Type)
	}
}
