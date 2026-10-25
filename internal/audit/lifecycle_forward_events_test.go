// Audit tests for the lifecycle peer-forward event (AGENT.md Audit Logging
// Rule): the action name, its CEF signature, a JSON-lines round-trip and a CEF
// escape test with pipe, '=', backslash and newline in the header AND the
// extension values. The webhook signatures are checked alongside so a new
// action cannot reuse one of their IDs.

package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const lifecycleForwardHostileError = "boom\nCEF:0|evil|x|1|2|3|9|a=b\\c"

func lifecycleForwardDetails() map[string]any {
	return map[string]any{
		"subscription_id": "sub|1=x",
		"delivery_id":     "d|1=x",
		"peer":            "peer|1=x",
		"event_kind":      "prd.completed",
		"host":            "peer.example.net:8443",
		"attempt":         2,
		"max_attempts":    5,
		"status_code":     503,
		"outcome":         "failed",
		"error":           lifecycleForwardHostileError,
		"hops":            1,
		"duration_ms":     int64(4321),
	}
}

func TestLifecycleForwardEvents_NameAndCEF(t *testing.T) {
	const (
		wantAction = "autonomous_lifecycle_forward_delivery"
		wantID     = 48
		wantName   = "AutonomousLifecycleForwardDelivery"
		wantSev    = 3
	)
	if ActionLifecycleForwardDelivery != wantAction {
		t.Errorf("action constant = %q, want %q", ActionLifecycleForwardDelivery, wantAction)
	}
	id, name, sev := cefSignature(ActionLifecycleForwardDelivery)
	if id != wantID || name != wantName || sev != wantSev {
		t.Errorf("cefSignature = %d %q %d, want %d %q %d", id, name, sev, wantID, wantName, wantSev)
	}
	// The ID must differ from every other lifecycle signature, the webhook ones included.
	for _, other := range []string{ActionLifecycleWebhookDelivery, ActionLifecycleWebhookFailed} {
		if oid, _, _ := cefSignature(other); oid == id {
			t.Errorf("signature id %d is shared with %q", id, other)
		}
	}
}

func TestLifecycleForwardEvents_JSONLines(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := Entry{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Actor:     "autonomous",
		Action:    ActionLifecycleForwardDelivery,
		Details:   lifecycleForwardDetails(),
	}
	if err := l.Write(in); err != nil {
		t.Fatal(err)
	}
	_ = l.Close()

	f, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 1 {
		t.Fatalf("want 1 JSON line, got %d: %q", len(lines), lines)
	}
	var out Entry
	if err := json.Unmarshal([]byte(lines[0]), &out); err != nil {
		t.Fatalf("line is not valid JSON: %v (%q)", err, lines[0])
	}
	if out.Action != ActionLifecycleForwardDelivery || out.Actor != "autonomous" || !out.Timestamp.Equal(in.Timestamp) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Details["peer"] != "peer|1=x" || out.Details["delivery_id"] != "d|1=x" ||
		out.Details["error"] != lifecycleForwardHostileError || out.Details["outcome"] != "failed" ||
		out.Details["host"] != "peer.example.net:8443" || out.Details["event_kind"] != "prd.completed" ||
		out.Details["attempt"] != float64(2) || out.Details["status_code"] != float64(503) ||
		out.Details["hops"] != float64(1) || out.Details["duration_ms"] != float64(4321) {
		t.Fatalf("details lost: %+v", out.Details)
	}
}

