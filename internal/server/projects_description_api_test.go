// Project description, directory and default backend through the
// /api/projects web endpoints.
//
// POST /api/projects accepts description, dir and default_backend; GET
// /api/projects (list) and GET /api/projects/{name} must return all of them,
// including after a save round-trip — they are persistent parts of the
// project record. The upsert is a whole-record write: an omitted (or empty)
// field clears the stored value, and every caller that edits one field must
// send the others back unchanged.

package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const descriptionProjectBody = `{
  "name": "acme",
  "dir": "/srv/acme",
  "default_backend": "claude-code",
  "description": "the billing service of the Automata",
  "owner": "platform"
}`

func TestProjectDescription_PostGetRoundTrip(t *testing.T) {
	s := bl90Server(t)

	if rr := recordDo(t, s, http.MethodPost, "/api/projects", descriptionProjectBody); rr.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr := recordDo(t, s, http.MethodGet, "/api/projects/acme", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got projectListItem
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Description != "the billing service of the Automata" {
		t.Errorf("description = %q, want %q", got.Description, "the billing service of the Automata")
	}
	if got.Dir != "/srv/acme" {
		t.Errorf("dir = %q, want /srv/acme", got.Dir)
	}
	if got.Owner != "platform" {
		t.Errorf("owner = %q, want platform", got.Owner)
	}
}

func TestProjectDescription_KeepsDescriptionOnList(t *testing.T) {
	s := bl90Server(t)
	if rr := recordDo(t, s, http.MethodPost, "/api/projects", descriptionProjectBody); rr.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr := recordDo(t, s, http.MethodGet, "/api/projects", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET list status=%d body=%s", rr.Code, rr.Body.String())
	}
	var list []projectListItem
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1", len(list))
	}
	if list[0].Description != "the billing service of the Automata" {
		t.Errorf("list[0].description = %q, want %q", list[0].Description, "the billing service of the Automata")
	}
	if list[0].Dir != "/srv/acme" {
		t.Errorf("list[0].dir = %q, want /srv/acme", list[0].Dir)
	}
	if list[0].DefaultBackend != "claude-code" {
		t.Errorf("list[0].default_backend = %q, want claude-code", list[0].DefaultBackend)
	}
}

// Updating one field of the entry (here default_backend) must not clear the
// already-saved description; the PWA edit form posts the whole entry and the
// description must stay.
func TestProjectDescription_UpdateKeepsDescription(t *testing.T) {
	s := bl90Server(t)
	if rr := recordDo(t, s, http.MethodPost, "/api/projects", descriptionProjectBody); rr.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rr.Code, rr.Body.String())
	}

	update := `{"name":"acme","dir":"/srv/acme","default_backend":"opencode","description":"the billing service of the Automata"}`
	if rr := recordDo(t, s, http.MethodPost, "/api/projects", update); rr.Code != http.StatusOK {
		t.Fatalf("update POST status=%d body=%s", rr.Code, rr.Body.String())
	}
	if entry := s.cfg.Projects["acme"]; entry.DefaultBackend != "opencode" {
		t.Errorf("default_backend = %q, want opencode", entry.DefaultBackend)
	}
	if entry := s.cfg.Projects["acme"]; entry.Description != "the billing service of the Automata" {
		t.Errorf("description after update = %q, want %q", entry.Description, "the billing service of the Automata")
	}

	// And the reply to the update carries it too.
	rr := recordDo(t, s, http.MethodGet, "/api/projects/acme", "")
	var got projectListItem
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.Description != "the billing service of the Automata" {
		t.Errorf("GET description after update = %q, want %q", got.Description, "the billing service of the Automata")
	}
}

