// State path coverage for discussion scopes: the WAL and participant
// state must live under the server's configured data directory and never
// under the process home directory, even when a decoy discussions tree
// sits in HOME.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatePathDiscussionWALUnderDataDir proves the single-source rule for
// discussion state: with a data directory configured, a discussion WAL entry
// written through the write handler lands under the data directory, the
// WAL read handler returns only that new entry, and a decoy
// $HOME/.datawatch/discussions/<id>/wal.jsonl is left byte-identical —
// i.e. the implementation never reads or writes the home directory.
func TestStatePathDiscussionWALUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()

	// Decoy HOME holding a discussions folder with a sentinel WAL file for
	// the very same discussion ID the server will write.
	decoyHome := t.TempDir()
	t.Setenv("HOME", decoyHome)
	const id = "state-path-decoy-id"
	decoyWAL := filepath.Join(decoyHome, ".datawatch", "discussions", id, "wal.jsonl")
	if err := os.MkdirAll(filepath.Dir(decoyWAL), 0755); err != nil {
		t.Fatalf("create decoy dir: %v", err)
	}
	sentinel := []byte(`{"seq":99,"content":"decoy sentinel — must never be touched","role":"user","timestamp":"2020-01-01T00:00:00Z"}` + "\n")
	if err := os.WriteFile(decoyWAL, sentinel, 0600); err != nil {
		t.Fatalf("write decoy wal: %v", err)
	}

	s := &Server{dataDir: dataDir}

	// Write one entry through the discussion write handler (POST route).
	body := map[string]any{"content": "real entry under the data dir", "role": "user"}
	bBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal write body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/memory/discussion/"+id, bytes.NewReader(bBody))
	rr := httptest.NewRecorder()
	s.handleDiscussionScope(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("write handler: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// The WAL must live under the data directory — never under HOME.
	realWAL := filepath.Join(dataDir, "discussions", id, "wal.jsonl")
	st, err := os.Stat(realWAL)
	if err != nil {
		t.Fatalf("wal file missing under data dir: %v", err)
	}
	if !st.Mode().IsRegular() {
		t.Fatalf("wal path %q is not a regular file", realWAL)
	}
	if strings.HasPrefix(realWAL, filepath.Clean(decoyHome)+string(os.PathSeparator)) {
		t.Fatalf("wal path %q must not be under the decoy HOME", realWAL)
	}

	// Read it back through the WAL read handler (GET .../wal): the response
	// must contain exactly the one entry just written.
	req2 := httptest.NewRequest(http.MethodGet, "/api/memory/discussion/"+id+"/wal", nil)
	rr2 := httptest.NewRecorder()
	s.handleDiscussionScope(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("wal read handler: expected 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}
	var resp struct {
		DiscussionID string `json:"discussion_id"`
		Entries      []struct {
			Seq     int    `json:"seq"`
			Content string `json:"content"`
			Role    string `json:"role"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode wal response: %v (body=%s)", err, rr2.Body.String())
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected exactly 1 WAL entry, got %d (body=%s)", len(resp.Entries), rr2.Body.String())
	}
	if resp.Entries[0].Content != "real entry under the data dir" {
		t.Errorf("entry content = %q, want the entry written under the data dir", resp.Entries[0].Content)
	}
	if resp.Entries[0].Seq != 0 {
		t.Errorf("entry seq = %d, want 0 (first entry of a fresh WAL)", resp.Entries[0].Seq)
	}

	// The decoy HOME file must be byte-identical: the code never read or
	// wrote the home directory.
	gotDecoy, err := os.ReadFile(decoyWAL)
	if err != nil {
		t.Fatalf("read decoy wal after test: %v", err)
	}
	if !bytes.Equal(gotDecoy, sentinel) {
		t.Errorf("decoy wal file under HOME was modified:\n got %q\nwant %q", gotDecoy, sentinel)
	}
}
