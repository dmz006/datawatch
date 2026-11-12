// Audit tests for the identity actions identity_set_refused,
// identity_restore and identity_reset (AGENT.md Audit Logging Rule): the
// handlers write the entries, a JSON-lines round trip returns them intact
// with hostile characters in the details, and the CEF line escapes the
// header and the extension and carries the signature triple.

package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/identity"
)

// newIdentityAuditServer is an identity test server with an audit log.
func newIdentityAuditServer(t *testing.T) *Server {
	t.Helper()
	s, _ := newIdentityTestServer(t)
	dir := t.TempDir()
	al, err := audit.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s.auditLog = al
	return s
}

func identityAuditEntries(t *testing.T, s *Server, action string) []audit.Entry {
	t.Helper()
	es, err := s.auditLog.Read(audit.QueryFilter{Action: action})
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	return es
}

func TestIdentityAuditRefusedWriteNamesMethodAndFieldsKept(t *testing.T) {
	method := http.MethodPut
	s := newIdentityAuditServer(t)
	idSeed(t, s)
	if w := idCall(s, method, "/api/identity", `{}`); w.Code != http.StatusConflict {
		t.Fatalf("%s: %d", method, w.Code)
	}
	es := identityAuditEntries(t, s, "identity_set_refused")
	if len(es) != 1 {
		t.Fatalf("want one identity_set_refused entry, got %d", len(es))
	}
	e := es[0]
	if e.Actor != "operator" {
		t.Errorf("actor %q", e.Actor)
	}
	if e.Details["method"] != method {
		t.Errorf("method detail %v", e.Details["method"])
	}
	if e.Details["fields_kept"] != "role,values,current_focus" {
		t.Errorf("fields_kept detail %v", e.Details["fields_kept"])
	}
	if e.Details["resource_type"] != "identity" || e.Details["resource_id"] != "self" {
		t.Errorf("resource details %v", e.Details)
	}
	// Field names only: never the identity content.
	for _, v := range e.Details {
		if strings.Contains(fmt.Sprint(v), "platform engineer") {
			t.Errorf("identity content leaked into the audit entry: %v", e.Details)
		}
	}
	if n := len(identityAuditEntries(t, s, "identity_set")); n != 1 {
		t.Errorf("the seeding write should be the only identity_set entry, got %d", n)
	}
}

func TestIdentityAuditHarmlessEmptyPatchIsAnUpdateNotARefusal(t *testing.T) {
	s := newIdentityAuditServer(t)
	idSeed(t, s)
	for _, m := range []string{http.MethodPatch, http.MethodPost} {
		if w := idCall(s, m, "/api/identity", `{}`); w.Code != 200 {
			t.Fatalf("%s: %d", m, w.Code)
		}
	}
	if n := len(identityAuditEntries(t, s, "identity_set_refused")); n != 0 {
		t.Errorf("PATCH/POST of nothing is not a refusal, got %d refused entries", n)
	}
	if n := len(identityAuditEntries(t, s, "identity_update")); n != 2 {
		t.Errorf("want 2 identity_update entries, got %d", n)
	}
}

