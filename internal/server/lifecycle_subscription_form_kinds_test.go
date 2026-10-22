package server

import (
	"os"
	"regexp"
	"sort"
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
	for _, m := range regexp.MustCompile(`'((?:prd|task)\.[a-z_]+)'`).FindAllSubmatch(block[1], -1) {
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
