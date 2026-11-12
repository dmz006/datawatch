package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// snapshotTree records every path under root, with whether it is a directory,
// so two snapshots can be compared for new files or folders.
func snapshotTree(t *testing.T, root string) map[string]bool {
	t.Helper()
	snap := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		snap[p] = d.IsDir()
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return snap
}

// TestDiscussionRoutesRejectTraversalIDs serves every discussion route with a
// set of invalid ids, straight through the handler functions (the mux would
// clean ".." paths before a handler ran), and checks each is refused with a
// plain-text 400 and leaves the state directory and file service root
// untouched.
func TestDiscussionRoutesRejectTraversalIDs(t *testing.T) {
	routes := []struct {
		name   string
		method string
		prefix string // path before the id
		suffix string // path after the id
		files  bool   // served by handleFilesDiscussion
	}{
		{name: "recall", method: http.MethodGet, prefix: "/api/memory/discussion/"},
		{name: "write", method: http.MethodPost, prefix: "/api/memory/discussion/"},
		{name: "delete", method: http.MethodDelete, prefix: "/api/memory/discussion/"},
		{name: "wal", method: http.MethodGet, prefix: "/api/memory/discussion/", suffix: "/wal"},
		{name: "participants", method: http.MethodGet, prefix: "/api/memory/discussion/", suffix: "/participants"},
		{name: "conflicts", method: http.MethodGet, prefix: "/api/memory/discussion/", suffix: "/conflicts"},
		{name: "conflicts-resolve", method: http.MethodPost, prefix: "/api/memory/discussion/", suffix: "/conflicts/resolve"},
		{name: "receive", method: http.MethodPost, prefix: "/api/memory/discussion/", suffix: "/receive"},
		{name: "files", method: http.MethodGet, prefix: "/api/files/discussions/", files: true},
	}

	badIDs := []struct {
		name string
		id   string
	}{
		{"dot", "."},
		{"dotdot", ".."},
		{"forward-slash", "a/b"},
		{"backslash", `a\b`},
		{"nul-byte", "a\x00b"},
		{"too-long", strings.Repeat("a", 129)},
		{"space", "a b"},
	}

	for _, rt := range routes {
		for _, bad := range badIDs {
			t.Run(rt.name+"/"+bad.name, func(t *testing.T) {
				dataDir := t.TempDir()
				fileRoot := t.TempDir()
				s := &Server{
					memoryBackend: &fakeDiscussionBackend{},
					dataDir:       dataDir,
					cfg: &config.Config{
						Session: config.SessionConfig{FileServiceRoot: fileRoot},
					},
				}

				dataBefore := snapshotTree(t, dataDir)
				filesBefore := snapshotTree(t, fileRoot)

				// Build the request with a safe path, then set the path
				// directly so ids the URL parser would refuse still arrive.
				req := httptest.NewRequest(rt.method, "/placeholder", strings.NewReader("{}"))
				req.URL.Path = rt.prefix + bad.id + rt.suffix
				req.URL.RawPath = ""
				rr := httptest.NewRecorder()

				if rt.files {
					s.handleFilesDiscussion(rr, req)
				} else {
					s.handleDiscussionScope(rr, req)
				}

				if rr.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400; body=%q", rr.Code, rr.Body.String())
				}
				if got := strings.TrimSpace(rr.Body.String()); got != "invalid discussion id" {
					t.Fatalf("body = %q, want %q", got, "invalid discussion id")
				}
				if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
					t.Errorf("content-type = %q, want text/plain", ct)
				}

				if after := snapshotTree(t, dataDir); !reflect.DeepEqual(dataBefore, after) {
					t.Errorf("state dir changed:\nbefore=%v\nafter=%v", dataBefore, after)
				}
				if after := snapshotTree(t, fileRoot); !reflect.DeepEqual(filesBefore, after) {
					t.Errorf("file service root changed:\nbefore=%v\nafter=%v", filesBefore, after)
				}
			})
		}
	}
}
