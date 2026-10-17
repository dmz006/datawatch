package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return name
}

func TestVerdictForAlreadyLanded(t *testing.T) {
	dir := t.TempDir()
	passing := writeScript(t, dir, "pass.sh", "#!/usr/bin/env bash\nexit 0\n")
	failing := writeScript(t, dir, "fail.sh", "#!/usr/bin/env bash\nexit 1\n")

	tests := []struct {
		name     string
		script   string
		wantOK   bool
		wantTrue bool
	}{
		{name: "empty script returns false", script: "", wantOK: false, wantTrue: false},
		{name: "whitespace script returns false", script: "   ", wantOK: false, wantTrue: false},
		{name: "passing script returns OK true", script: passing, wantOK: true, wantTrue: true},
		{name: "failing script returns false", script: failing, wantOK: false, wantTrue: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, gotTrue := verdictForAlreadyLanded(tt.script, dir)
			if gotTrue != tt.wantTrue {
				t.Fatalf("want true=%v, got %v (res %+v)", tt.wantTrue, gotTrue, res)
			}
			if res.OK != tt.wantOK {
				t.Fatalf("want OK=%v, got %v", tt.wantOK, res.OK)
			}
			if tt.wantTrue {
				if res.Severity != "low" || res.Summary == "" || res.VerifiedAt.IsZero() {
					t.Fatalf("unexpected result fields: %+v", res)
				}
			}
		})
	}
}
