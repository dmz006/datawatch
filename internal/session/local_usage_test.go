package session

import (
	"testing"

	"github.com/dmz006/datawatch/internal/spendledger"
)

type fakeLocalReader struct{ u LocalUsage }

func (f fakeLocalReader) ReadLocalUsage(string, string) LocalUsage { return f.u }

type captureLedger struct{ got []spendledger.Entry }

func (c *captureLedger) Upsert(e spendledger.Entry) error { c.got = append(c.got, e); return nil }

func localLedgerRun(t *testing.T, family string, tokIn int, r LocalUsageReader) spendledger.Entry {
	t.Helper()
	m := newTestManager(t)
	cl := &captureLedger{}
	m.SetSpendLedger(cl)
	if r != nil {
		m.SetLocalUsageReader(r)
	}
	s := newTestSession(t, m, "lu1")
	s.BackendFamily = family
	s.TokensIn = tokIn
	m.upsertLedger(s)
	if len(cl.got) == 0 {
		t.Fatal("no entry written")
	}
	return cl.got[len(cl.got)-1]
}

func TestLocalUsageCountsRecorded(t *testing.T) {
	e := localLedgerRun(t, "ollama", 0, fakeLocalReader{LocalUsage{TokensIn: 11, TokensOut: 7, ToolCalls: 3, Reported: true}})
	if !e.Local || !e.TokensReported || e.LocalTokensIn != 11 || e.LocalTokensOut != 7 || e.ToolCalls != 3 {
		t.Fatalf("bad local entry: %+v", e)
	}
	if e.TokensIn != 0 || e.TokensOut != 0 {
		t.Fatalf("local tokens leaked into Claude fields: %+v", e)
	}
}

func TestLocalUsageNotReported(t *testing.T) {
	e := localLedgerRun(t, "pi", 0, fakeLocalReader{LocalUsage{ToolCalls: 2}})
	if !e.Local || e.TokensReported || e.LocalTokensIn != 0 || e.LocalTokensOut != 0 || e.ToolCalls != 2 {
		t.Fatalf("bad not-reported entry: %+v", e)
	}
}

func TestLocalUsageClaudeEntryUnchangedNotReportedFlagOff(t *testing.T) {
	e := localLedgerRun(t, "claude-code", 42, fakeLocalReader{LocalUsage{TokensIn: 9, Reported: true}})
	if e.Local || e.TokensReported || e.LocalTokensIn != 0 || e.TokensIn != 42 {
		t.Fatalf("claude entry changed: %+v", e)
	}
}

func TestLocalUsageParseOllamaAndPi(t *testing.T) {
	o := ParseLocalUsage("ollama", `{"prompt_eval_count": 5, "eval_count": 9}`)
	if !o.Reported || o.TokensIn != 5 || o.TokensOut != 9 {
		t.Fatalf("ollama: %+v", o)
	}
	p := ParseLocalUsage("pi-prompt", `{"type":"toolCall"} {"usage":{"input": 100, "output": 20}} {"type":"toolCall"}`)
	if !p.Reported || p.TokensIn != 100 || p.TokensOut != 20 || p.ToolCalls != 2 {
		t.Fatalf("pi: %+v", p)
	}
	n := ParseLocalUsage("pi", "plain text")
	if n.Reported {
		t.Fatalf("NotReported expected: %+v", n)
	}
}
