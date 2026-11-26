// A discussion entry that was not saved is a failure, a peer that did not take
// one is reported, and a secure install sends content only to a peer that says
// it seals what it receives.

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

const discussionSecret = "SECRET-DISCUSSION-CONTENT"

// breakDiscussionLog makes every append to the discussion's log fail.
func breakDiscussionLog(t *testing.T, s *Server, id string) {
	t.Helper()
	dir, err := s.discussionFolderForID(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "wal.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func serveDiscussion(s *Server, method, path string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handleDiscussionScope(rr, httptest.NewRequest(method, path, bytes.NewReader(b)))
	return rr
}

func TestDiscussionReceiveFailsWhenTheEntryIsNotSaved(t *testing.T) {
	s := newSyncTestServer(t)
	breakDiscussionLog(t, s, "d1")
	entry, _ := json.Marshal(discussionWALEntry{Content: "x", Role: "discussion", OriginPeer: "other"})
	rr := serveDiscussion(s, http.MethodPost, "/api/memory/discussion/d1/receive", syncWritePayload{Message: string(entry)})
	if rr.Code != http.StatusInternalServerError || strings.Contains(rr.Body.String(), `"ok":true`) {
		t.Fatalf("status %d body %s, want an error", rr.Code, rr.Body.String())
	}
}

func TestDiscussionWriteFailsWhenTheLogIsNotSaved(t *testing.T) {
	resetThrottleMap()
	s := newSyncTestServer(t)
	breakDiscussionLog(t, s, "d1")
	rr := serveDiscussion(s, http.MethodPost, "/api/memory/discussion/d1", map[string]string{"content": "x"})
	if rr.Code != http.StatusInternalServerError || strings.Contains(rr.Body.String(), `"ok":true`) {
		t.Fatalf("status %d body %s, want an error", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "peers were not sent") {
		t.Errorf("body %q does not say what was lost", rr.Body.String())
	}
}

func TestDiscussionConflictResolveFailsWhenTheMarkerIsNotSaved(t *testing.T) {
	s := newSyncTestServer(t)
	first, err := s.discussionAppendWALEntry("d1", "same text", "discussion", "a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.discussionAppendWALEntry("d1", "same text", "discussion", "b", nil); err != nil {
		t.Fatal(err)
	}
	dir, _ := s.discussionFolderForID("d1")
	wal := filepath.Join(dir, "wal.jsonl")
	if err := os.Chmod(wal, 0o400); err != nil {
		t.Fatal(err)
	}
	if f, err := os.OpenFile(wal, os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		_ = f.Close()
		t.Skip("this user can write a read-only file")
	}
	rr := serveDiscussion(s, http.MethodPost, "/api/memory/discussion/d1/conflicts/resolve", map[string]int{"seq": first.Seq})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status %d body %s, want an error", rr.Code, rr.Body.String())
	}
}

// discussionPeer is a fake sibling daemon.
type discussionPeer struct {
	mu       sync.Mutex
	posts    []string
	probes   []string // Authorization headers of the sealing questions
	sealing  string   // answer to GET .../receive; "" answers 405 like an older daemon
	postCode int
}

func (p *discussionPeer) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.Method == http.MethodGet {
		p.probes = append(p.probes, r.Header.Get("Authorization"))
		if p.sealing == "" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write([]byte(p.sealing))
		return
	}
	body, _ := io.ReadAll(r.Body)
	p.posts = append(p.posts, string(body))
	if p.postCode != 0 {
		w.WriteHeader(p.postCode)
	}
}

func (p *discussionPeer) posted() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.posts)
}

