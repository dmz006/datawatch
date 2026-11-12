// The generated annotated template must carry the acknowledge-unauthenticated
// key so `config generate` / `config init` self-document it like every other
// server field.

package config

import (
	"strings"
	"testing"
)

func TestListenerAuth_AnnotatedTemplateIncludesKey(t *testing.T) {
	out := GenerateAnnotatedConfig(DefaultConfig())
	if !strings.Contains(out, "acknowledge_unauthenticated") {
		t.Fatal("generated template missing the acknowledge_unauthenticated key")
	}
	if !strings.Contains(out, "Silence the startup warning for listeners reachable without authentication") {
		t.Fatal("generated template entry is missing its annotation")
	}
	// The key must sit in the server section, not somewhere else.
	srvIdx := strings.Index(out, "server:\n")
	keyIdx := strings.Index(out, "acknowledge_unauthenticated")
	if srvIdx == -1 || keyIdx < srvIdx || !strings.Contains(out[srvIdx:keyIdx], "tls_cert") {
		t.Fatal("acknowledge_unauthenticated is not inside the server section")
	}
}
