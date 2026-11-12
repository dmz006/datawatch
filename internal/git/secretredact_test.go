package git

import (
	"strings"
	"testing"
)

func TestRedactSecretsDetectors(t *testing.T) {
	aws := "AKIA" + "IOSFODNN7EXAMPLE"
	out, n := RedactSecrets("k=" + aws + " ok")
	if n != 1 || strings.Contains(out, "AKIA") || !strings.Contains(out, SecretRedactMarker) {
		t.Fatalf("got %q n=%d", out, n)
	}
	pem := "-----BEGIN EC PRIVATE" + " KEY-----\nzzz\n-----END EC PRIVATE" + " KEY-----\ntail"
	out, n = RedactSecrets(pem)
	if n != 1 || strings.Contains(out, "zzz") || !strings.HasSuffix(out, "tail") {
		t.Fatalf("pem: %q n=%d", out, n)
	}
	if out, n = RedactSecrets("plain text"); n != 0 || out != "plain text" {
		t.Fatalf("clean text changed: %q", out)
	}
	if ContainsSecret("password=aaaaaaaaaaaaaaaaaaaa") {
		t.Fatal("entropy floor not applied")
	}
}
