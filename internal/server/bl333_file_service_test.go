// BL333 (v8.3.0 T43) — Federated File Service unit tests.
//
//   TS-701: GET /api/files/meta returns valid JSON with root field
//   TS-702: POST multipart upload then DELETE — file lifecycle
//   TS-703: GET /api/files/peers/{name} returns correct subdirectory

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// bl333Server builds a minimal Server rooted at a temp directory.
func bl333Server(t *testing.T, root string) *Server {
	t.Helper()
	return &Server{
		cfg: &config.Config{
			Session: config.SessionConfig{
				FileServiceRoot: root,
			},
		},
	}
}

// TestFileMeta_Empty — GET /api/files/meta on a fresh root returns valid JSON
// with root, peers, and discussions fields.
func TestFileMeta_Empty(t *testing.T) {
	root := t.TempDir()
	s := bl333Server(t, root)

	req := httptest.NewRequest(http.MethodGet, "/api/files/meta", nil)
	rr := httptest.NewRecorder()
	s.handleFilesMeta(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var data map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := data["root"]; !ok {
		t.Error("response missing 'root' field")
	}
	if _, ok := data["peers"]; !ok {
		t.Error("response missing 'peers' field")
	}
	if _, ok := data["discussions"]; !ok {
		t.Error("response missing 'discussions' field")
	}
	if data["root"] != filepath.Clean(root) {
		t.Errorf("root=%q want %q", data["root"], filepath.Clean(root))
	}
}

// TestFilesUpload_And_Delete — multipart POST creates a file, then DELETE removes it.
func TestFilesUpload_And_Delete(t *testing.T) {
	root := t.TempDir()
	s := bl333Server(t, root)

	destPath := filepath.Join(root, "hello.txt")

	// Build multipart body.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("path", destPath); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(fw, "hello world") //nolint:errcheck
	mw.Close()                    //nolint:errcheck

	req := httptest.NewRequest(http.MethodPost, "/api/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	s.handleFilesUpload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", rr.Code, rr.Body.String())
	}

	// Verify file exists on disk.
	if _, err := os.Stat(destPath); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// Now DELETE it.
	delBody := strings.NewReader(fmt.Sprintf(`{"path":%q}`, destPath))
	delReq := httptest.NewRequest(http.MethodDelete, "/api/files", delBody)
	delReq.Header.Set("Content-Type", "application/json")
	delRR := httptest.NewRecorder()
	s.handleFilesDelete(delRR, delReq)

	if delRR.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delRR.Code, delRR.Body.String())
	}

	// Verify file gone.
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Error("file still exists after delete")
	}
}

// TestFilesUpload_ImageFile — multipart POST with a JPEG file returns path + bytes,
// mirrors the PWA image-attachment flow (POST /api/files with accept="image/*").
func TestFilesUpload_ImageFile(t *testing.T) {
	root := t.TempDir()
	s := bl333Server(t, root)

	destPath := filepath.Join(root, "dw_attach_1234_photo.jpg")

	// Minimal 1×1 JPEG header (enough to test the handler; not a valid image decode).
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("path", destPath); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(jpegData); err != nil {
		t.Fatal(err)
	}
	mw.Close() //nolint:errcheck

	req := httptest.NewRequest(http.MethodPost, "/api/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	s.handleFilesUpload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["path"] != destPath {
		t.Errorf("path=%q want %q", resp["path"], destPath)
	}
	gotBytes, _ := resp["bytes"].(float64)
	if int(gotBytes) != len(jpegData) {
		t.Errorf("bytes=%v want %d", resp["bytes"], len(jpegData))
	}
	// File must exist on disk.
	if fi, err := os.Stat(destPath); err != nil || fi.Size() != int64(len(jpegData)) {
		t.Errorf("file on disk wrong: stat=%v size=%v", err, fi)
	}
}

// TestFilesUpload_PathTraversal — upload attempt with a path outside the root is rejected.
func TestFilesUpload_PathTraversal(t *testing.T) {
	root := t.TempDir()
	s := bl333Server(t, root)

	escapePath := "/tmp/evil_file.txt"

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("path", escapePath)
	fw, _ := mw.CreateFormFile("file", "evil.txt")
	_, _ = fw.Write([]byte("evil"))
	mw.Close() //nolint:errcheck

	req := httptest.NewRequest(http.MethodPost, "/api/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	s.handleFilesUpload(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for path traversal, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestFilesPeer_Subdir — GET /api/files/peers/{name} auto-creates and lists
// the peer subdirectory.
func TestFilesPeer_Subdir(t *testing.T) {
	root := t.TempDir()
	s := bl333Server(t, root)

	// Pre-create a file in the peer directory so the listing is non-empty.
	peerDir := filepath.Join(root, "peers", "test-peer")
	if err := os.MkdirAll(peerDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(peerDir, "note.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/files/peers/test-peer", nil)
	req.URL.Path = "/api/files/peers/test-peer" // ensure path is set
	rr := httptest.NewRecorder()
	s.handleFilesPeer(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var data map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	entries, ok := data["entries"].([]interface{})
	if !ok {
		t.Fatal("entries not an array")
	}
	if len(entries) == 0 {
		t.Error("expected at least one entry")
	}
	found := false
	for _, e := range entries {
		if m, ok := e.(map[string]interface{}); ok {
			if m["name"] == "note.txt" {
				found = true
			}
		}
	}
	if !found {
		t.Error("note.txt not found in peer directory listing")
	}
}
