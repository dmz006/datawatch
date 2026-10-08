// SEC-022 — PUT /api/skills/registries/{name}/sync: a skill whose manifest
// Verify command failed must be rolled back (Unsync) by default, and kept
// when the caller passes trust_unverified.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/skills"
)

// fakeSkillsManager is a minimal skillsManager implementation for testing
// the sync REST handler's rollback-on-unverified behavior in isolation,
// without needing a real git clone.
type fakeSkillsManager struct {
	syncResult    []*skills.Synced
	unsyncedNames []string
}

func (f *fakeSkillsManager) Store() *skills.Store                             { return nil }
func (f *fakeSkillsManager) AddDefault() error                                { return nil }
func (f *fakeSkillsManager) AddBuiltinDefaults() error                        { return nil }
func (f *fakeSkillsManager) Connect(string) ([]*skills.AvailableSkill, error) { return nil, nil }
func (f *fakeSkillsManager) Browse(string) ([]*skills.AvailableSkill, error)  { return nil, nil }
func (f *fakeSkillsManager) Sync(registry string, names []string) ([]*skills.Synced, error) {
	return f.syncResult, nil
}
func (f *fakeSkillsManager) Unsync(registry string, names []string) ([]string, error) {
	f.unsyncedNames = append(f.unsyncedNames, names...)
	return names, nil
}
func (f *fakeSkillsManager) LoadSkillContent(string) (string, error)  { return "", nil }
func (f *fakeSkillsManager) RegistryCachePath(string) (string, error) { return "", nil }

func newSkillsSyncTestServer(t *testing.T, result []*skills.Synced) (*Server, *fakeSkillsManager) {
	t.Helper()
	f := &fakeSkillsManager{syncResult: result}
	s := &Server{skillsMgr: f}
	return s, f
}

func postSkillsSync(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/skills/registries/test-reg/sync", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	s.handleSkillsRegistries(rr, req)
	return rr
}

func TestHandleSkillsSync_RollsBackUnverifiedByDefault(t *testing.T) {
	result := []*skills.Synced{
		{Registry: "test-reg", Name: "good", Verified: true},
		{Registry: "test-reg", Name: "bad", Verified: false, VerifyError: "missing dependency"},
	}
	s, f := newSkillsSyncTestServer(t, result)

	rr := postSkillsSync(t, s, map[string]any{"all": true})
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(f.unsyncedNames) != 1 || f.unsyncedNames[0] != "bad" {
		t.Fatalf("expected the unverified skill to be rolled back, got unsynced=%v", f.unsyncedNames)
	}

	var resp struct {
		Synced            []*skills.Synced `json:"synced"`
		RefusedUnverified []*skills.Synced `json:"refused_unverified"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Synced) != 1 || resp.Synced[0].Name != "good" {
		t.Errorf("expected only 'good' in synced, got %+v", resp.Synced)
	}
	if len(resp.RefusedUnverified) != 1 || resp.RefusedUnverified[0].Name != "bad" {
		t.Errorf("expected 'bad' reported in refused_unverified, got %+v", resp.RefusedUnverified)
	}
}

func TestHandleSkillsSync_TrustUnverifiedKeepsEverything(t *testing.T) {
	result := []*skills.Synced{
		{Registry: "test-reg", Name: "good", Verified: true},
		{Registry: "test-reg", Name: "bad", Verified: false, VerifyError: "missing dependency"},
	}
	s, f := newSkillsSyncTestServer(t, result)

	rr := postSkillsSync(t, s, map[string]any{"all": true, "trust_unverified": true})
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(f.unsyncedNames) != 0 {
		t.Fatalf("trust_unverified must keep every skill, got rolled back: %v", f.unsyncedNames)
	}

	var resp struct {
		Synced []*skills.Synced `json:"synced"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Synced) != 2 {
		t.Errorf("expected both skills kept with trust_unverified, got %+v", resp.Synced)
	}
}
