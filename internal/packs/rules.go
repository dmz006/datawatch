package packs

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// PackTask is one standard task as checked by CheckTaskRules. It is the same
// type as Task; the alias names the rule-check entry point's view of a task.
type PackTask = Task

// Rule-check folders, relative to the pack folder and to a task's report.
const (
	packVerifyDir  = "verify/"
	packReportsDir = ".datawatch/reports/"
)

// shellMetacharacters are the characters a verification_script value may not
// contain. A script path is a plain file name, so any of these means the value
// is a command or an expression, not a file.
const shellMetacharacters = ";&|<>$`\\\"'(){}[]*?!#~"

// CheckTaskRules applies the rules every built-in standard task follows to one
// task of pack. It returns nil when the task passes, or an error that names the
// rule it broke.
//
//   - A task that is not read-only must name a verification_script. The script
//     must be one file path under verify/ of the pack folder: no whitespace,
//     no shell metacharacters, no path that leaves verify/ (including through a
//     symlink), and the file must exist, be a regular file and start with a
//     shebang line.
//   - A read-only task must list no files and must name a report_path under
//     .datawatch/reports/.
//
// The verification script is checked against pack.Dir, the folder the pack was
// loaded from; a pack without a Dir cannot be checked and is refused.
func CheckTaskRules(pack *Pack, task PackTask) error {
	if pack == nil {
		return fmt.Errorf("pack rule: no pack to check task %q against", task.Title)
	}
	if task.ReadOnly {
		return checkReadOnlyTask(task)
	}
	return checkVerifiedTask(pack, task)
}

// checkVerifiedTask applies the rules for a task that changes files.
func checkVerifiedTask(pack *Pack, task PackTask) error {
	script := task.VerificationScript
	if script == "" {
		return fmt.Errorf("pack rule: task %q is not read-only and must name a verification_script", task.Title)
	}
	const scriptRule = "pack rule: verification_script must be a script file under .datawatch/verify/"
	if strings.ContainsAny(script, " \t\r\n") || strings.ContainsAny(script, shellMetacharacters) {
		return fmt.Errorf("%s (task %q: %q holds whitespace or shell syntax)", scriptRule, task.Title, script)
	}
	if strings.HasPrefix(script, "/") || path.Clean(script) != script || !strings.HasPrefix(script, packVerifyDir) {
		return fmt.Errorf("%s (task %q: %q is not a clean path under verify/)", scriptRule, task.Title, script)
	}
	for _, seg := range strings.Split(script, "/") {
		if seg == ".." {
			return fmt.Errorf("%s (task %q: %q leaves verify/)", scriptRule, task.Title, script)
		}
	}
	if pack.Dir == "" {
		return fmt.Errorf("pack rule: verification_script for task %q cannot be checked: the pack has no folder", task.Title)
	}
	if err := checkScriptFile(pack.Dir, script); err != nil {
		return fmt.Errorf("%s (task %q: %v)", scriptRule, task.Title, err)
	}
	return nil
}

// checkScriptFile confirms that script exists in packDir as a regular file
// that resolves inside packDir/verify/ and starts with a shebang.
func checkScriptFile(packDir, script string) error {
	root, err := filepath.EvalSymlinks(filepath.Join(packDir, "verify"))
	if err != nil {
		return fmt.Errorf("verify folder: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(packDir, script))
	if err != nil {
		return fmt.Errorf("script %q: %v", script, err)
	}
	if !withinPackDir(root, resolved) {
		return fmt.Errorf("script %q resolves outside verify/", script)
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("script %q: %v", script, err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("script %q is not a regular file", script)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return fmt.Errorf("script %q: %v", script, err)
	}
	defer func() { _ = f.Close() }()
	head, _ := bufio.NewReader(f).Peek(2)
	if string(head) != "#!" {
		return fmt.Errorf("script %q has no shebang line", script)
	}
	return nil
}

// checkReadOnlyTask applies the rules for a read-only task.
func checkReadOnlyTask(task PackTask) error {
	if len(task.FileScopes) > 0 {
		return fmt.Errorf("pack rule: read-only task %q must list no files (file_scopes: %v)", task.Title, task.FileScopes)
	}
	rp := task.ReportPath
	if rp == "" {
		return fmt.Errorf("pack rule: read-only task %q must name a report_path under .datawatch/reports/", task.Title)
	}
	if strings.HasPrefix(rp, "/") || path.Clean(rp) != rp || !strings.HasPrefix(rp, packReportsDir) {
		return fmt.Errorf("pack rule: report_path must be a clean path under .datawatch/reports/ (task %q: %q)", task.Title, rp)
	}
	for _, seg := range strings.Split(rp, "/") {
		if seg == ".." {
			return fmt.Errorf("pack rule: report_path must be a clean path under .datawatch/reports/ (task %q: %q)", task.Title, rp)
		}
	}
	return nil
}

// withinPackDir reports whether p lies inside root (both already resolved).
func withinPackDir(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
