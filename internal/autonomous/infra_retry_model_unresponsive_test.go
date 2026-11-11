package autonomous

import (
	"errors"
	"strings"
	"testing"
)

func TestModelUnresponsiveRequeueUsesOwnBound(t *testing.T) {
	m, prd, task := infraRetryFixture(t, nil)
	task.ComputeNode = "gpu-a"
	err := errors.New("DATAWATCH_PI_FAILED: pi exited\nError: Request timed out.")

	plan, ok := m.planModelUnresponsiveRetry(m.infraRetryConfig(), task, err, "pi-prompt", "qwen-x")
	if !ok || plan.Class != InfraClassModelUnresponsive {
		t.Fatalf("plan=%+v ok=%v", plan, ok)
	}
	if !strings.Contains(plan.Reason, "gpu-a") || !strings.Contains(plan.Reason, "qwen-x") {
		t.Fatalf("reason lacks node/model: %q", plan.Reason)
	}
	m.recordInfraRetry(prd, task, plan)
	if task.InfraRetryCount != 0 || task.ModelUnresponsiveRetries != 1 {
		t.Fatalf("InfraRetryCount=%d ModelUnresponsiveRetries=%d", task.InfraRetryCount, task.ModelUnresponsiveRetries)
	}

	task.ModelUnresponsiveRetries = maxModelUnresponsiveRetries
	if _, ok := m.planModelUnresponsiveRetry(m.infraRetryConfig(), task, err, "", ""); ok {
		t.Fatal("retry planned past the bound")
	}
	clearInfraRetryCount(task)
	if task.ModelUnresponsiveRetries != 0 {
		t.Fatal("not cleared")
	}
}