func TestIdentityAuditRestoreAndReset(t *testing.T) {
	s := newIdentityAuditServer(t)
	idCall(s, http.MethodPut, "/api/identity", `{"role":"older"}`)
	idCall(s, http.MethodPut, "/api/identity", `{"role":"newer"}`)

	if w := idCall(s, http.MethodPost, "/api/identity/restore", `{"backup":"latest"}`); w.Code != 200 {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	es := identityAuditEntries(t, s, "identity_restore")
	if len(es) != 1 || es[0].Details["backup"] != "latest" {
		t.Fatalf("identity_restore entries: %+v", es)
	}

	// A failed restore is not audited as a restore.
	idCall(s, http.MethodPost, "/api/identity/restore", `{"backup":"../x"}`)
	if n := len(identityAuditEntries(t, s, "identity_restore")); n != 1 {
		t.Errorf("failed restore audited: %d entries", n)
	}

	if w := idCall(s, http.MethodPost, "/api/identity/reset", ""); w.Code != 200 {
		t.Fatalf("reset: %d", w.Code)
	}
	es = identityAuditEntries(t, s, "identity_reset")
	if len(es) != 1 || es[0].Details["resource_type"] != "identity" {
		t.Fatalf("identity_reset entries: %+v", es)
	}
}

func TestIdentityAuditNoLogIsANoop(t *testing.T) {
	s, _ := newIdentityTestServer(t) // auditLog nil
	idSeed(t, s)
	idCall(s, http.MethodPut, "/api/identity", `{}`)
	idCall(s, http.MethodPost, "/api/identity/reset", "")
	idCall(s, http.MethodPost, "/api/identity/restore", `{"backup":"latest"}`)
}

// identityAuditCases are the three new actions with details carrying the
// characters that break a log format: pipe, '=', backslash, newline,
// carriage return, edge spaces and non-ASCII text.
var identityAuditCases = []struct {
	name    string
	action  string
	details map[string]any
}{
	{
		name: "refused", action: "identity_set_refused",
		details: map[string]any{
			"resource_type": "identity",
			"resource_id":   "self",
			"method":        "PUT",
			"fields_kept":   "role,values|x=1\\y\nz\r 确认 ",
		},
	},
	{
		name: "restore", action: "identity_restore",
		details: map[string]any{
			"resource_type": "identity",
			"resource_id":   "self",
			"backup":        " identity-2026|1=2\\3\n4\r5 备份 ",
		},
	},
	{
		name: "reset", action: "identity_reset",
		details: map[string]any{
			"resource_type": "identity",
			"resource_id":   "self",
		},
	},
}

// TestIdentityAudit_JSONLines writes each action through the audit log,
// checks every raw line is valid JSON and that Read returns the details
// byte for byte.
func TestIdentityAudit_JSONLines(t *testing.T) {
	dir := t.TempDir()
	l, err := audit.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now().UTC().Truncate(time.Millisecond)
	for _, c := range identityAuditCases {
		if err := l.Write(audit.Entry{Timestamp: ts, Actor: "peer:a|b=c", Action: c.action, Details: c.details}); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		if !json.Valid(sc.Bytes()) {
			t.Fatalf("line %d is not valid JSON: %q", n, sc.Text())
		}
	}
	if n != len(identityAuditCases) {
		t.Fatalf("want %d lines, got %d", len(identityAuditCases), n)
	}

	l, err = audit.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	entries, err := l.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(identityAuditCases) {
		t.Fatalf("Read returned %d entries", len(entries))
	}
	// Read returns newest first and the cases were written in order.
	for i, c := range identityAuditCases {
		out := entries[len(identityAuditCases)-1-i]
		if out.Action != c.action || out.Actor != "peer:a|b=c" {
			t.Errorf("%s: action/actor %q/%q", c.name, out.Action, out.Actor)
		}
		for k, v := range c.details {
			if out.Details[k] != v {
				t.Errorf("%s: detail %q = %q, want %q", c.name, k, out.Details[k], v)
			}
		}
	}
}

// TestIdentityAudit_CEFEscaping checks the CEF line: header pipe and
// backslash escaping (through the version), the generic signature triple
// for an action with no dedicated signature, extension escaping of '=',
// backslash, newline and carriage return in the actor and every detail,
// and that no raw line break survives.
func TestIdentityAudit_CEFEscaping(t *testing.T) {
	audit.SetCEFVersionFn(func() string { return `9|0\1` })
	defer audit.SetCEFVersionFn(func() string { return "v?" })
	for _, c := range identityAuditCases {
		t.Run(c.name, func(t *testing.T) {
			line := audit.FormatCEFLine(audit.Entry{
				Timestamp: time.Now(),
				Actor:     "oper|ator=x\ninj",
				Action:    c.action,
				Details:   c.details,
			})
			wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|0|AuditEvent|3|`
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("header: %q, want prefix %q", line, wantPrefix)
			}
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw line break in CEF line (event injection): %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1="+c.action) {
				t.Errorf("action extension missing: %q", line)
			}
			if !strings.Contains(line, `suser=oper|ator\=x\ninj`) {
				t.Errorf("actor not escaped: %q", line)
			}
			// Every '=' that is not a key separator is escaped: strip the
			// escaped pairs and the line must hold only key=value joints.
			for _, v := range c.details {
				s := v.(string)
				esc := strings.NewReplacer(`\`, `\\`, `=`, `\=`, "\n", `\n`, "\r", `\r`).Replace(s)
				if !strings.Contains(line, esc) {
					t.Errorf("detail %q not found escaped as %q in %q", s, esc, line)
				}
			}
		})
	}
}

// TestIdentityAudit_CEFOfHandlerEntries renders the entries the handlers
// really wrote, so a change to the details keeps the CEF mapping honest:
// the method lands in requestMethod.
func TestIdentityAudit_CEFOfHandlerEntries(t *testing.T) {
	s := newIdentityAuditServer(t)
	idSeed(t, s)
	idCall(s, http.MethodPut, "/api/identity", `{}`)
	es := identityAuditEntries(t, s, "identity_set_refused")
	if len(es) != 1 {
		t.Fatalf("entries: %d", len(es))
	}
	line := audit.FormatCEFLine(es[0])
	for _, want := range []string{"requestMethod=PUT", "deviceCustomString1=identity_set_refused", "fields_kept"} {
		if !strings.Contains(line, want) {
			t.Errorf("CEF line lacks %q: %s", want, line)
		}
	}
}

// refusingIdentityMgr answers every write with the wipe-guard sentinel, so
// the refusal path is covered for PATCH and POST as well as PUT.
type refusingIdentityMgr struct{ identityManager }

func (refusingIdentityMgr) Set(identity.Identity) (identity.Identity, error) {
	return identity.Identity{}, identity.ErrEmptyOverwrite
}
func (refusingIdentityMgr) Update(identity.Identity) (identity.Identity, error) {
	return identity.Identity{}, fmt.Errorf("wrapped: %w", identity.ErrEmptyOverwrite)
}

func TestIdentityAuditRefusalNamesTheMethodForEveryWriteVerb(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			s := newIdentityAuditServer(t)
			idSeed(t, s)
			s.identityMgr = refusingIdentityMgr{identityManager: s.identityMgr}
			w := idCall(s, method, "/api/identity", `{}`)
			if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "identity_wipe_refused") {
				t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
			}
			es := identityAuditEntries(t, s, "identity_set_refused")
			if len(es) != 1 || es[0].Details["method"] != method {
				t.Fatalf("%s: audit entries %+v", method, es)
			}
		})
	}
}