func syncRig(t *testing.T, p *discussionPeer) (*Server, *alerts.Store) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(ts.Close)
	s := newSyncTestServer(t)
	peers, err := multiserver.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := peers.Add(&multiserver.Entry{Name: "sib", URL: ts.URL, Token: "tok", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s.serverStore = peers
	al, err := alerts.NewStore(filepath.Join(t.TempDir(), "alerts.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.alertStore = al
	return s, al
}

func waitAlerts(t *testing.T, al *alerts.Store, n int) []*alerts.Alert {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for len(al.List()) < n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	got := al.List()
	if len(got) != n {
		t.Fatalf("%d alerts, want %d: %+v", len(got), n, got)
	}
	return got
}

func syncOnce(t *testing.T, s *Server, peer string) {
	t.Helper()
	if err := s.discussionWriteParticipants("d1", []string{peer}, s.encKey); err != nil {
		t.Fatal(err)
	}
	s.discussionSyncToParticipants("d1", discussionWALEntry{Content: discussionSecret, Role: "discussion"})
}

func TestDiscussionSyncFailureIsReportedOnce(t *testing.T) {
	p := &discussionPeer{postCode: http.StatusInternalServerError}
	s, al := syncRig(t, p)
	syncOnce(t, s, "sib")
	a := waitAlerts(t, al, 1)[0]
	text := a.Title + " " + a.Body
	if !strings.Contains(text, "d1") || !strings.Contains(text, "sib") || !strings.Contains(text, "500") {
		t.Errorf("alert %q does not name the discussion, the peer and the status", text)
	}
	if strings.Contains(text, discussionSecret) {
		t.Errorf("alert carries the content: %q", text)
	}
	// The same failure again raises nothing new.
	syncOnce(t, s, "sib")
	waitFor2 := time.Now().Add(2 * time.Second)
	for p.posted() < 2 && time.Now().Before(waitFor2) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	waitAlerts(t, al, 1)
}

func TestDiscussionSyncToAnUnknownPeerIsReported(t *testing.T) {
	s, al := syncRig(t, &discussionPeer{})
	syncOnce(t, s, "nobody")
	if a := waitAlerts(t, al, 1)[0]; !strings.Contains(a.Body, "nobody") {
		t.Errorf("alert %q does not name the peer", a.Body)
	}
}

func TestSecureDiscussionContentIsNotSentToAPeerThatDoesNotSeal(t *testing.T) {
	for name, answer := range map[string]string{
		"an older daemon that cannot answer": "",
		"a peer that says it does not seal":  `{"sealed":false}`,
		"an answer without the field":        `{"status":"ok"}`,
		"an unreadable answer":               `<html>`,
	} {
		t.Run(name, func(t *testing.T) {
			p := &discussionPeer{sealing: answer}
			s, al := syncRig(t, p)
			s.encKey = bytes.Repeat([]byte{7}, 32)
			syncOnce(t, s, "sib")
			a := waitAlerts(t, al, 1)[0]
			if n := p.posted(); n != 0 {
				t.Fatalf("%d posts to a peer that did not say it seals", n)
			}
			if !strings.Contains(a.Body, "seal") || strings.Contains(a.Title+a.Body, discussionSecret) {
				t.Errorf("alert = %q %q", a.Title, a.Body)
			}
		})
	}
}

func TestSecureDiscussionContentIsSentToAPeerThatSeals(t *testing.T) {
	p := &discussionPeer{sealing: `{"sealed":true}`}
	s, al := syncRig(t, p)
	s.encKey = bytes.Repeat([]byte{7}, 32)
	syncOnce(t, s, "sib")
	deadline := time.Now().Add(3 * time.Second)
	for p.posted() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.posts) != 1 || !strings.Contains(p.posts[0], discussionSecret) {
		t.Fatalf("posts = %q, want the entry once", p.posts)
	}
	if len(p.probes) != 1 || p.probes[0] != "Bearer tok" {
		t.Errorf("sealing questions = %q, want one with the peer's token", p.probes)
	}
	if len(al.List()) != 0 {
		t.Errorf("alerts = %+v", al.List())
	}
}

// An install that is not secure sends as before, without asking.
func TestPlainDiscussionSyncAsksNothing(t *testing.T) {
	p := &discussionPeer{}
	s, _ := syncRig(t, p)
	syncOnce(t, s, "sib")
	deadline := time.Now().Add(3 * time.Second)
	for p.posted() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.posts) != 1 || len(p.probes) != 0 {
		t.Fatalf("posts %d, questions %d; want 1 and 0", len(p.posts), len(p.probes))
	}
}

func TestDiscussionReceiveSaysWhetherItSeals(t *testing.T) {
	for _, secure := range []bool{false, true} {
		s := newSyncTestServer(t)
		if secure {
			s.encKey = bytes.Repeat([]byte{7}, 32)
		}
		rr := serveDiscussion(s, http.MethodGet, "/api/memory/discussion/d1/receive", nil)
		var out map[string]any
		if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &out) != nil || out["sealed"] != secure {
			t.Errorf("secure=%v: status %d body %s", secure, rr.Code, rr.Body.String())
		}
	}
}
