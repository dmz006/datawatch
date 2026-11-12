package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const testPackManifest = `
name: rest-pack
version: 1.0.0
description: REST test pack
kinds: [feature]
tasks:
  - title: Summarise the tree
    read_only: true
    report_path: .datawatch/reports/summary.md
inputs:
  - name: region
    type: string
    default: eu
  - name: token
    type: string
    secret: true
`

func packsTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DataDir = dir
	return &Server{cfg: cfg, dataDir: dir}
}

func packsReq(s *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handlePacks(rr, req)
	return rr
}

func writeTestPack(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte(testPackManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return src
}

func TestPacksConfigDefaultsAndEnableNeedsRegistry(t *testing.T) {
	s := packsTestServer(t)
	rr := packsReq(s, http.MethodGet, "/api/packs/config", "")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"builtin"`) {
		t.Fatalf("get config: %d %s", rr.Code, rr.Body.String())
	}
	// No skills manager: no connected registry, so enabling is refused.
	rr = packsReq(s, http.MethodPut, "/api/packs/config", `{"enabled":true}`)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "connected skills registry") {
		t.Fatalf("enable: %d %s", rr.Code, rr.Body.String())
	}
	if s.cfg.Packs.Enabled {
		t.Fatal("enabled must stay false after a refusal")
	}
	// Turning packs off or changing active does not need a registry.
	rr = packsReq(s, http.MethodPut, "/api/packs/config", `{"active":["builtin"]}`)
	if rr.Code != 200 {
		t.Fatalf("set active: %d %s", rr.Code, rr.Body.String())
	}
	rr = packsReq(s, http.MethodPut, "/api/packs/config", `{"active":["nope"]}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown active pack: %d", rr.Code)
	}
}

func TestPacksInstallAcceptListRemove(t *testing.T) {
	s := packsTestServer(t)
	src := writeTestPack(t)
	rr := packsReq(s, http.MethodPost, "/api/packs/install", `{"source_dir":"`+src+`"}`)
	if rr.Code != 200 {
		t.Fatalf("install: %d %s", rr.Code, rr.Body.String())
	}
	if rr = packsReq(s, http.MethodPost, "/api/packs/install", `{"source_dir":"`+src+`"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("reinstall: %d", rr.Code)
	}
	rr = packsReq(s, http.MethodGet, "/api/packs", "")
	if !strings.Contains(rr.Body.String(), `"rest-pack"`) || !strings.Contains(rr.Body.String(), `"accepted":false`) {
		t.Fatalf("list before accept: %s", rr.Body.String())
	}
	if rr = packsReq(s, http.MethodPost, "/api/packs/accept", `{"name":"rest-pack","version":"1.0.0"}`); rr.Code != 200 {
		t.Fatalf("accept: %d %s", rr.Code, rr.Body.String())
	}
	if rr = packsReq(s, http.MethodPost, "/api/packs/accept", `{"name":"missing","version":"1"}`); rr.Code != http.StatusNotFound {
		t.Fatalf("accept missing: %d", rr.Code)
	}
	rr = packsReq(s, http.MethodGet, "/api/packs", "")
	if !strings.Contains(rr.Body.String(), `"accepted":true`) {
		t.Fatalf("list after accept: %s", rr.Body.String())
	}
	if rr = packsReq(s, http.MethodPut, "/api/packs/config", `{"active":["builtin","rest-pack"]}`); rr.Code != 200 {
		t.Fatalf("activate installed: %d %s", rr.Code, rr.Body.String())
	}
	if rr = packsReq(s, http.MethodDelete, "/api/packs/rest-pack/1.0.0", ""); rr.Code != 200 {
		t.Fatalf("remove: %d %s", rr.Code, rr.Body.String())
	}
	if rr = packsReq(s, http.MethodDelete, "/api/packs/rest-pack/1.0.0", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("remove again: %d", rr.Code)
	}
	if rr = packsReq(s, http.MethodGet, "/api/packs", ""); strings.Contains(rr.Body.String(), "rest-pack") {
		t.Fatalf("still listed: %s", rr.Body.String())
	}
}

func TestPacksInputsLevelsAndSecrets(t *testing.T) {
	s := packsTestServer(t)
	src := writeTestPack(t)
	if rr := packsReq(s, http.MethodPost, "/api/packs/install", `{"source_dir":"`+src+`"}`); rr.Code != 200 {
		t.Fatalf("install: %d %s", rr.Code, rr.Body.String())
	}
	base := "/api/packs/inputs?pack=rest-pack&level="
	// Plain value at each level, kept apart.
	for _, c := range []struct{ q, val string }{
		{"global", "g"}, {"project&key=/work/p", "p"}, {"automaton&key=a1", "a"},
	} {
		rr := packsReq(s, http.MethodPut, base+c.q, `{"inputs":{"region":"`+c.val+`"}}`)
		if rr.Code != 200 {
			t.Fatalf("put %s: %d %s", c.q, rr.Code, rr.Body.String())
		}
	}
	for _, c := range []struct{ q, val string }{
		{"global", "g"}, {"project&key=/work/p", "p"}, {"automaton&key=a1", "a"},
	} {
		rr := packsReq(s, http.MethodGet, base+c.q, "")
		if !strings.Contains(rr.Body.String(), `"region":"`+c.val+`"`) {
			t.Fatalf("get %s: %s", c.q, rr.Body.String())
		}
	}
	// A secret input only takes a vault reference.
	if rr := packsReq(s, http.MethodPut, base+"global", `{"inputs":{"token":"plaintext"}}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("plain secret accepted: %d", rr.Code)
	}
	if rr := packsReq(s, http.MethodPut, base+"global", `{"inputs":{"token":"${secret:API_TOKEN}"}}`); rr.Code != 200 {
		t.Fatalf("secret ref: %d %s", rr.Code, rr.Body.String())
	}
	if rr := packsReq(s, http.MethodPut, base+"global", `{"inputs":{"bogus":"x"}}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("undeclared input: %d", rr.Code)
	}
	// Argument errors.
	if rr := packsReq(s, http.MethodGet, base+"project", ""); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing key: %d", rr.Code)
	}
	if rr := packsReq(s, http.MethodGet, "/api/packs/inputs?pack=ghost&level=global", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown pack: %d", rr.Code)
	}
	if rr := packsReq(s, http.MethodGet, "/api/packs/inputs?pack=rest-pack&level=bad", ""); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad level: %d", rr.Code)
	}
}

func TestPacksUnknownRoute(t *testing.T) {
	s := packsTestServer(t)
	if rr := packsReq(s, http.MethodGet, "/api/packs/nothing", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("got %d", rr.Code)
	}
}
