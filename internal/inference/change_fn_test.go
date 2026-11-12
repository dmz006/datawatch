package inference

import "testing"

// The change callback runs after Add, Update and Delete succeed, can read the
// registry (no lock held), and does not run for a failed call.
func TestRegistryChangeFnRunsAfterSuccessfulEdits(t *testing.T) {
	r := NewRegistry()
	var seen []int
	r.SetChangeFn(func() { seen = append(seen, len(r.List())) })

	l := &LLM{Name: "one", Kind: "ollama", ComputeNodes: []string{"n1"}}
	if err := r.Add(l); err != nil {
		t.Fatal(err)
	}
	l.ComputeNodes = []string{"n1", "n2"}
	if err := r.Update(l); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(&LLM{Name: "one", Kind: "ollama"}); err == nil {
		t.Fatal("duplicate Add succeeded")
	}
	if err := r.Delete("one"); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete("one"); err == nil {
		t.Fatal("second Delete succeeded")
	}
	if want := []int{1, 1, 0}; len(seen) != 3 || seen[0] != want[0] || seen[1] != want[1] || seen[2] != want[2] {
		t.Fatalf("callback saw %v, want %v", seen, want)
	}

	r.SetChangeFn(nil)
	if err := r.Add(&LLM{Name: "two", Kind: "ollama"}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 {
		t.Fatalf("cleared callback still ran: %v", seen)
	}
}
