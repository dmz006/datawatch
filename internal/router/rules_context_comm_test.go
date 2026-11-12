package router

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

type rulesCommLog struct {
	puts  []string
	query string
}

func rulesCommDaemon(t *testing.T) (*rulesCommLog, int) {
	t.Helper()
	l := &rulesCommLog{}
	cfg := `{"rules_file":"RULES.md","context_files":["A.md"],"digest_mode":"off"}`
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			raw, _ := io.ReadAll(r.Body)
			l.puts = append(l.puts, string(raw))
			cfg = string(raw)
		} else {
			l.query = r.URL.RawQuery
		}
		_, _ = w.Write([]byte(`{"config":` + cfg + `,"policy":{"rules_file":"RULES.md","context_files":["A.md"],"digest_mode":"off","digest_max_bytes":0},"record":{"prd_id":"p1","recorded":true,"file_count":2,"present_count":1,"missing_count":1,"missing":["B.md"],"digest_mode":"off"}}`))
	})
	return l, port
}

func TestRulesContextSettingsComm_Read(t *testing.T) {
	l, port := rulesCommDaemon(t)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous rules p1"), " ")
	if l.query != "prd=p1" {
		t.Errorf("query = %q", l.query)
	}
	for _, want := range []string{"rules_file=RULES.md", "context_files=A.md", "1 missing", "B.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestRulesContextSettingsComm_Write(t *testing.T) {
	l, port := rulesCommDaemon(t)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous rules set digest_mode all"), " ")
	if len(l.puts) != 1 || !strings.Contains(l.puts[0], `"digest_mode":"all"`) || !strings.Contains(l.puts[0], `"rules_file":"RULES.md"`) {
		t.Fatalf("PUT = %v (reply %q)", l.puts, got)
	}
	r.HandleTestMessage("autonomous rules set context_files B.md,C.md /srv/x")
	if len(l.puts) != 2 || !strings.Contains(l.puts[1], `"project_dir":"/srv/x"`) || !strings.Contains(l.puts[1], `"C.md"`) {
		t.Fatalf("override PUT = %v", l.puts)
	}
}

func TestRulesContextSettingsComm_RejectsBadValue(t *testing.T) {
	l, port := rulesCommDaemon(t)
	r := setupPeerRouter(t, port)
	for _, msg := range []string{
		"autonomous rules set digest_mode sometimes",
		"autonomous rules set digest_max_bytes lots",
		"autonomous rules set rules_file /etc/passwd",
		"autonomous rules set nope 1",
		"autonomous rules set digest_mode",
		"autonomous rules a b",
	} {
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "failed") {
			t.Errorf("%q: reply = %q", msg, got)
		}
	}
	if len(l.puts) != 0 {
		t.Errorf("a rejected value must not write: %v", l.puts)
	}
}

func TestRulesContextSettingsComm_VerbsRegistered(t *testing.T) {
	for _, v := range []string{"rules", "rules set"} {
		if !slices.Contains(RulesContextCommVerbs(), v) {
			t.Errorf("verb %q missing", v)
		}
	}
	help := strings.Join(setupPeerRouter(t, 0).HandleTestMessage("autonomous"), " ")
	if !strings.Contains(help, "rules [automaton-id]") || !strings.Contains(help, "rules set <key> <value>") {
		t.Errorf("help must list both verbs: %s", help)
	}
}
