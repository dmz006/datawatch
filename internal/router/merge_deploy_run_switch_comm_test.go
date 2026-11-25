package router

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestDeployComm_RunRefusalNamesTheSetting(t *testing.T) {
	_, _, got, _ := deployCommSay(t, 403, config.MergeDeployRunRefusal+"\n", "autonomous deploy run")
	if !strings.Contains(got, "autonomous deploy failed") || !strings.Contains(got, config.MergeDeployRunRefusal) {
		t.Errorf("reply %q, want the refusal", got)
	}
}
