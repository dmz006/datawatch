package multiserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestStore_AddGetDelete(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	entry := &Entry{Name: "prod", URL: "http://prod.example.com:8080", Enabled: true}
	if err := s.Add(entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok := s.Get("prod")
	if !ok {
		t.Fatal("Get: entry not found after Add")
	}
	if got.URL != "http://prod.example.com:8080" {
		t.Errorf("URL = %q, want %q", got.URL, "http://prod.example.com:8080")
	}
	if got.Builtin {
		t.Error("runtime entry should not have Builtin=true")
	}

	list := s.List()
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	if err := s.Delete("prod"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get("prod"); ok {
		t.Error("entry still present after Delete")
	}
}

func TestStore_ConflictOnDuplicate(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	e := &Entry{Name: "dup", URL: "http://a.example.com"}
	if err := s.Add(e); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if err := s.Add(e); err != ErrConflict {
		t.Errorf("second Add: got %v, want ErrConflict", err)
	}
}

func TestStore_BuiltinCannotDelete(t *testing.T) {
	dir := t.TempDir()
	seeds := []config.RemoteServerConfig{
		{Name: "yaml-server", URL: "http://yaml.example.com", Enabled: true},
	}
	s, err := NewStore(dir, seeds)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Builtin entry should be visible in the list.
	got, ok := s.Get("yaml-server")
	if !ok {
		t.Fatal("builtin entry not found")
	}
	if !got.Builtin {
		t.Error("builtin entry should have Builtin=true")
	}

	// Attempting to delete a builtin must fail.
	if err := s.Delete("yaml-server"); err != ErrBuiltin {
		t.Errorf("Delete builtin: got %v, want ErrBuiltin", err)
	}
}

func TestStore_BuiltinCannotUpdate(t *testing.T) {
	dir := t.TempDir()
	seeds := []config.RemoteServerConfig{
		{Name: "yaml-server", URL: "http://yaml.example.com", Enabled: true},
	}
	s, err := NewStore(dir, seeds)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	updated := &Entry{Name: "yaml-server", URL: "http://changed.example.com", Enabled: false}
	if err := s.Update("yaml-server", updated); err != ErrBuiltin {
		t.Errorf("Update builtin: got %v, want ErrBuiltin", err)
	}
}

func TestStore_Persist(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	if err := s.Add(&Entry{Name: "srv1", URL: "http://srv1.example.com", Enabled: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Reload from disk.
	s2, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("reload NewStore: %v", err)
	}
	got, ok := s2.Get("srv1")
	if !ok {
		t.Fatal("entry not found after reload")
	}
	if got.URL != "http://srv1.example.com" {
		t.Errorf("persisted URL = %q, want %q", got.URL, "http://srv1.example.com")
	}
	// Make sure builtins are NOT persisted to disk.
	storePath := filepath.Join(dir, "servers.json")
	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	if string(data) == "null" || len(data) == 0 {
		t.Error("store file is empty after Add")
	}
}

func TestStore_BuiltinsNotPersisted(t *testing.T) {
	dir := t.TempDir()
	seeds := []config.RemoteServerConfig{
		{Name: "builtin1", URL: "http://builtin1.example.com", Enabled: true},
	}
	// Create with a seed.
	_, err := NewStore(dir, seeds)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Reload without any seeds — builtin should NOT reappear.
	s2, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := s2.Get("builtin1"); ok {
		t.Error("builtin entry was incorrectly persisted to disk")
	}
}

func TestStore_Test(t *testing.T) {
	// Spin up a tiny test HTTP server that returns a health response AND
	// a real authenticated-endpoint response -- Test() now requires both.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"version":"7.0.0-smoke"}`)) //nolint:errcheck
		case "/api/sessions":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`)) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(&Entry{Name: "smoke", URL: srv.URL, Enabled: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	latency, version, err := s.Test(context.Background(), "smoke")
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if latency < 0 {
		t.Errorf("latency %d < 0", latency)
	}
	if version != "7.0.0-smoke" {
		t.Errorf("version = %q, want %q", version, "7.0.0-smoke")
	}
}

// Operator-reported 2026-10-08: Test() used to report success against a
// server whose token was missing/wrong, because it only ever checked
// /api/health -- deliberately public, never actually exercising the
// token. This pins the fix: a healthy-but-unauthenticated remote must
// surface as a real error, not a green "latency Nms" result.
func TestStore_Test_FalsePositiveOnBadToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			// Public endpoint: responds fine regardless of auth, same as
			// every real datawatch daemon.
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"version":"7.0.0-smoke"}`)) //nolint:errcheck
		case "/api/sessions":
			// Authenticated endpoint: rejects the bad/missing token.
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(&Entry{Name: "bad-token", URL: srv.URL, Enabled: true, Token: "wrong-token"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	_, version, err := s.Test(context.Background(), "bad-token")
	if err == nil {
		t.Fatal("Test: expected an error for a server with a bad token, got nil (false positive)")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error = %q, want it to mention authentication failure", err.Error())
	}
	// The version should still come through -- the health check alone
	// isn't wrong, it's just not sufficient on its own for "success".
	if version != "7.0.0-smoke" {
		t.Errorf("version = %q, want %q (health info should still be reported even on auth failure)", version, "7.0.0-smoke")
	}
}

// Operator-reported 2026-10-08 (peer-d): Test() against a peer with a
// self-signed certificate used to fail with "x509: certificate signed by
// unknown authority" for every caller, with no way to opt a specific,
// trusted peer out of verification. TLSSkipVerify fixes this per-entry
// (every other peer keeps full verification).
func TestStore_Test_TLSSkipVerify(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"version":"7.0.0-selfsigned"}`)) //nolint:errcheck
		case "/api/sessions":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`)) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	if err := s.Add(&Entry{Name: "strict", URL: srv.URL, Enabled: true}); err != nil {
		t.Fatalf("Add(strict): %v", err)
	}
	if _, _, err := s.Test(context.Background(), "strict"); err == nil {
		t.Fatal("Test(strict): expected a certificate-verification error against a self-signed server with TLSSkipVerify=false, got nil")
	} else if !strings.Contains(err.Error(), "certificate") && !strings.Contains(err.Error(), "x509") {
		t.Errorf("Test(strict) error = %q, want it to mention the certificate problem", err.Error())
	}

	if err := s.Add(&Entry{Name: "trusted", URL: srv.URL, Enabled: true, TLSSkipVerify: true}); err != nil {
		t.Fatalf("Add(trusted): %v", err)
	}
	_, version, err := s.Test(context.Background(), "trusted")
	if err != nil {
		t.Fatalf("Test(trusted): expected TLSSkipVerify=true to succeed against the same self-signed server, got: %v", err)
	}
	if version != "7.0.0-selfsigned" {
		t.Errorf("version = %q, want %q", version, "7.0.0-selfsigned")
	}
}

