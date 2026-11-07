package autonomous

import "testing"

func TestManagerArchiveRecordsActor(t *testing.T) {
	for _, actor := range []string{"supervisor-loop", "operator"} {
		m := newEmitManager(t, DefaultConfig(), nil)
		p := newEmitPRD(t, m, PRDCompleted)
		if _, err := m.Archive(p.ID, actor); err != nil {
			t.Fatalf("Archive(%q): %v", actor, err)
		}
		got, _ := m.Store().GetPRD(p.ID)
		d := lastDecision(got, "archive")
		if d == nil || d.Actor != actor {
			t.Fatalf("archive decision actor = %+v, want %q", d, actor)
		}
	}

	m := newEmitManager(t, DefaultConfig(), nil)
	p := newEmitPRD(t, m, PRDCompleted)
	if _, err := m.Archive(p.ID, ""); err == nil {
		t.Fatal("empty actor must return an error")
	}
	got, _ := m.Store().GetPRD(p.ID)
	if got.Status != PRDCompleted || lastDecision(got, "archive") != nil {
		t.Fatalf("record changed after refused archive: status=%q", got.Status)
	}
}
