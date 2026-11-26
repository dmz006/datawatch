package router

import (
	"strings"
	"testing"
)

func TestAcceptanceComm_Parse(t *testing.T) {
	cmd := Parse("acceptance confirm R1 seen yes I Saw  It")
	if cmd.Type != CmdAcceptance || cmd.Text != "confirm R1 seen yes I Saw  It" {
		t.Fatalf("%+v", cmd)
	}
	if got := Parse("acceptance"); got.Type != CmdAcceptance {
		t.Fatalf("%+v", got)
	}
}

func TestAcceptanceComm_Requests(t *testing.T) {
	path, body, ok := acceptanceCommRequest("confirm", "R1 seen YES I Saw it")
	if !ok || path != "/api/acceptance/confirm" || body["id"] != "R1" || body["answer"] != "yes" || body["words"] != "I Saw it" || body["actor"] != "channel" {
		t.Fatalf("%v %v %v", path, body, ok)
	}
	_, body, ok = acceptanceCommRequest("confirm", "R1 list yes abc123 it covers it")
	if !ok || body["hash"] != "abc123" || body["words"] != "it covers it" {
		t.Fatalf("%v", body)
	}
	_, body, ok = acceptanceCommRequest("edit", `R1 {"id":"other","actor":"operator","via_session":"x","words":"w"}`)
	if !ok || body["id"] != "R1" || body["actor"] != "channel" || body["via_session"] != nil {
		t.Fatalf("a message chose its own id or caller: %v", body)
	}
	for _, bad := range [][2]string{{"confirm", "R1 seen yes"}, {"accept", "R1 seen"}, {"edit", "R1 not-json"}, {"verdict", "R1 x met"}, {"freeze", "R1"}} {
		if _, _, ok := acceptanceCommRequest(bad[0], bad[1]); ok {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestAcceptanceComm_SecureRepliesCarryIDsAndResultsOnly(t *testing.T) {
	const view = `{"secure":%s,"hash":"h","list":{"request_id":"r1","items":[{"id":"a","statement":"private words"}]},"rollup":{"label":"open","items":[{"id":"a","result":{"value":"cannot_tell"}}]}}`
	plain := formatAcceptanceView(strings.Replace(view, "%s", "false", 1))
	secure := formatAcceptanceView(strings.Replace(view, "%s", "true", 1))
	if !strings.Contains(plain, "private words") || strings.Contains(secure, "private") {
		t.Fatalf("plain=%q secure=%q", plain, secure)
	}
	if !strings.Contains(secure, "a: cannot tell") {
		t.Fatalf("%q", secure)
	}
}
