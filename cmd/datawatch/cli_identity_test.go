package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type identityCLIDaemon struct {
	mu    sync.Mutex
	calls []string // "METHOD path"
	body  string   // last request body
}

func (d *identityCLIDaemon) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

// newIdentityCLIDaemon points the CLI at an httptest stand-in that answers
// every request with status and reply. HOME is a scratch dir so no real
// configuration or data directory is read.
func newIdentityCLIDaemon(t *testing.T, status int, reply string) *identityCLIDaemon {
	t.Helper()
	d := &identityCLIDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		d.mu.Lock()
		d.calls = append(d.calls, r.Method+" "+r.URL.Path)
		d.body = string(raw)
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DATAWATCH_TOKEN", "test-token")
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return d
}

func runIdentityCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newIdentityCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setIdentityStdin(t *testing.T, input string, terminal bool) {
	t.Helper()
	prevIn, prevTTY := identityStdin, identityStdinIsTerminal
	identityStdin = strings.NewReader(input)
	identityStdinIsTerminal = func() bool { return terminal }
	t.Cleanup(func() { identityStdin, identityStdinIsTerminal = prevIn, prevTTY })
}

const identityBackupsReply = `{"keep":10,"backups":[
 {"name":"identity-20261029T100000.000Z.yaml","created_at":"2026-10-29T10:00:00Z","size_bytes":10,"role":"older role","field_count":2},
 {"name":"identity-20261030T100000.000Z.yaml","created_at":"2026-10-30T10:00:00Z","size_bytes":10,"role":"","field_count":5}]}`

