package server

// Tests for the autonomous.tags.placement settings on the REST config surface,
// for the system/axis filters of the tag listing, and for the refusal of an
// operator writing a system tag to a session.

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// placementDisplay extracts the autonomous.tags.placement object of
// GET /api/config.
func placementDisplay(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	got, ok := getTagSettings(t, s)["placement"].(map[string]interface{})
	if !ok {
		t.Fatalf("GET /api/config: no autonomous.tags.placement object")
	}
	return got
}

func TestPlacementTags_APIRoundTrip(t *testing.T) {
	s, _ := newTagSettingsServer(t)

	// A default config carries the placement block: the master switch on,
	// every built-in axis on with its conventional template, and no custom
	// axes.
	if got, want := placementDisplay(t, s), defaultPlacementDisplay(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET placement = %v, want %v", got, want)
	}

	// A per-key PATCH round trip: the master switch, an axis off, and one
	// axis template.
	rr := putTagPatch(t, s, map[string]interface{}{
		"autonomous.tags.placement.enabled":         false,
		"autonomous.tags.placement.lane.enabled":    false,
		"autonomous.tags.placement.lane.template":   "L{lane}",
		"autonomous.tags.placement.compute.enabled": true,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT placement keys: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	placement := s.cfg.Autonomous.Tags.Placement
	if placement.Enabled || placement.Lane.Enabled || placement.Lane.Template != "L{lane}" || !placement.Compute.Enabled {
		t.Fatalf("stored placement = %+v", placement)
	}
	got := placementDisplay(t, s)
	if got["enabled"] != false {
		t.Errorf("GET placement.enabled = %v, want false", got["enabled"])
	}
	axes := got["axes"].(map[string]interface{})
	if lane := axes["lane"].(map[string]interface{}); lane["enabled"] != false || lane["template"] != "L{lane}" {
		t.Errorf("GET placement axes.lane = %v, want {enabled:false template:L{lane}}", lane)
	}
	if compute := axes["compute"].(map[string]interface{}); compute["enabled"] != true || compute["template"] != "compute:{node}" {
		t.Errorf("GET placement axes.compute = %v, want its previous value", compute)
	}

	// The comm channel and the CLI send the value as a string; the same keys
	// are accepted and land the same way.
	rr = putTagBody(t, s, `{"autonomous.tags.placement.enabled":"true","autonomous.tags.placement.project.enabled":"true","autonomous.tags.placement.compute.enabled":"false"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT placement strings: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	placement = s.cfg.Autonomous.Tags.Placement
	if !placement.Enabled || !placement.Project.Enabled || placement.Compute.Enabled {
		t.Fatalf("stored placement after string PUT = %+v", placement)
	}

	// A template without the axis' own placeholder is refused with exactly the
	// shared validator's text, and nothing is applied.
	before := s.cfg.Autonomous.Tags
	for key, value := range map[string]string{
		"autonomous.tags.placement.lane.template": "\"L\"",
		"autonomous.tags.placement.cpu.enabled":   "\"true\"",
		"autonomous.tags.placement.lane.switch":   "\"true\"",
	} {
		rr := putTagBody(t, s, `{"`+key+`":`+value+`}`)
		want := config.ValidateTagSetting(key, strings.Trim(value, `"`))
		if want == nil {
			t.Fatalf("test bug: %s accepts %v", key, value)
		}
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %s: got %d %s, want 400", key, rr.Code, rr.Body.String())
			continue
		}
		if got := strings.TrimSpace(rr.Body.String()); got != want.Error() {
			t.Errorf("PUT %s: REST message = %q, want the shared validator text %q", key, got, want.Error())
		}
	}
	if !reflect.DeepEqual(s.cfg.Autonomous.Tags, before) {
		t.Fatalf("a refused placement PUT changed the block: %+v", before)
	}
}

// placementTagView is the part of a listed tag this suite reads.
type placementTagView struct {
	Name   string `json:"name"`
	System bool   `json:"system"`
}

// placementListTags lists the tags with a query and returns name → view.
func placementListTags(t *testing.T, s *Server, query string) map[string]placementTagView {
	t.Helper()
	rec := tagCall(t, s, http.MethodGet, "/api/autonomous/tags"+query, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list tags %q: %d %s", query, rec.Code, rec.Body.String())
	}
	var out struct {
		Tags []placementTagView `json:"tags"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	m := map[string]placementTagView{}
	for _, tg := range out.Tags {
		m[tg.Name] = tg
	}
	return m
}

func TestPlacementTags_LookupSystemAndAxisFilters(t *testing.T) {
	s, mgr := newTagTestServer(t)
	t.Helper()
	sys, err := mgr.EnsureSystemTag("lane:gpu")
	if err != nil {
		t.Fatalf("EnsureSystemTag: %v", err)
	}
	if !sys.System {
		t.Fatalf("EnsureSystemTag returned a non-system tag: %+v", sys)
	}
	createTag(t, s, `{"name":"infra"}`)
	createTag(t, s, `{"name":"lane:cpu"}`)

	all := placementListTags(t, s, "")
	if len(all) != 3 {
		t.Fatalf("no filter: %d tags, want 3: %v", len(all), all)
	}
	if got := all["lane:gpu"]; !got.System {
		t.Errorf("lane:gpu (registry-created): system = %v, want true", got.System)
	}
	for name, wantSystem := range map[string]bool{"lane:cpu": false, "infra": false} {
		if got := all[name]; got.System != wantSystem {
			t.Errorf("%s: system = %v, want %v", name, got.System, wantSystem)
		}
	}

	// system=true keeps the registry-created tag only; system=false the
	// operator tags only; the two halves cover the whole list.
	sysOnly := placementListTags(t, s, "?system=true")
	if len(sysOnly) != 1 || !sysOnly["lane:gpu"].System {
		t.Fatalf("system=true: %v, want only lane:gpu", sysOnly)
	}
	opOnly := placementListTags(t, s, "?system=false")
	if len(opOnly) != 2 {
		t.Fatalf("system=false: %v, want infra and lane:cpu", opOnly)
	}

	// axis=lane keeps the lane-axis tags of both kinds (the "<axis>:<value>"
	// names); axis=infra keeps the bare name that matches it.
	lane := placementListTags(t, s, "?axis=lane")
	if len(lane) != 2 {
		t.Fatalf("axis=lane: %v, want lane:gpu and lane:cpu", lane)
	}
	infra := placementListTags(t, s, "?axis=infra")
	if len(infra) != 1 {
		t.Fatalf("axis=infra: %v, want infra", infra)
	}

	// The filters combine: the system lane tag, and nothing else.
	both := placementListTags(t, s, "?axis=lane&system=true")
	if len(both) != 1 {
		t.Fatalf("axis=lane&system=true: %v, want only lane:gpu", both)
	}
	none := placementListTags(t, s, "?axis=lane&system=false")
	if got := placementListTags(t, s, "?axis=infra&system=true"); len(got) != 0 {
		t.Fatalf("axis=infra&system=true: %v, want no tag", got)
	}
	if len(none) != 1 {
		t.Fatalf("axis=lane&system=false: %v, want only lane:cpu", none)
	}

	// An axis name outside the axis vocabulary (lowercase letters, digits and
	// hyphens) is refused with 400.
	rec := tagCall(t, s, http.MethodGet, `/api/autonomous/tags?axis=BAD_NAME`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("axis=BAD_NAME: got %d, want 400", rec.Code)
	}
}

func TestPlacementTags_SessionRefusesSystemTag(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	amgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	s.autonomousMgr = newTestAPI(amgr)
	s.wireSessionTags()
	sys, err := amgr.EnsureSystemTag("lane:gpu")
	if err != nil {
		t.Fatalf("EnsureSystemTag: %v", err)
	}
	sysID := sys.ID
	tg := createTag(t, s, `{"name":"infra"}`)
	fullID := startTagsSession(t, s, `{"task":"plain","project_dir":"`+t.TempDir()+`"}`)

	// An operator assigning the system tag is refused before anything is
	// written; the operator tag on its own still goes through.
	w := sessionTagsCall(t, s, "", fullID, tg.ID, sysID)
	if w.Code != http.StatusConflict {
		t.Fatalf("assign operator+system tag: got %d %s, want 409", w.Code, w.Body.String())
	}
	if sess, ok := s.manager.GetSession(fullID); !ok || len(sess.Tags) != 0 {
		t.Fatalf("refused assignment left tags %v", sess.Tags)
	}
	w = sessionTagsCall(t, s, "", fullID, sysID)
	if w.Code != http.StatusConflict {
		t.Fatalf("assign the system tag alone: got %d %s, want 409", w.Code, w.Body.String())
	}
	w = sessionTagsCall(t, s, "", fullID, tg.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("assign the operator tag: want 200, got %d %s", w.Code, w.Body.String())
	}
	got := decodeSessionTagsFields(t, w)
	if !reflect.DeepEqual(got.Tags, []string{tg.ID}) {
		t.Errorf("own tags = %v, want [%s]", got.Tags, tg.ID)
	}
}
