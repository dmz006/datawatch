package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rulesPack writes a pack folder holding the given files (relative path →
// content) and returns the pack as LoadPackFolder would describe it.
func rulesPack(t *testing.T, files map[string]string) *Pack {
	t.Helper()
	dir := writePackDir(t, "name: rules-pack\nversion: 1.0.0\n", files)
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &Pack{Name: "rules-pack", Version: "1.0.0", Dir: abs}
}

const shellScript = "#!/bin/sh\nexit 0\n"

func TestCheckTaskRulesVerifiedTaskPasses(t *testing.T) {
	p := rulesPack(t, map[string]string{"verify/review.sh": shellScript})
	task := PackTask{Title: "Review", VerificationScript: "verify/review.sh", FileScopes: []string{"internal/**"}}
	if err := CheckTaskRules(p, task); err != nil {
		t.Fatalf("valid verified task: unexpected error: %v", err)
	}
}

func TestCheckTaskRulesVerifiedTaskNeedsScript(t *testing.T) {
	p := rulesPack(t, nil)
	err := CheckTaskRules(p, PackTask{Title: "Docs"})
	if err == nil {
		t.Fatal("non-read-only task with no verification_script: expected error")
	}
	if !strings.Contains(err.Error(), "must name a verification_script") {
		t.Errorf("error = %q, want the verification_script rule named", err)
	}
}

func TestCheckTaskRulesScriptValueRefused(t *testing.T) {
	p := rulesPack(t, map[string]string{
		"verify/ok.sh":      shellScript,
		"verify/my tool.sh": shellScript,
		"verify/x.sh;rm":    shellScript,
	})
	cases := map[string]string{
		"whitespace":       "verify/my tool.sh",
		"command string":   "go build ./... && go test ./...",
		"semicolon":        "verify/x.sh;rm",
		"dollar expansion": "verify/$HOME.sh",
		"pipe":             "verify/ok.sh|cat",
		"not under verify": "scripts/ok.sh",
		"dotdot escape":    "verify/../ok.sh",
		"absolute":         "/etc/passwd",
		"unclean":          "verify//ok.sh",
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			err := CheckTaskRules(p, PackTask{Title: "T", VerificationScript: script})
			if err == nil {
				t.Fatalf("script %q: expected refusal", script)
			}
			if !strings.Contains(err.Error(), "pack rule: verification_script must be a script file under .datawatch/verify/") {
				t.Errorf("error = %q, want the script-file rule named", err)
			}
		})
	}
}

func TestCheckTaskRulesScriptFileChecks(t *testing.T) {
	p := rulesPack(t, map[string]string{
		"verify/ok.sh":    shellScript,
		"verify/noshe.sh": "echo no shebang\n",
		"verify/dir.d/x":  "unused",
	})
	// Symlink under verify/ pointing outside it.
	outside := filepath.Join(t.TempDir(), "outside.sh")
	if err := os.WriteFile(outside, []byte(shellScript), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(p.Dir, "verify", "link.sh")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, script, want string
	}{
		{"missing file", "verify/absent.sh", "no such file"},
		{"no shebang", "verify/noshe.sh", "has no shebang line"},
		{"directory", "verify/dir.d", "is not a regular file"},
		{"symlink escape", "verify/link.sh", "resolves outside verify/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckTaskRules(p, PackTask{Title: "T", VerificationScript: tc.script})
			if err == nil {
				t.Fatalf("script %q: expected refusal", tc.script)
			}
			if !strings.Contains(err.Error(), "pack rule: verification_script must be a script file under .datawatch/verify/") {
				t.Errorf("error = %q, want the script-file rule named", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}

	if err := CheckTaskRules(p, PackTask{Title: "T", VerificationScript: "verify/ok.sh"}); err != nil {
		t.Errorf("valid script: unexpected error: %v", err)
	}
}

func TestCheckTaskRulesPackWithoutFolder(t *testing.T) {
	err := CheckTaskRules(&Pack{Name: "p", Version: "1.0.0"}, PackTask{Title: "T", VerificationScript: "verify/ok.sh"})
	if err == nil || !strings.Contains(err.Error(), "the pack has no folder") {
		t.Errorf("pack without folder: error = %v, want it to say the folder is missing", err)
	}
}

func TestCheckTaskRulesNilPack(t *testing.T) {
	if err := CheckTaskRules(nil, PackTask{Title: "T", ReadOnly: true, ReportPath: ".datawatch/reports/r.md"}); err == nil {
		t.Error("nil pack: expected error")
	}
}

func TestCheckTaskRulesReadOnlyPasses(t *testing.T) {
	p := rulesPack(t, nil)
	task := PackTask{Title: "Review", ReadOnly: true, ReportPath: ".datawatch/reports/review.md"}
	if err := CheckTaskRules(p, task); err != nil {
		t.Fatalf("valid read-only task: unexpected error: %v", err)
	}
}

func TestCheckTaskRulesReadOnlyListsFiles(t *testing.T) {
	p := rulesPack(t, nil)
	task := PackTask{Title: "Review", ReadOnly: true, ReportPath: ".datawatch/reports/r.md", FileScopes: []string{"internal/**"}}
	err := CheckTaskRules(p, task)
	if err == nil || !strings.Contains(err.Error(), "pack rule: read-only task \"Review\" must list no files") {
		t.Errorf("read-only task with files: error = %v, want the no-files rule named", err)
	}
}

func TestCheckTaskRulesReadOnlyReportPath(t *testing.T) {
	p := rulesPack(t, nil)
	cases := map[string]struct {
		report string
		want   string
	}{
		"missing report_path": {"", "must name a report_path under .datawatch/reports/"},
		"outside reports":     {"docs/review.md", "report_path must be a clean path under .datawatch/reports/"},
		"absolute":            {"/tmp/r.md", "report_path must be a clean path under .datawatch/reports/"},
		"dotdot escape":       {".datawatch/reports/../../secret.md", "report_path must be a clean path under .datawatch/reports/"},
		"unclean":             {".datawatch/reports//r.md", "report_path must be a clean path under .datawatch/reports/"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := CheckTaskRules(p, PackTask{Title: "Review", ReadOnly: true, ReportPath: tc.report})
			if err == nil {
				t.Fatalf("report %q: expected refusal", tc.report)
			}
			if !strings.Contains(err.Error(), "pack rule:") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}
