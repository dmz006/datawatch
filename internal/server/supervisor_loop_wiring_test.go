package server

import (
	"context"
	"errors"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func TestSupervisorLoopWiringNilIsNotConfigured(t *testing.T) {
	h := NewSupervisorLoopHooks(nil, nil, nil)
	if h.Deploy != nil || h.Lanes != nil || h.MergeUntilConflict != nil {
		t.Errorf("nil manager must leave hooks unset: %+v", h)
	}
}

func TestSupervisorLoopWiringNoBaseRepoNotConfigured(t *testing.T) {
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Skipf("manager: %v", err)
	}
	if _, err := supervisorLoopDeploy(context.Background(), mgr, t.TempDir(), config.MergePipelineConfig{}); !errors.Is(err, autonomous.ErrSupervisorStepNotConfigured) {
		t.Errorf("deploy with no base repo: %v", err)
	}
	if _, err := supervisorLoopMerge(context.Background(), mgr, t.TempDir(), config.MergePipelineConfig{}); !errors.Is(err, autonomous.ErrSupervisorStepNotConfigured) {
		t.Errorf("merge with no base repo: %v", err)
	}
}
