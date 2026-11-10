// Project pack list over REST: the list round-trips on upsert/get/list in the
// order given, and is normalized (trimmed, repeats and blanks dropped) on write.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestProjectsPacks_UpsertGetListRoundTrip(t *testing.T) {
	s := bl90Server(t)
	rr := s.projectsUpsertPolicy(t, `{"name":"acme","dir":"/home/<user>/acme","packs":[" zeta ","alpha","","zeta"]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", rr.Code, rr.Body.String())
	}
	want := []string{"zeta", "alpha"}

	rr = httptest.NewRecorder()
	s.handleProjects(rr, httptest.NewRequest(http.MethodGet, "/api/projects/acme", nil))
	var one projectListItem
	if err := json.Unmarshal(rr.Body.Bytes(), &one); err != nil || !reflect.DeepEqual(one.Packs, want) {
		t.Fatalf("get packs=%#v err=%v body=%s", one.Packs, err, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	s.handleProjects(rr, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	var all []projectListItem
	if err := json.Unmarshal(rr.Body.Bytes(), &all); err != nil {
		t.Fatalf("list decode: %v", err)
	}
	for _, p := range all {
		if p.Name == "acme" && reflect.DeepEqual(p.Packs, want) {
			return
		}
	}
	t.Fatalf("acme with packs %v not in list: %s", want, rr.Body.String())
}

func TestProjectsPacks_OmittedWhenEmpty(t *testing.T) {
	s := bl90Server(t)
	if rr := s.projectsUpsertPolicy(t, `{"name":"plain","dir":"/home/<user>/plain"}`); rr.Code != http.StatusOK {
		t.Fatalf("upsert status=%d", rr.Code)
	}
	rr := httptest.NewRecorder()
	s.handleProjects(rr, httptest.NewRequest(http.MethodGet, "/api/projects/plain", nil))
	if strings.Contains(rr.Body.String(), `"packs"`) {
		t.Fatalf("empty packs should be omitted: %s", rr.Body.String())
	}
}
