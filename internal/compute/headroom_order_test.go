package compute

import (
	"reflect"
	"testing"
)

// TestSortSmallestFirst_SizeThenName pins the order Measure's allow-list needs:
// the smallest model first by tag size, models of unknown size last by name.
func TestSortSmallestFirst_SizeThenName(t *testing.T) {
	in := []string{"qwen:30b", "qwen:4b", "llama:8b", "mystery"}
	got := SortSmallestFirst(in)
	want := []string{"qwen:4b", "llama:8b", "qwen:30b", "mystery"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SortSmallestFirst = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(in, []string{"qwen:30b", "qwen:4b", "llama:8b", "mystery"}) {
		t.Fatalf("input slice was modified: %v", in)
	}
}

// TestSortSmallestFirst_EmptyAndSingle covers the edges Measure relies on: an
// empty list stays empty, and the one name is returned as is.
func TestSortSmallestFirst_EmptyAndSingle(t *testing.T) {
	if got := SortSmallestFirst(nil); len(got) != 0 {
		t.Fatalf("empty input returned %v", got)
	}
	if got := SortSmallestFirst([]string{"qwen:4b"}); !reflect.DeepEqual(got, []string{"qwen:4b"}) {
		t.Fatalf("single input returned %v", got)
	}
}
