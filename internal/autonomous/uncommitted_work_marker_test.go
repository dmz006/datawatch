package autonomous

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

const markerStatus = "?? DATAWATCH_COMPLETE\x00 M DATAWATCH_COMPLETE\x00 M internal/real.go\x00?? sub/DATAWATCH_COMPLETE\x00"

func TestDetectUncommittedWorkSkippingListsMarkers(t *testing.T) {
	files, skipped, err := DetectUncommittedWorkSkipping(context.Background(), t.TempDir(), uwStatusRunner(markerStatus))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"internal/real.go", "sub/DATAWATCH_COMPLETE"}
	if got := uwPaths(files); !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(skipped, []string{"DATAWATCH_COMPLETE"}) {
		t.Fatalf("skipped = %v", skipped)
	}
}

func TestDetectUncommittedWorkDropsMarker(t *testing.T) {
	files, err := DetectUncommittedWork(context.Background(), t.TempDir(), uwStatusRunner(markerStatus))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == "DATAWATCH_COMPLETE" {
			t.Fatalf("marker returned: %+v", files)
		}
	}
}

func TestDetectUncommittedWorkOnlyMarkerYieldsNothing(t *testing.T) {
	files, skipped, err := DetectUncommittedWorkSkipping(context.Background(), t.TempDir(), uwStatusRunner("?? DATAWATCH_COMPLETE\x00"))
	if err != nil || len(files) != 0 || len(skipped) != 1 {
		t.Fatalf("files=%v skipped=%v err=%v", files, skipped, err)
	}
}

func TestSkippedMarkerFilesNote(t *testing.T) {
	if got := skippedMarkerFilesNote(nil); got != "" {
		t.Fatalf("nil note = %q", got)
	}
	got := skippedMarkerFilesNote([]string{"DATAWATCH_COMPLETE"})
	if !strings.Contains(got, "DATAWATCH_COMPLETE") || !strings.Contains(got, "Automaton") {
		t.Fatalf("note = %q", got)
	}
	multi := skippedMarkerFilesNote([]string{"b", "a"})
	if !strings.Contains(multi, "a, b") {
		t.Fatalf("multi note not sorted: %q", multi)
	}
}
