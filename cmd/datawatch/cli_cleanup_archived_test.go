// `datawatch config set` and comm `configure` send the cleanup_archived switch
// as a boolean.

package main

import (
	"encoding/json"
	"testing"
)

func TestCleanupArchivedParity_CommConfigureBody(t *testing.T) {
	var body map[string]any
	if err := json.Unmarshal([]byte(configureRequestBody("autonomous.automation.cleanup_archived", "false")), &body); err != nil {
		t.Fatal(err)
	}
	if v, ok := body["autonomous.automation.cleanup_archived"].(bool); !ok || v {
		t.Fatalf("body = %v", body)
	}
}
