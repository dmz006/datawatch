package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// TestAttemptRestoreRunsAfterDirtyCapture proves the spawn-time order: the
// pre-task capture runs first, then the restore of the last attempt ref.
func TestAttemptRestoreRunsAfterDirtyCapture(t *testing.T) {
	var calls []string
	capture := func() { calls = append(calls, "capture") }
	restore := func(ctx context.Context, dir, ref string) error {
		calls = append(calls, "restore:"+dir+":"+ref)
		return nil
	}

	restoreAttemptWorkBeforeSpawn(context.Background(), "/proj",
		[]string{"refs/datawatch/attempts/t/1", "refs/datawatch/attempts/t/2"}, true, capture, restore)

	want := []string{"capture", "restore:/proj:refs/datawatch/attempts/t/2"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
}

func TestAttemptRestoreSkippedWhenDisabledOrNoRefs(t *testing.T) {
	cases := []struct {
		name    string
		dir     string
		refs    []string
		enabled bool
	}{
		{"disabled", "/proj", []string{"refs/datawatch/attempts/t/1"}, false},
		{"no refs", "/proj", nil, true},
		{"no dir", "", []string{"refs/datawatch/attempts/t/1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			capture := func() { calls = append(calls, "capture") }
			restore := func(ctx context.Context, dir, ref string) error {
				calls = append(calls, "restore")
				return nil
			}
			restoreAttemptWorkBeforeSpawn(context.Background(), tc.dir, tc.refs, tc.enabled, capture, restore)
			if !reflect.DeepEqual(calls, []string{"capture"}) {
				t.Fatalf("calls = %v, want only capture", calls)
			}
		})
	}
}

// TestAttemptRestoreErrorDoesNotBlockSpawn checks a failed restore is absorbed:
// the helper returns normally and capture has already run.
func TestAttemptRestoreErrorDoesNotBlockSpawn(t *testing.T) {
	captured := false
	capture := func() { captured = true }
	restore := func(ctx context.Context, dir, ref string) error {
		return errors.New("boom")
	}
	restoreAttemptWorkBeforeSpawn(context.Background(), "/proj",
		[]string{"refs/datawatch/attempts/t/1"}, true, capture, restore)
	if !captured {
		t.Fatal("capture must run even when restore fails")
	}
}