// The upsert writes the config file: a Save followed by a Load (the daemon
// restart path) must carry directory, default backend and description — the
// written record is the source every other surface reads back.
func TestProjectDescription_SaveLoadRoundTripCarriesAllThree(t *testing.T) {
	s := bl90Server(t)
	if rr := recordDo(t, s, http.MethodPost, "/api/projects", descriptionProjectBody); rr.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rr.Code, rr.Body.String())
	}
	if s.cfgPath == "" {
		t.Fatalf("test server has no config path")
	}
	loaded, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	entry, ok := loaded.Projects["acme"]
	if !ok {
		t.Fatalf("loaded config missing project acme: %+v", loaded.Projects)
	}
	if entry.Dir != "/srv/acme" {
		t.Errorf("dir after load = %q, want /srv/acme", entry.Dir)
	}
	if entry.DefaultBackend != "claude-code" {
		t.Errorf("default_backend after load = %q, want claude-code", entry.DefaultBackend)
	}
	if entry.Description != "the billing service of the Automata" {
		t.Errorf("description after load = %q, want %q", entry.Description, "the billing service of the Automata")
	}

	// A second save (nothing else touched) keeps all three.
	loaded.Projects["acme"] = entry
	if err := config.Save(loaded, s.cfgPath); err != nil {
		t.Fatalf("config.Save 2: %v", err)
	}
	second, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatalf("config.Load 2: %v", err)
	}
	got := second.Projects["acme"]
	if got.Dir != "/srv/acme" || got.DefaultBackend != "claude-code" || got.Description != "the billing service of the Automata" {
		t.Errorf("second round-trip dropped a field: %+v", got)
	}

	// ... and the live server still serves them after the external round-trip.
	s.cfg = second
	rr := recordDo(t, s, http.MethodGet, "/api/projects/acme", "")
	var item projectListItem
	_ = json.Unmarshal(rr.Body.Bytes(), &item)
	if item.Dir != "/srv/acme" || item.DefaultBackend != "claude-code" || item.Description != "the billing service of the Automata" {
		t.Errorf("server after reload = %+v, want all three fields", item)
	}
}

// Whole-record contract: the upsert body IS the record. A POST that omits
// description and default_backend (nothing but name and dir) clears the
// stored values rather than guessing them back. Callers that edit one field
// are expected to send the others back — which is what the MCP, CLI and
// comm-channel surfaces implement on top of this contract.
func TestProjectDescription_OmittedFieldsClearStoredValues(t *testing.T) {
	s := bl90Server(t)
	if rr := recordDo(t, s, http.MethodPost, "/api/projects", descriptionProjectBody); rr.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := recordDo(t, s, http.MethodPost, "/api/projects", `{"name":"acme","dir":"/srv/acme"}`); rr.Code != http.StatusOK {
		t.Fatalf("partial POST status=%d body=%s", rr.Code, rr.Body.String())
	}
	rr := recordDo(t, s, http.MethodGet, "/api/projects/acme", "")
	var got projectListItem
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.Dir != "/srv/acme" {
		t.Errorf("dir = %q, want /srv/acme", got.Dir)
	}
	if got.Description != "" {
		t.Errorf("description = %q, want cleared", got.Description)
	}
	if got.DefaultBackend != "" {
		t.Errorf("default_backend = %q, want cleared", got.DefaultBackend)
	}
}

// An explicit empty string is the same as an omission at this layer: it
// clears. (The MCP and CLI surfaces distinguish "omitted" from "empty" on
// their own side and carry the stored value for the omitted case.)
func TestProjectDescription_ExplicitEmptyStringClears(t *testing.T) {
	s := bl90Server(t)
	if rr := recordDo(t, s, http.MethodPost, "/api/projects", descriptionProjectBody); rr.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := `{"name":"acme","dir":"/srv/acme","description":"","default_backend":""}`
	if rr := recordDo(t, s, http.MethodPost, "/api/projects", body); rr.Code != http.StatusOK {
		t.Fatalf("empty POST status=%d body=%s", rr.Code, rr.Body.String())
	}
	rr := recordDo(t, s, http.MethodGet, "/api/projects", "")
	var items []projectListItem
	_ = json.Unmarshal(rr.Body.Bytes(), &items)
	for _, it := range items {
		if it.Name != "acme" {
			continue
		}
		if it.Description != "" {
			t.Errorf("description = %q, want cleared", it.Description)
		}
		if it.DefaultBackend != "" {
			t.Errorf("default_backend = %q, want cleared", it.DefaultBackend)
		}
		return
	}
	t.Fatalf("acme not in list: %v", items)
}
