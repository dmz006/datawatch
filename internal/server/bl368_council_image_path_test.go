// BL368 Phase 4 — council run image_path field unit tests.
//
// TC-1: image_path + visioner wired → description prepended to proposal
// TC-2: image_path file not found → HTTP 400
// TC-3: image_path with visioner error → HTTP 500

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/council"
)

// fakeCouncilOrchestratorForImage is a minimal stub that captures the Run() proposal.
type fakeCouncilOrchestratorForImage struct {
	capturedProposal string
}

func (f *fakeCouncilOrchestratorForImage) Personas() []council.Persona { return nil }
func (f *fakeCouncilOrchestratorForImage) GetPersona(string) (council.Persona, error) {
	return council.Persona{}, nil
}
func (f *fakeCouncilOrchestratorForImage) AddPersona(council.Persona) error    { return nil }
func (f *fakeCouncilOrchestratorForImage) UpdatePersona(string, council.Persona) error { return nil }
func (f *fakeCouncilOrchestratorForImage) RemovePersona(string) error          { return nil }
func (f *fakeCouncilOrchestratorForImage) RestoreDefaultPersona(string) error  { return nil }
func (f *fakeCouncilOrchestratorForImage) Run(proposal string, names []string, mode council.Mode) (*council.Run, error) {
	f.capturedProposal = proposal
	return &council.Run{ID: "test-run-id"}, nil
}
func (f *fakeCouncilOrchestratorForImage) LoadRun(string) (*council.Run, error) {
	return &council.Run{ID: "test-run-id"}, nil
}
func (f *fakeCouncilOrchestratorForImage) ListRuns(int) ([]*council.Run, error) { return nil, nil }
func (f *fakeCouncilOrchestratorForImage) Cancel(string) bool                  { return false }

// TC-1: image_path + visioner wired → description prepended to proposal.
func TestBL368_CouncilRun_ImagePath_PrependedToProposal(t *testing.T) {
	dir := t.TempDir()
	imgFile := filepath.Join(dir, "test.png")
	// Minimal PNG bytes (1x1 pixel)
	pngBytes := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")
	if err := os.WriteFile(imgFile, pngBytes, 0600); err != nil {
		t.Fatal(err)
	}

	orch := &fakeCouncilOrchestratorForImage{}
	vis := &fakeVisioner{result: "a red door on a white wall"}
	srv := &Server{councilOrch: orch, visioner: vis}

	bodyStr := `{"proposal":"What colour is the door?","image_path":"` + imgFile + `","async":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/council/run", strings.NewReader(bodyStr))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleCouncilRun(rr, req)

	if rr.Code == http.StatusServiceUnavailable {
		t.Skip("council disabled in this server config")
	}
	if rr.Code >= 500 {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(orch.capturedProposal, "[image:") {
		t.Errorf("proposal should contain image description; got: %q", orch.capturedProposal)
	}
	if !strings.Contains(orch.capturedProposal, "a red door on a white wall") {
		t.Errorf("proposal should contain visioner description; got: %q", orch.capturedProposal)
	}
	if !strings.Contains(orch.capturedProposal, "What colour is the door?") {
		t.Errorf("original proposal should be preserved; got: %q", orch.capturedProposal)
	}
}

// TC-2: image_path file not found → HTTP 400.
func TestBL368_CouncilRun_ImagePath_FileNotFound_Returns400(t *testing.T) {
	orch := &fakeCouncilOrchestratorForImage{}
	vis := &fakeVisioner{result: "some description"}
	srv := &Server{councilOrch: orch, visioner: vis}

	body := `{"proposal":"test proposal","image_path":"/nonexistent/path/ts-bl368.png"}`
	req := httptest.NewRequest(http.MethodPost, "/api/council/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleCouncilRun(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing file, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "image_path") {
		t.Errorf("error should mention image_path; got: %s", rr.Body.String())
	}
}

// TC-3: visioner returns error → HTTP 500.
func TestBL368_CouncilRun_ImagePath_VisionerError_Returns500(t *testing.T) {
	dir := t.TempDir()
	imgFile := filepath.Join(dir, "err.png")
	if err := os.WriteFile(imgFile, []byte("not-a-real-image"), 0600); err != nil {
		t.Fatal(err)
	}

	orch := &fakeCouncilOrchestratorForImage{}
	vis := &fakeVisioner{err: os.ErrClosed}
	srv := &Server{councilOrch: orch, visioner: vis}

	bodyStr := `{"proposal":"test","image_path":"` + imgFile + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/council/run", strings.NewReader(bodyStr))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleCouncilRun(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 for visioner error, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "image description failed") {
		t.Errorf("want 'image description failed' in error; got: %s", rr.Body.String())
	}
}