func TestIdentityCLI_BackupsTable(t *testing.T) {
	d := newIdentityCLIDaemon(t, http.StatusOK, identityBackupsReply)
	out, err := runIdentityCLI(t, "backups")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 1 || d.calls[0] != "GET /api/identity/backups" {
		t.Fatalf("calls = %v", d.calls)
	}
	newer := strings.Index(out, "identity-20261030T100000.000Z.yaml")
	older := strings.Index(out, "identity-20261029T100000.000Z.yaml")
	if newer < 0 || older < 0 || newer > older {
		t.Errorf("want both names, newest first:\n%s", out)
	}
	for _, want := range []string{"NAME", "TIME", "ROLE", "FIELDS", "older role", "5", "2"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestIdentityCLI_BackupsJSON(t *testing.T) {
	newIdentityCLIDaemon(t, http.StatusOK, identityBackupsReply)
	out, err := runIdentityCLI(t, "backups", "--json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"keep": 10`, `"backups"`, `"field_count": 5`, `"size_bytes": 10`} {
		if !strings.Contains(out, want) {
			t.Errorf("--json output lacks %q:\n%s", want, out)
		}
	}
}

func TestIdentityCLI_BackupsEmpty(t *testing.T) {
	newIdentityCLIDaemon(t, http.StatusOK, `{"backups":[],"keep":10}`)
	out, err := runIdentityCLI(t, "backups")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "no backups yet" {
		t.Errorf("output = %q", out)
	}
}

func TestIdentityCLI_RestoreDefaultsToLatest(t *testing.T) {
	d := newIdentityCLIDaemon(t, http.StatusOK, `{"role":"restored role"}`)
	out, err := runIdentityCLI(t, "restore")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 1 || d.calls[0] != "POST /api/identity/restore" {
		t.Fatalf("calls = %v", d.calls)
	}
	if !strings.Contains(d.body, `"backup":"latest"`) {
		t.Errorf("body = %s", d.body)
	}
	if !strings.Contains(out, "restored role") {
		t.Errorf("restored identity not printed:\n%s", out)
	}
}

func TestIdentityCLI_RestoreNamedBackup(t *testing.T) {
	d := newIdentityCLIDaemon(t, http.StatusOK, `{"role":"older role"}`)
	if _, err := runIdentityCLI(t, "restore", "--backup", "identity-20261029T100000.000Z.yaml"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.body, `"backup":"identity-20261029T100000.000Z.yaml"`) {
		t.Errorf("body = %s", d.body)
	}
}

func TestIdentityCLI_ResetWithYes(t *testing.T) {
	d := newIdentityCLIDaemon(t, http.StatusOK, `{}`)
	setIdentityStdin(t, "", false)
	if _, err := runIdentityCLI(t, "reset", "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 1 || d.calls[0] != "POST /api/identity/reset" {
		t.Errorf("calls = %v", d.calls)
	}
}

func TestIdentityCLI_ResetTypedConfirmation(t *testing.T) {
	d := newIdentityCLIDaemon(t, http.StatusOK, `{}`)
	setIdentityStdin(t, "RESET\n", true)
	out, err := runIdentityCLI(t, "reset")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 1 || d.calls[0] != "POST /api/identity/reset" {
		t.Errorf("calls = %v", d.calls)
	}
	for _, want := range []string{"RESET", "backup is kept", "datawatch identity restore"} {
		if !strings.Contains(out, want) {
			t.Errorf("confirmation text lacks %q:\n%s", want, out)
		}
	}
}

func TestIdentityCLI_ResetWrongConfirmationMakesNoCall(t *testing.T) {
	d := newIdentityCLIDaemon(t, http.StatusOK, `{}`)
	for _, typed := range []string{"reset\n", "yes\n", "\n", ""} {
		setIdentityStdin(t, typed, true)
		out, err := runIdentityCLI(t, "reset")
		if err != nil {
			t.Fatalf("input %q: %v", typed, err)
		}
		if !strings.Contains(out, "Cancelled") {
			t.Errorf("input %q: output lacks Cancelled:\n%s", typed, out)
		}
	}
	if n := d.count(); n != 0 {
		t.Errorf("daemon got %d calls, want 0: %v", n, d.calls)
	}
}

func TestIdentityCLI_ResetNoTerminalNoYes(t *testing.T) {
	d := newIdentityCLIDaemon(t, http.StatusOK, `{}`)
	setIdentityStdin(t, "RESET\n", false)
	_, err := runIdentityCLI(t, "reset")
	if err == nil || !strings.Contains(err.Error(), "not a terminal") || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v", err)
	}
	if n := d.count(); n != 0 {
		t.Errorf("daemon got %d calls, want 0", n)
	}
}

func TestIdentityCLI_SetShowsWipeRefusal(t *testing.T) {
	d := newIdentityCLIDaemon(t, http.StatusConflict,
		`{"error":"refusing to replace the saved identity with an empty one; to clear it on purpose use POST /api/identity/reset","code":"identity_wipe_refused","fields_kept":["role","values"]}`)
	_, err := runIdentityCLI(t, "set", "--field", "role", "--value", "x")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"409", "identity_wipe_refused", "refusing to replace the saved identity", "identity reset", "role, values"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if len(d.calls) != 1 || d.calls[0] != "PATCH /api/identity" {
		t.Errorf("calls = %v", d.calls)
	}
}

func TestIdentityCLI_OtherErrorsKeepStatusAndBody(t *testing.T) {
	newIdentityCLIDaemon(t, http.StatusNotFound, `{"error":"backup not found"}`)
	_, err := runIdentityCLI(t, "restore", "--backup", "nope")
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") || !strings.Contains(err.Error(), "backup not found") {
		t.Errorf("err = %v", err)
	}
}

func TestIdentityCLI_HelpDescribesBackupsAndReset(t *testing.T) {
	cmd := newIdentityCmd()
	for _, want := range []string{"backup", "empty", "identity reset", "identity restore"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("Long help lacks %q", want)
		}
	}
	for _, use := range []string{"reset", "backups", "restore"} {
		found := false
		for _, c := range cmd.Commands() {
			if c.Use == use {
				found = true
			}
		}
		if !found {
			t.Errorf("subcommand %q not registered", use)
		}
	}
}