func TestLifecycleForwardEvents_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	e := Entry{
		Timestamp: time.Now(),
		Actor:     "au|to=x",
		Action:    ActionLifecycleForwardDelivery,
		Details:   lifecycleForwardDetails(),
	}
	line := FormatCEFLine(e)

	// The header triple, with the version's pipe and backslash escaped.
	wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|48|AutonomousLifecycleForwardDelivery|3|`
	if !strings.HasPrefix(line, wantPrefix) {
		t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
	}
	// One physical line: the hostile newline must be escaped.
	if strings.ContainsAny(line, "\n\r") {
		t.Fatalf("raw newline survived into CEF line: %q", line)
	}
	if !strings.Contains(line, `suser=au|to\=x`) {
		t.Errorf("actor extension not escaped: %q", line)
	}
	if !strings.Contains(line, "deviceCustomString1="+ActionLifecycleForwardDelivery) {
		t.Errorf("action extension missing: %q", line)
	}
	// The hostile error sits in msg with its '=', '\' and newline escaped.
	if !strings.Contains(line, `error\=boom\nCEF:0|evil|x|1|2|3|9|a\=b\\c`) {
		t.Errorf("hostile error not escaped in msg: %q", line)
	}
	// The only header-looking text is the real header.
	if got := strings.Count(line, "CEF:0|datawatch|"); got != 1 {
		t.Errorf("want exactly one real CEF header, found %d in %q", got, line)
	}
	if got := cefExtEscape(lifecycleForwardHostileError); got != `boom\nCEF:0|evil|x|1|2|3|9|a\=b\\c` {
		t.Errorf("cefExtEscape = %q", got)
	}
}

// lifecycleForwardReceiveDetails are the receive record's fields, with the
// hostile characters in the free-text reason and the peer name.
func lifecycleForwardReceiveDetails() map[string]any {
	return map[string]any{
		"outcome":     "refused",
		"status_code": 409,
		"reason":      "boom\nCEF:0|evil|x|1|2|3|9|a=b\\c",
		"event_id":    "ev|1=x",
		"event_kind":  "prd.completed",
		"origin_host": "origin|1=x",
		"sender_host": "sender.example.net",
		"hops":        1,
		"peer":        "peer|1=x",
	}
}

func TestLifecycleForwardReceiveEvents_NameAndCEF(t *testing.T) {
	const (
		wantAction = "autonomous_lifecycle_forward_receive"
		wantID     = 49
		wantName   = "AutonomousLifecycleForwardReceive"
		wantSev    = 3
	)
	if ActionLifecycleForwardReceive != wantAction {
		t.Errorf("action constant = %q, want %q", ActionLifecycleForwardReceive, wantAction)
	}
	id, name, sev := cefSignature(ActionLifecycleForwardReceive)
	if id != wantID || name != wantName || sev != wantSev {
		t.Errorf("cefSignature = %d %q %d, want %d %q %d", id, name, sev, wantID, wantName, wantSev)
	}
	// The ID must differ from every other lifecycle signature, the send-side ones included.
	for _, other := range []string{ActionLifecycleWebhookDelivery, ActionLifecycleWebhookFailed, ActionLifecycleForwardDelivery} {
		if oid, _, _ := cefSignature(other); oid == id {
			t.Errorf("signature id %d is shared with %q", id, other)
		}
	}
}

func TestLifecycleForwardReceiveEvents_JSONLines(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := Entry{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Actor:     "peer:origin",
		Action:    ActionLifecycleForwardReceive,
		Details:   lifecycleForwardReceiveDetails(),
	}
	if err := l.Write(in); err != nil {
		t.Fatal(err)
	}
	_ = l.Close()

	f, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 1 {
		t.Fatalf("want 1 JSON line, got %d: %q", len(lines), lines)
	}
	var out Entry
	if err := json.Unmarshal([]byte(lines[0]), &out); err != nil {
		t.Fatalf("line is not valid JSON: %v (%q)", err, lines[0])
	}
	if out.Action != ActionLifecycleForwardReceive || out.Actor != "peer:origin" || !out.Timestamp.Equal(in.Timestamp) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Details["peer"] != "peer|1=x" || out.Details["event_id"] != "ev|1=x" ||
		out.Details["reason"] != lifecycleForwardReceiveDetails()["reason"] || out.Details["outcome"] != "refused" ||
		out.Details["origin_host"] != "origin|1=x" || out.Details["sender_host"] != "sender.example.net" ||
		out.Details["status_code"] != float64(409) || out.Details["hops"] != float64(1) {
		t.Fatalf("details lost: %+v", out.Details)
	}
}

func TestLifecycleForwardReceiveEvents_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	e := Entry{
		Timestamp: time.Now(),
		Actor:     "peer|to=x",
		Action:    ActionLifecycleForwardReceive,
		Details:   lifecycleForwardReceiveDetails(),
	}
	line := FormatCEFLine(e)

	wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|49|AutonomousLifecycleForwardReceive|3|`
	if !strings.HasPrefix(line, wantPrefix) {
		t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
	}
	if strings.ContainsAny(line, "\n\r") {
		t.Fatalf("raw newline survived into CEF line: %q", line)
	}
	if !strings.Contains(line, `suser=peer|to\=x`) {
		t.Errorf("actor extension not escaped: %q", line)
	}
	if !strings.Contains(line, "deviceCustomString1="+ActionLifecycleForwardReceive) {
		t.Errorf("action extension missing: %q", line)
	}
	// The hostile reason lands in a custom string or the msg overflow, escaped.
	hostile := lifecycleForwardReceiveDetails()["reason"].(string)
	if !strings.Contains(line, cefExtEscape(hostile)) {
		t.Errorf("hostile reason not escaped: %q", line)
	}
	if got := strings.Count(line, "CEF:0|datawatch|"); got != 1 {
		t.Errorf("want exactly one real CEF header, found %d in %q", got, line)
	}
}
