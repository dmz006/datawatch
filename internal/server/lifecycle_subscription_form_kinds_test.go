package server

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// The PWA subscription form embeds the closed event-kind set; this keeps it
// in step with autonomous.KnownEventKinds so a new status kind cannot be
// accepted by the server yet be missing from (or stale in) the form.
func TestLifecycleSubscriptionForm_KindListMatchesKnownEventKinds(t *testing.T) {
	src, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)const _LIFECYCLE_SUB_KIND_GROUPS = \[(.*?)\n\];`).FindSubmatch(src)
	if block == nil {
		t.Fatal("_LIFECYCLE_SUB_KIND_GROUPS not found in web/app.js")
	}
	got := map[string]bool{}
	// Kinds are prd.<status>, task.<status> or a bare kind (opt-in
	// warning, session, question); the group label keys (lifecycle_sub_form_*)
	// share the quoted shape and are skipped.
	for _, m := range regexp.MustCompile(`'((?:(?:prd|task|question)\.)?[a-z_]+)'`).FindAllSubmatch(block[1], -1) {
		if strings.HasPrefix(string(m[1]), "lifecycle_sub_form_") {
			continue
		}
		got[string(m[1])] = true
	}
	var missing, extra []string
	for k := range autonomous.KnownEventKinds {
		if !got[k] {
			missing = append(missing, k)
		}
	}
	for k := range got {
		if !autonomous.KnownEventKinds[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("form kinds out of sync with KnownEventKinds: missing %v, unknown %v", missing, extra)
	}
}

// The PWA sink picker embeds the closed sink-type set. This keeps it in step
// with the sink constants the daemon accepts, and gives every sink a label, so
// a new sink (or the webhook and peer sinks) cannot be accepted by the server
// yet be missing from the picker.
func TestLifecycleSubscriptionForm_SinkListMatchesSinkTypes(t *testing.T) {
	src, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	list := regexp.MustCompile(`const _LIFECYCLE_SUB_SINKS = \[([^\]]*)\];`).FindSubmatch(src)
	if list == nil {
		t.Fatal("_LIFECYCLE_SUB_SINKS not found in web/app.js")
	}
	got := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllSubmatch(list[1], -1) {
		got[string(m[1])] = true
	}
	want := map[string]bool{
		autonomous.SinkComm:     true,
		autonomous.SinkSession:  true,
		autonomous.SinkTopic:    true,
		autonomous.SinkAlert:    true,
		autonomous.SinkMCPQueue: true,
		autonomous.SinkWebhook:  true,
		autonomous.SinkPeer:     true,
	}
	var missing, extra []string
	for k := range want {
		if !got[k] {
			missing = append(missing, k)
		}
	}
	for k := range got {
		if !want[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("sink picker out of sync with the sink types: missing %v, unknown %v", missing, extra)
	}

	labels := regexp.MustCompile(`(?s)const _LIFECYCLE_SUB_SINK_LABELS = \{(.*?)\n\};`).FindSubmatch(src)
	if labels == nil {
		t.Fatal("_LIFECYCLE_SUB_SINK_LABELS not found in web/app.js")
	}
	for sink := range want {
		if !strings.Contains(string(labels[1]), sink+": 'lifecycle_sub_form_sink_") {
			t.Errorf("sink %q has no picker label key", sink)
		}
	}
}
