package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestResourceLimitsCLI_ConfigSetKeys: datawatch config set sends the three
// resource_limits keys to the daemon as whole numbers, and shows the daemon's
// refusal for a bad value.
func TestResourceLimitsCLI_ConfigSetKeys(t *testing.T) {
	for key, value := range map[string]string{
		"resource_limits.memory_share_percent": "40",
		"resource_limits.memory_floor_mb":      "1024",
		"resource_limits.memory_cap_mb":        "8192",
	} {
		var got map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.WriteHeader(http.StatusOK)
		}))
		prev := serverURL
		serverURL = srv.URL
		out, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, key, value)
		})
		serverURL = prev
		srv.Close()
		if err != nil {
			t.Errorf("%s: %v", key, err)
			continue
		}
		if want := map[string]any{key: want0(value)}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: daemon got %#v, want %#v", key, got, want)
		}
		if !strings.Contains(out, "Set "+key+" = "+value) {
			t.Errorf("%s: output = %q", key, out)
		}
	}
}

func want0(v string) float64 {
	n, _ := strconv.Atoi(v)
	return float64(n)
}
