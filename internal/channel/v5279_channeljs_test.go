// v5.27.9 (BL212 follow-up, datawatch#29) — assert the embedded channel.js
// fallback exposes the same memory tool surface the Go bridge has. Storage
// testing instances were still hitting the JS path despite the Go bridge
// being primary; full parity is non-negotiable per the project rule.

package channel

import (
	"strings"
	"testing"
)

func TestChannelJS_ExposesMemoryTools(t *testing.T) {
	body := string(channelJS)
	wantTools := []string{
		"reply",
		"memory_remember",
		"memory_recall",
		"memory_list",
		"memory_forget",
		"memory_stats",
	}
	for _, name := range wantTools {
		needle := "name: '" + name + "'"
		if !strings.Contains(body, needle) {
			t.Errorf("channel.js missing tool registration %q (looking for %q)", name, needle)
		}
	}
}

func TestChannelJS_MemoryToolsForwardToParent(t *testing.T) {
	body := string(channelJS)
	wantPaths := []string{
		"/api/memory/save",
		"/api/memory/search?q=",
		"/api/memory/list?n=",
		"/api/memory/delete",
		"/api/memory/stats",
	}
	for _, p := range wantPaths {
		if !strings.Contains(body, p) {
			t.Errorf("channel.js missing forward to %q", p)
		}
	}
}

func TestChannelJS_HasCallParentHelper(t *testing.T) {
	// callParent (vs the legacy fire-and-forget postToDatawatch) is
	// what makes memory_* return parent JSON to the model. Drift
	// between the two helpers would silently break the contract.
	body := string(channelJS)
	if !strings.Contains(body, "function callParent(") {
		t.Errorf("channel.js missing callParent helper")
	}
	if !strings.Contains(body, "async function postToDatawatch(") {
		t.Errorf("channel.js missing postToDatawatch helper (still needed for reply/ready/permission)")
	}
}
