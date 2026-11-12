package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestAutonomousConfigSet_UndeclaredEditMode(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{}`))
	})
	for _, want := range []string{"off", "warn", "block"} {
		got = nil
		if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"undeclared_edit_mode": want})); err != nil {
			t.Fatal(err)
		}
		if v, ok := got["undeclared_edit_mode"]; !ok || v != want {
			t.Fatalf("undeclared_edit_mode=%q not forwarded: %v", want, got)
		}
	}
	// Not mentioning the field at all must not add it to the body.
	got = nil
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"capacity_enabled": true})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["undeclared_edit_mode"]; ok {
		t.Fatalf("undeclared_edit_mode present when the caller never mentioned it: %v", got)
	}
}