func TestStore_TestNotFound(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	_, _, err = s.Test(context.Background(), "nonexistent")
	if err != ErrNotFound {
		t.Errorf("Test nonexistent: got %v, want ErrNotFound", err)
	}
}

func TestStore_UpdateNotFound(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	err = s.Update("ghost", &Entry{Name: "ghost", URL: "http://ghost.example.com"})
	if err != ErrNotFound {
		t.Errorf("Update nonexistent: got %v, want ErrNotFound", err)
	}
}

func TestStore_DeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Delete("ghost"); err != ErrNotFound {
		t.Errorf("Delete nonexistent: got %v, want ErrNotFound", err)
	}
}

func TestStore_AddRequiresNameAndURL(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(&Entry{}); err == nil {
		t.Error("Add with empty entry: expected error")
	}
	if err := s.Add(&Entry{Name: "no-url"}); err == nil {
		t.Error("Add with no URL: expected error")
	}
}

// GetByToken resolves which federated peer is making a request --
// fedAuthMiddleware's identity check for every authenticated federation
// call. Found with zero test coverage during a 2026-10-08 testing audit
// prompted by the Store.Test() false-positive bug (same package, same
// auth-adjacent theme). The implementation itself is already correct
// (constant-time compare, explicit empty-token guard) -- this locks that
// behavior in so a future "simplification" can't quietly drop either
// property.
func TestStore_GetByToken(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(&Entry{Name: "peer-a", URL: "http://a.example.com", Enabled: true, Token: "token-a"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(&Entry{Name: "peer-b", URL: "http://b.example.com", Enabled: true, Token: "token-b"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	e, ok := s.GetByToken("token-a")
	if !ok || e.Name != "peer-a" {
		t.Errorf("GetByToken(token-a) = %v, %v; want peer-a, true", e, ok)
	}

	if _, ok := s.GetByToken("token-nonexistent"); ok {
		t.Error("GetByToken with an unregistered token: expected no match, got one")
	}

	// The critical regression case: an entry with no token configured
	// (the exact real-world state that caused today's live federation
	// bugs -- a peer added without a token) must never match an empty
	// bearer value. Without this guard, a caller presenting NO token at
	// all would be resolved as that peer's identity.
	if err := s.Add(&Entry{Name: "peer-no-token", URL: "http://c.example.com", Enabled: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, ok := s.GetByToken(""); ok {
		t.Error("GetByToken(\"\") matched a token-less entry -- empty bearer values must never resolve to a peer identity")
	}
}

// TestFingerprintSyncDefaultsOff — the per-peer fingerprint-exchange opt-in
// must be OFF unless explicitly set: a fresh Entry and a JSON-persisted
// entry that predates the field (or was created without it) both read
// back false. Opt-in per peer is the operator decision recorded in
// docs/plans/2026-10-14-concurrency-and-budget-design.md (open question 10).
func TestFingerprintSyncDefaultsOff(t *testing.T) {
	e := &Entry{Name: "fresh", URL: "http://fresh.example.com:8080", Enabled: true}
	if e.FingerprintSync {
		t.Error("a new Entry must default to FingerprintSync=false")
	}

	// Simulate a pre-existing servers.json written before the field
	// existed: decode it into a fresh store via a persisted file.
	dir := t.TempDir()
	legacyJSON := `[{"name":"legacy","url":"http://legacy.example.com:8080","enabled":true,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]`
	dataPath := filepath.Join(dir, "servers.json")
	if err := os.WriteFile(dataPath, []byte(legacyJSON), 0o600); err != nil {
		t.Fatalf("write legacy JSON: %v", err)
	}
	s2, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	got, ok := s2.Get("legacy")
	if !ok {
		t.Fatal("legacy entry not found after reload")
	}
	if got.FingerprintSync {
		t.Error("an entry loaded from JSON without the field must read back FingerprintSync=false")
	}
}

// TestFingerprintSyncStoreRoundTrip — an explicitly set FingerprintSync
// survives Add/persist/reload and Update, and Redacted() keeps the value
// in a copy while leaving the token logic untouched.
func TestFingerprintSyncStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(&Entry{Name: "peer", URL: "http://peer.example.com:8080", Token: "tok", Enabled: true, FingerprintSync: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Redacted() must carry the flag across.
	eg, ok := s.Get("peer")
	if !ok {
		t.Fatal("Get after Add: not found")
	}
	if rd := eg.Redacted(); rd == nil || !rd.FingerprintSync {
		t.Error("Redacted() copy must keep FingerprintSync=true")
	} else if rd.Token != "" {
		t.Error("Redacted() copy must not keep the token")
	}

	// Update (partial-ish PUT merge is at the API layer; here a full store
	// replace) must persist the flag as sent.
	up, ok := s.Get("peer")
	if !ok {
		t.Fatal("Get before Update: not found")
	}
	up.FingerprintSync = false
	if err := s.Update("peer", up); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Reload from disk — the flag (both states) must round-trip.
	s2, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	re, ok := s2.Get("peer")
	if !ok {
		t.Fatal("reload: peer not found")
	}
	if re.FingerprintSync {
		t.Error("FingerprintSync=false must survive persist/reload")
	}
	re.FingerprintSync = true
	if err := s2.Update("peer", re); err != nil {
		t.Fatalf("Update to true: %v", err)
	}
	s3, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore reload 2: %v", err)
	}
	fin, ok := s3.Get("peer")
	if !ok {
		t.Fatal("reload 2: peer not found")
	}
	if !fin.FingerprintSync {
		t.Error("FingerprintSync=true must survive persist/reload")
	}
}

func TestStore_PinRoundTrip(t *testing.T) {
	fp := strings.Repeat("c3", 32)
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// An uppercase pin must be normalized to lowercase hex on Add.
	if err := s.Add(&Entry{Name: "pinned", URL: "https://pinned.example.com:8080", Enabled: true, TLSCertFingerprint: strings.ToUpper(fp)}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := s.Get("pinned")
	if !ok {
		t.Fatal("Get: entry not found after Add")
	}
	if got.TLSCertFingerprint != fp {
		t.Fatalf("TLSCertFingerprint = %q, want normalized %q", got.TLSCertFingerprint, fp)
	}

	// The pin must survive persist/reload into a fresh store.
	s2, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	re, ok := s2.Get("pinned")
	if !ok {
		t.Fatal("reload: entry not found")
	}
	if re.TLSCertFingerprint != fp {
		t.Fatalf("reload: TLSCertFingerprint = %q, want %q", re.TLSCertFingerprint, fp)
	}
}

func TestStore_AddRejectsPinWithSkipVerify(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(&Entry{Name: "both", URL: "https://both.example.com", TLSSkipVerify: true, TLSCertFingerprint: strings.Repeat("d4", 32)}); err == nil {
		t.Fatal("Add: expected an error for pin + tls_skip_verify together")
	}
	if _, ok := s.Get("both"); ok {
		t.Fatal("rejected entry must not be stored")
	}
}

func TestStore_UpdatePinNormalizationAndRejection(t *testing.T) {
	fp := strings.Repeat("e5", 32)
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(&Entry{Name: "up", URL: "https://up.example.com", Enabled: true}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Update with an uppercase pin — normalized on the way in.
	if err := s.Update("up", &Entry{URL: "https://up.example.com", Enabled: true, TLSCertFingerprint: strings.ToUpper(fp)}); err != nil {
		t.Fatalf("Update(pin): %v", err)
	}
	got, ok := s.Get("up")
	if !ok {
		t.Fatal("Get after Update: not found")
	}
	if got.TLSCertFingerprint != fp {
		t.Fatalf("TLSCertFingerprint = %q, want normalized %q", got.TLSCertFingerprint, fp)
	}

	// Update with both a pin and skip-verify must be rejected and leave
	// the stored entry unchanged.
	if err := s.Update("up", &Entry{URL: "https://up.example.com", TLSSkipVerify: true, TLSCertFingerprint: strings.Repeat("f6", 32)}); err == nil {
		t.Fatal("Update: expected an error for pin + tls_skip_verify together")
	}
	if got.TLSSkipVerify || got.TLSCertFingerprint != fp {
		t.Fatalf("rejected Update mutated the entry: %+v", got)
	}
}

func TestStore_Test_Pinned(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"version":"7.0.0-pinned"}`)) //nolint:errcheck
		case "/api/sessions":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`)) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	sum := sha256.Sum256(srv.Certificate().Raw)
	fp := hex.EncodeToString(sum[:])

	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// A pin matching the self-signed leaf must pass Test(); this is the
	// point of pinning — trusted peer without disabling verification.
	if err := s.Add(&Entry{Name: "pinned", URL: srv.URL, Enabled: true, TLSCertFingerprint: fp}); err != nil {
		t.Fatalf("Add(pinned): %v", err)
	}
	_, version, err := s.Test(context.Background(), "pinned")
	if err != nil {
		t.Fatalf("Test(pinned): expected the matching pin to succeed, got: %v", err)
	}
	if version != "7.0.0-pinned" {
		t.Errorf("version = %q, want %q", version, "7.0.0-pinned")
	}

	// A wrong pin must fail closed against the same server.
	if err := s.Add(&Entry{Name: "badpin", URL: srv.URL, Enabled: true, TLSCertFingerprint: strings.Repeat("0a", 32)}); err != nil {
		t.Fatalf("Add(badpin): %v", err)
	}
	if _, _, err := s.Test(context.Background(), "badpin"); err == nil {
		t.Fatal("Test(badpin): expected a fingerprint-mismatch error, got nil")
	}

	// No pin and no skip-verify still fails against the self-signed cert.
	if err := s.Add(&Entry{Name: "strict", URL: srv.URL, Enabled: true}); err != nil {
		t.Fatalf("Add(strict): %v", err)
	}
	if _, _, err := s.Test(context.Background(), "strict"); err == nil {
		t.Fatal("Test(strict): expected a certificate-verification error, got nil")
	}
}

// A servers.json written before TLSCertFingerprint existed (no such
// field in the JSON) must load unchanged: every pre-existing entry keeps
// its values and the new field simply reads empty.
func TestStore_Migration_LegacyStoreFile(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte(`[
  {
    "name": "legacy",
    "url": "https://legacy.example.com:8080",
    "token": "abc123",
    "enabled": true,
    "tls_skip_verify": true,
    "fingerprint_sync": true,
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-01-02T00:00:00Z"
  }
]
`)
	if err := os.WriteFile(filepath.Join(dir, "servers.json"), legacy, 0o600); err != nil {
		t.Fatalf("write legacy store: %v", err)
	}

	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore over legacy file: %v", err)
	}
	got, ok := s.Get("legacy")
	if !ok {
		t.Fatal("legacy entry did not load")
	}
	if got.URL != "https://legacy.example.com:8080" {
		t.Errorf("URL = %q, unchanged value expected", got.URL)
	}
	if got.Token != "abc123" {
		t.Errorf("Token = %q, unchanged value expected", got.Token)
	}
	if !got.Enabled || !got.TLSSkipVerify || !got.FingerprintSync {
		t.Errorf("legacy flags changed: %+v", got)
	}
	if got.TLSCertFingerprint != "" {
		t.Errorf("TLSCertFingerprint = %q, want empty for a pre-change entry", got.TLSCertFingerprint)
	}
	if got.Builtin {
		t.Error("legacy entry must not read as builtin")
	}
}
