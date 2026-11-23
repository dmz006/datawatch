package audit

import (
	"testing"
	"time"
)

func TestPackNameOf(t *testing.T) {
	cases := []struct {
		name string
		e    Entry
		want string
	}{
		{"no details", Entry{Action: "x"}, ""},
		{"no pack_name key", Entry{Details: map[string]any{"other": "v"}}, ""},
		{"non-string value", Entry{Details: map[string]any{PackNameField: 42}}, ""},
		{"plain name", Entry{Details: map[string]any{PackNameField: "standard-tasks"}}, "standard-tasks"},
		{"surrounding space trimmed", Entry{Details: map[string]any{PackNameField: "  alpha \t"}}, "alpha"},
		{"blank name is empty", Entry{Details: map[string]any{PackNameField: "   "}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PackNameOf(c.e); got != c.want {
				t.Errorf("PackNameOf = %q, want %q", got, c.want)
			}
		})
	}
}

func TestPackNameFieldIsPackNameKey(t *testing.T) {
	if PackNameField != "pack_name" {
		t.Errorf("PackNameField = %q, want pack_name", PackNameField)
	}
}

// seedPackLog writes entries that carry pack attribution in Details, plus
// one unattributed entry, oldest first.
func seedPackLog(t *testing.T) *Log {
	t.Helper()
	l, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for i, e := range []Entry{
		{Actor: "daemon", Action: "task_start", Subject: "t1", Details: map[string]any{PackNameField: "alpha"}},
		{Actor: "daemon", Action: "task_start", Subject: "t2", Details: map[string]any{PackNameField: "beta"}},
		{Actor: "operator", Action: "configure", Subject: "cfg"},
		{Actor: "daemon", Action: "task_done", Subject: "t1", Details: map[string]any{PackNameField: "alpha"}},
		{Actor: "daemon", Action: "task_done", Subject: "t2", Details: map[string]any{PackNameField: " beta "}},
	} {
		e.Timestamp = base.Add(time.Duration(i) * time.Minute)
		if err := l.Write(e); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func TestAuditQuery_PackNameFilter(t *testing.T) {
	l := seedPackLog(t)
	cases := []struct {
		name   string
		filter QueryFilter
		want   []string // subjects, newest first
	}{
		{"no pack filter returns all", QueryFilter{}, []string{"t2", "t1", "cfg", "t2", "t1"}},
		{"alpha only", QueryFilter{PackName: "alpha"}, []string{"t1", "t1"}},
		{"beta matches trimmed stored name", QueryFilter{PackName: "beta"}, []string{"t2", "t2"}},
		{"unknown pack matches nothing", QueryFilter{PackName: "gamma"}, nil},
		{"pack is exact, not a prefix", QueryFilter{PackName: "alp"}, nil},
		{"pack combined with action", QueryFilter{PackName: "beta", Action: "task_done"}, []string{"t2"}},
		{"pack combined with subject", QueryFilter{PackName: "alpha", Subject: "t2"}, nil},
		{"pack combined with limit", QueryFilter{PackName: "alpha", Limit: 1}, []string{"t1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := l.Read(c.filter)
			if err != nil {
				t.Fatal(err)
			}
			subs := make([]string, len(got))
			for i, e := range got {
				subs[i] = e.Subject
			}
			if !sameStrings(subs, c.want) {
				t.Errorf("subjects = %v, want %v", subs, c.want)
			}
		})
	}
}

func TestParseQuery_PackName(t *testing.T) {
	vals := map[string]string{"pack_name": "alpha"}
	f, err := ParseQuery(func(n string) string { return vals[n] })
	if err != nil {
		t.Fatal(err)
	}
	if f.PackName != "alpha" {
		t.Errorf("PackName = %q, want alpha", f.PackName)
	}
	f, err = ParseQuery(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if f.PackName != "" {
		t.Errorf("unset PackName = %q, want empty", f.PackName)
	}
}

func TestQueryParamNames_IncludesPackName(t *testing.T) {
	for _, n := range QueryParamNames {
		if n == "pack_name" {
			return
		}
	}
	t.Errorf("QueryParamNames %v missing pack_name", QueryParamNames)
}
