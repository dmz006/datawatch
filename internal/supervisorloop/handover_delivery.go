package supervisorloop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxDeliveredPromptChars is the bound on the prompt typed into a supervisor
// session. A larger handover goes to a file and the prompt names the file.
const MaxDeliveredPromptChars = 8000

// HandoverFilePath is where a schedule's handover file lives: under the
// project's .datawatch folder, one file per schedule.
func HandoverFilePath(projectDir, scheduleName string) string {
	return filepath.Join(projectDir, ".datawatch", "supervisor", scheduleName+"-handover.md")
}

// DeliverablePrompt returns the prompt to deliver for a rendered handover.
// A handover that fits MaxDeliveredPromptChars is delivered inline. A larger
// one is (re)written to HandoverFilePath and a short bootstrap prompt that
// names the file is returned instead. The file is written through a root on the
// project folder, so neither the schedule name nor a link inside the project
// can put it outside the project.
func DeliverablePrompt(projectDir, scheduleName, handover string) (string, error) {
	if len(handover) <= MaxDeliveredPromptChars {
		return handover, nil
	}
	path := HandoverFilePath(projectDir, scheduleName)
	if err := writeHandover(projectDir, scheduleName, []byte(handover)); err != nil {
		return "", fmt.Errorf("handover file: %w", err)
	}
	return fmt.Sprintf("You are a datawatch supervisor on a recurring pass. Your handover (build context, rules, direction and this pass) is in %s. "+
		"Read that file in full first, then follow it. Keep the pass short. "+EndingInstruction, path), nil
}

// writeHandover writes one schedule's handover file inside projectDir.
func writeHandover(projectDir, scheduleName string, body []byte) error {
	if scheduleName == "" || strings.ContainsRune(scheduleName, 0) {
		return fmt.Errorf("schedule name %q cannot name a file", scheduleName)
	}
	for _, part := range strings.Split(filepath.ToSlash(scheduleName), "/") {
		if part == ".." {
			return fmt.Errorf("schedule name %q must not contain a '..' path segment", scheduleName)
		}
	}
	rel, err := filepath.Rel(projectDir, HandoverFilePath(projectDir, scheduleName))
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(projectDir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.MkdirAll(filepath.Dir(rel), 0o700); err != nil {
		return err
	}
	return root.WriteFile(rel, body, 0o600)
}
