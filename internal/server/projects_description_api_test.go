// Project description through the /api/projects web endpoints.
//
// POST /api/projects accepts a description and GET /api/projects (list) and
// GET /api/projects/{name} must return it, including after a save round-trip
// — the description is a persistent part of the project record.

package server

import (
	"encoding/json"
	"net/http"
	"testing"
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
