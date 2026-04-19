// BL110 — `datawatch config set/get` helper tests.

package main

import "testing"

func TestLookupConfigKey_Simple(t *testing.T) {
	m := map[string]interface{}{"foo": "bar"}
	if got := lookupConfigKey(m, "foo"); got != "bar" {
		t.Errorf("got %v want bar", got)
	}
}

func TestLookupConfigKey_Nested(t *testing.T) {
	m := map[string]interface{}{
		"agents": map[string]interface{}{
			"image_tag": "v3.0.0",
		},
	}
	if got := lookupConfigKey(m, "agents.image_tag"); got != "v3.0.0" {
		t.Errorf("got %v want v3.0.0", got)
	}
}

func TestLookupConfigKey_DeepNested(t *testing.T) {
	m := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": 42,
			},
		},
	}
	if got := lookupConfigKey(m, "a.b.c"); got != 42 {
		t.Errorf("got %v want 42", got)
	}
}

func TestLookupConfigKey_Missing(t *testing.T) {
	m := map[string]interface{}{"foo": "bar"}
	if got := lookupConfigKey(m, "nope"); got != nil {
		t.Errorf("got %v want nil", got)
	}
	if got := lookupConfigKey(m, "foo.nope"); got != nil {
		t.Errorf("got %v want nil for non-map traversal", got)
	}
}
