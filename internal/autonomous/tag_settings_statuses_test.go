package autonomous

import (
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestTagSettingsStatusesMatchPRDStatuses keeps the Automaton status list the
// tag settings validate against in step with the status constants here. A new
// status must be added to both, or the attention list would refuse it.
func TestTagSettingsStatusesMatchPRDStatuses(t *testing.T) {
	want := []PRDStatus{
		PRDDraft, PRDPlanning, PRDNeedsReview, PRDApproved, PRDRevisionsAsked,
		PRDRunning, PRDCompleted, PRDRejected, PRDCancelled, PRDArchived,
		PRDBlocked, PRDFailed, PRDPaused,
	}
	if len(want) != len(config.TagAutomatonStatuses) {
		t.Fatalf("config lists %d Automaton statuses, autonomous defines %d", len(config.TagAutomatonStatuses), len(want))
	}
	for i, s := range want {
		if config.TagAutomatonStatuses[i] != string(s) {
			t.Fatalf("status %d: config has %q, autonomous has %q", i, config.TagAutomatonStatuses[i], s)
		}
	}
}

// TestTagSettingsDefaultAttentionStatusesMatch checks the attention default
// the config block sets is the one the attention group resolves to.
func TestTagSettingsDefaultAttentionStatusesMatch(t *testing.T) {
	if !reflect.DeepEqual(DefaultAttentionStatuses, config.DefaultTagAttentionStatuses) {
		t.Fatalf("autonomous default attention statuses %v differ from config's %v",
			DefaultAttentionStatuses, config.DefaultTagAttentionStatuses)
	}
}
