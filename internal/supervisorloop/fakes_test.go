package supervisorloop

import (
	"context"
	"errors"
	"time"
)

type fakeClient struct {
	replies []string
	reqs    []Request
}

func (f *fakeClient) Ask(_ context.Context, r Request) (string, error) {
	f.reqs = append(f.reqs, r)
	if len(f.replies) == 0 {
		return "", errors.New("no reply")
	}
	out := f.replies[0]
	f.replies = f.replies[1:]
	return out, nil
}

type fakeEscalator struct{ got []Escalation }

func (f *fakeEscalator) Escalate(_ context.Context, e Escalation) error {
	f.got = append(f.got, e)
	return nil
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { c.t = c.t.Add(time.Second); return c.t }

type memStore struct{ recs []RunRecord }

func (m *memStore) Append(r RunRecord) error   { m.recs = append(m.recs, r); return nil }
func (m *memStore) List() ([]RunRecord, error) { return m.recs, nil }

func okOnly(s string) error {
	if s != "good" {
		return errors.New("not good: " + s)
	}
	return nil
}

func newEngine(c *fakeClient) (*Engine, *fakeEscalator, *memStore) {
	esc, st := &fakeEscalator{}, &memStore{}
	return &Engine{Settings: DefaultSettings(), Client: c, Escalator: esc, Clock: &fakeClock{}, Store: st}, esc, st
}
