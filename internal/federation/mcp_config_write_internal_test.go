package federation

import "testing"

// TestConfigWriteLists_Disjoint pins the white-box invariant the external
// classification test can only observe per name: the two explicit lists that
// back IsConfigWriteTool / IsNonConfigWriteTool (BL412 option C) never share
// a tool — every tool sits on exactly one side.
func TestConfigWriteLists_Disjoint(t *testing.T) {
	for name := range configWriteTools {
		if nonConfigWriteTools[name] {
			t.Errorf("%q sits in both configWriteTools and nonConfigWriteTools — it must be on exactly one side", name)
		}
	}
	total := 0
	for name := range configWriteTools {
		if !configWriteTools[name] {
			t.Errorf("configWriteTools[%q] = false, want true (explicit sets must be explicit)", name)
		}
		total++
	}
	for name := range nonConfigWriteTools {
		if !nonConfigWriteTools[name] {
			t.Errorf("nonConfigWriteTools[%q] = false, want true (explicit sets must be explicit)", name)
		}
	}
	if total < 30 {
		t.Errorf("configWriteTools has %d tools, want a substantive list (30+)", total)
	}
}
