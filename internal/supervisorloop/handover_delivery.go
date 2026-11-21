package supervisorloop

import (
	"fmt"
	"os"
	"path/filepath"
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
// names the file is returned instead.
func DeliverablePrompt(projectDir, scheduleName, handover string) (string, error) {
	if len(handover) <= MaxDeliveredPromptChars {
		return handover, nil
	}
	path := HandoverFilePath(projectDir, scheduleName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("handover file: %w", err)
	}
	if err := os.WriteFile(path, []byte(handover), 0o600); err != nil {
		return "", fmt.Errorf("handover file: %w", err)
	}
	return fmt.Sprintf("You are a datawatch supervisor on a recurring pass. Your handover (build context, rules, direction and this pass) is in %s. "+
		"Read that file in full first, then follow it. Keep the pass short. "+EndingInstruction, path), nil
}
