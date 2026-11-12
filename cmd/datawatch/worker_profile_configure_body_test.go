package main

import (
	"encoding/json"
	"testing"
)

// The worker profile tables go out as JSON objects so the daemon's PUT
// /api/config validation sees the real shape; text that is not an object goes
// out as a string and is refused there.
func TestConfigureRequestBody_WorkerProfileKeys(t *testing.T) {
	cases := []struct {
		name, key, value string
		wantObject       bool
		wantString       string
	}{
		{"profiles object", "autonomous.worker_profiles", `{"mine":{"name":"mine"}}`, true, ""},
		{"defaults object", "autonomous.worker_profile_defaults", `{"role":{"coder":"full"}}`, true, ""},
		{"profiles not json stays string", "autonomous.worker_profiles", "nope", false, "nope"},
		{"defaults array stays string", "autonomous.worker_profile_defaults", `[1,2]`, false, `[1,2]`},
		{"name stays string", "autonomous.worker_profile", "datawatch-tools", false, "datawatch-tools"},
		{"numeric name stays string", "autonomous.worker_profile", "123", false, "123"},
		{"empty name clears", "autonomous.worker_profile", "", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got map[string]any
			if err := json.Unmarshal([]byte(configureRequestBody(c.key, c.value)), &got); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			v, ok := got[c.key]
			if !ok {
				t.Fatalf("body lacks key %s: %v", c.key, got)
			}
			if c.wantObject {
				if _, isMap := v.(map[string]any); !isMap {
					t.Fatalf("value %v is not an object", v)
				}
				return
			}
			if s, isStr := v.(string); !isStr || s != c.wantString {
				t.Fatalf("value %#v, want string %q", v, c.wantString)
			}
		})
	}
}
