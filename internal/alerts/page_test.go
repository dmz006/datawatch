package alerts

import (
	"errors"
	"testing"
)

func ids(list []*Alert) []string {
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.ID
	}
	return out
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPage_EmptyStore(t *testing.T) {
	s := tempStore(t)
	items, more, err := s.Page(PageOptions{Limit: 10})
	if err != nil || more || len(items) != 0 || items == nil {
		t.Fatalf("got items=%v more=%v err=%v; want empty non-nil, false, nil", items, more, err)
	}
	if s.Count() != 0 {
		t.Fatalf("Count = %d, want 0", s.Count())
	}
}

func TestPage_UnlimitedMatchesListOrdering(t *testing.T) {
	s := tempStore(t)
	for i := 0; i < 5; i++ {
		s.Add(LevelInfo, "a", "", "")
	}
	items, more, err := s.Page(PageOptions{Limit: -1})
	if err != nil || more {
		t.Fatalf("more=%v err=%v", more, err)
	}
	if !equalIDs(ids(items), ids(s.List())) {
		t.Fatalf("Page order %v != List order %v", ids(items), ids(s.List()))
	}
	if s.Count() != 5 {
		t.Fatalf("Count = %d, want 5", s.Count())
	}
}

func TestPage_LimitAndCursorWalk(t *testing.T) {
	s := tempStore(t)
	for i := 0; i < 5; i++ {
		s.Add(LevelInfo, "a", "", "")
	}
	want := ids(s.List())

	p1, more, err := s.Page(PageOptions{Limit: 2})
	if err != nil || !more || !equalIDs(ids(p1), want[0:2]) {
		t.Fatalf("page1 = %v more=%v err=%v", ids(p1), more, err)
	}
	p2, more, err := s.Page(PageOptions{Limit: 2, BeforeID: p1[1].ID})
	if err != nil || !more || !equalIDs(ids(p2), want[2:4]) {
		t.Fatalf("page2 = %v more=%v err=%v", ids(p2), more, err)
	}
	p3, more, err := s.Page(PageOptions{Limit: 2, BeforeID: p2[1].ID})
	if err != nil || more || !equalIDs(ids(p3), want[4:5]) {
		t.Fatalf("page3 = %v more=%v err=%v", ids(p3), more, err)
	}
	// Exactly-full final page reports no more.
	p, more, _ := s.Page(PageOptions{Limit: 1, BeforeID: want[3]})
	if more || len(p) != 1 {
		t.Fatalf("exact page more=%v len=%d", more, len(p))
	}
}

func TestPage_LimitZeroCountsOnly(t *testing.T) {
	s := tempStore(t)
	s.Add(LevelInfo, "a", "", "")
	items, more, err := s.Page(PageOptions{Limit: 0})
	if err != nil || len(items) != 0 || !more {
		t.Fatalf("items=%d more=%v err=%v", len(items), more, err)
	}
}

func TestPage_UnreadOnly(t *testing.T) {
	s := tempStore(t)
	a1 := s.Add(LevelInfo, "1", "", "")
	a2 := s.Add(LevelInfo, "2", "", "")
	a3 := s.Add(LevelInfo, "3", "", "")
	if err := s.MarkRead(a2.ID); err != nil {
		t.Fatal(err)
	}
	items, more, err := s.Page(PageOptions{Limit: -1, UnreadOnly: true})
	if err != nil || more || !equalIDs(ids(items), []string{a3.ID, a1.ID}) {
		t.Fatalf("got %v more=%v err=%v", ids(items), more, err)
	}
}

func TestPage_SourceAndSessionFilter(t *testing.T) {
	s := tempStore(t)
	s.Add(LevelInfo, "sess", "", "s1")
	sys := s.AddSystem(LevelWarn, "sys", "")
	s.Add(LevelInfo, "sess2", "", "s2")

	items, _, _ := s.Page(PageOptions{Limit: -1, Source: "system"})
	if !equalIDs(ids(items), []string{sys.ID}) {
		t.Fatalf("source filter got %v", ids(items))
	}
	items, _, _ = s.Page(PageOptions{Limit: -1, SessionID: "s1"})
	if len(items) != 1 || items[0].SessionID != "s1" {
		t.Fatalf("session filter got %v", ids(items))
	}
	if s.CountBySource("system") != 1 {
		t.Fatalf("CountBySource = %d", s.CountBySource("system"))
	}
}

func TestPage_UnknownCursor(t *testing.T) {
	s := tempStore(t)
	s.Add(LevelInfo, "a", "", "")
	_, _, err := s.Page(PageOptions{Limit: 5, BeforeID: "nope"})
	if !errors.Is(err, ErrUnknownCursor) {
		t.Fatalf("err = %v, want ErrUnknownCursor", err)
	}
}

func TestPage_ReturnsCopies(t *testing.T) {
	s := tempStore(t)
	s.Add(LevelInfo, "a", "", "")
	items, _, _ := s.Page(PageOptions{Limit: -1})
	items[0].Read = true
	if s.UnreadCount() != 1 {
		t.Fatal("Page leaked a pointer into the store")
	}
}
