package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func tlsPeer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sessions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"aa01","full_id":"peer.example.com-aa01"}]`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fetchViaTable(s *Server, tg federationTarget) ([]*session.Session, error) {
	return fetchRemoteSessions(s.peerClient(tg), tg.url, tg.token)
}

func TestFederationSessionsTLS_SkipVerifyTrue(t *testing.T) {
	srv := tlsPeer(t)
	s := &Server{}
	got, err := fetchViaTable(s, federationTarget{name: "peer-a", url: srv.URL, skipVerify: true})
	if err != nil || len(got) != 1 {
		t.Fatalf("want 1 session, got %v err %v", got, err)
	}
}

func TestFederationSessionsTLS_SkipVerifyFalse(t *testing.T) {
	srv := tlsPeer(t)
	s := &Server{}
	_, err := fetchViaTable(s, federationTarget{name: "peer-b", url: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "certificate signed by unknown authority") {
		t.Fatalf("want unknown authority error, got %v", err)
	}
}

func TestFederationSessionsTLS_PinnedMatch(t *testing.T) {
	srv := tlsPeer(t)
	sum := sha256.Sum256(srv.Certificate().Raw)
	s := &Server{}
	got, err := fetchViaTable(s, federationTarget{name: "peer-c", url: srv.URL, pinnedSHA256: hex.EncodeToString(sum[:])})
	if err != nil || len(got) != 1 {
		t.Fatalf("want 1 session, got %v err %v", got, err)
	}
}

func TestFederationSessionsTLS_PinnedMismatch(t *testing.T) {
	srv := tlsPeer(t)
	s := &Server{}
	_, err := fetchViaTable(s, federationTarget{name: "peer-d", url: srv.URL, pinnedSHA256: strings.Repeat("ab", 32)})
	if err == nil || !strings.HasPrefix(err.Error(), "transport: ") {
		t.Fatalf("want transport pin error, got %v", err)
	}
}

func TestFederationSessionsTLS_ClientCacheRebuildsOnKeyChange(t *testing.T) {
	s := &Server{}
	a := s.peerClient(federationTarget{name: "p", url: "https://a.example.com"})
	if b := s.peerClient(federationTarget{name: "p", url: "https://a.example.com"}); a != b {
		t.Fatal("expected cached client")
	}
	if c := s.peerClient(federationTarget{name: "p", url: "https://a.example.com", skipVerify: true}); a == c {
		t.Fatal("expected rebuilt client")
	}
}
