package packs

import "testing"

func TestCheckSecretInput(t *testing.T) {
	sec := InputDecl{Name: "token", Secret: true}
	if err := CheckSecretInput(sec, "${secret:my-token}"); err != nil {
		t.Fatalf("reference refused: %v", err)
	}
	for _, bad := range []string{"", "hunter2", "${secret:}", "x${secret:a}", "${secret:a}x", "${env:A}"} {
		if err := CheckSecretInput(sec, bad); err == nil {
			t.Errorf("value %q accepted", bad)
		}
	}
	if err := CheckSecretInput(InputDecl{Name: "region"}, "us"); err != nil {
		t.Fatalf("plain input refused: %v", err)
	}
}

func TestRedactInputs(t *testing.T) {
	decls := []InputDecl{{Name: "token", Secret: true}, {Name: "bad", Secret: true}, {Name: "region"}}
	in := map[string]string{"token": "${secret:t}", "bad": "literal", "region": "us"}
	got := RedactInputs(in, decls)
	if got["token"] != "${secret:t}" || got["region"] != "us" {
		t.Fatalf("got %v", got)
	}
	if got["bad"] == "literal" {
		t.Fatalf("literal secret leaked: %v", got)
	}
	if in["bad"] != "literal" {
		t.Fatal("input map mutated")
	}
}
