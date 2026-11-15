// Instance-level .mcp.json management tests — fakes for the minter and
// the clock, t.TempDir() for every data directory, never the operator's
// data or home directory.

package channel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// noBridge pins BridgePath to empty so the JS-bridge code path is
// deterministic regardless of other tests mutating the package var.
func noBridge(t *testing.T) {
	t.Helper()
	prev := channelBinPathForReg
	channelBinPathForReg = ""
	t.Cleanup(func() { channelBinPathForReg = prev })
}

type fakeMintCall struct {
	ownerID string
	caps    []string
	ttl     time.Duration
}

// fakeMinter records calls and hands out fixed tokens from its sequence
// (falling back to tok-N).
type fakeMinter struct {
	mu          sync.Mutex
	tokens      []string
	err         error
	mintCalls   []fakeMintCall
	revokeErr   error
	revokeCalls []string
}

func (f *fakeMinter) MintScoped(ownerID string, caps []string, ttl time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mintCalls = append(f.mintCalls, fakeMintCall{ownerID: ownerID, caps: caps, ttl: ttl})
	if f.err != nil {
		return "", f.err
	}
	idx := len(f.mintCalls) - 1
	if idx < len(f.tokens) {
		return f.tokens[idx], nil
	}
	return "tok-" + string(rune('0'+idx)), nil
}

func (f *fakeMinter) Revoke(ownerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revokeCalls = append(f.revokeCalls, ownerID)
	return f.revokeErr
}

// fakeClock is a mutable pinned clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// readInstanceMCP reads and decodes <dir>/.mcp.json.
func readInstanceMCP(t *testing.T, dir string) MCPProjectConfig {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var cfg MCPProjectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode .mcp.json: %v", err)
	}
	return cfg
}

// writeInstanceMCP pre-writes a .mcp.json with the given servers.
func writeInstanceMCP(t *testing.T, dir string, servers map[string]MCPServerSpec) {
	t.Helper()
	raw, err := json.Marshal(MCPProjectConfig{MCPServers: servers})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
}

const sentinelLongLivedToken = "SENTINEL-LONG-LIVED-DATAWATCH-TOKE" // never written; test-only

func TestInstanceMCPConfig_RefreshWritesScopedTokenFile(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()
	minter := &fakeMinter{tokens: []string{"scoped-token-1"}}
	cfg := &InstanceMCPConfig{
		DataDir:       dir,
		ChannelJSPath: "/path/to/channel.js",
		BaseEnv:       map[string]string{"FOO": "bar"},
		Caps:          []string{"cap-a", "cap-b"},
		Minter:        minter,
	}
	updated, err := cfg.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Error("expected updated=true on first refresh")
	}

	// File mode is 0600.
	info, err := os.Stat(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("file mode = %o, want 0600", got)
	}

	m := readInstanceMCP(t, dir)
	dw, ok := m.MCPServers[ProjectMCPEntryName]
	if !ok {
		t.Fatalf("datawatch entry missing: %+v", m)
	}
	if got := dw.Env["DATAWATCH_TOKEN"]; got != "scoped-token-1" {
		t.Errorf("DATAWATCH_TOKEN = %q, want the minted scoped token", got)
	}
	if got := dw.Env["FOO"]; got != "bar" {
		t.Errorf("BaseEnv FOO = %q, want bar", got)
	}

	// The file never contains the sentinel long-lived token.
	raw, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if got := string(raw); strings.Contains(got, sentinelLongLivedToken) {
		t.Error("file contains the sentinel long-lived token — must never be written")
	}

	// Mint was called with the instance owner id, the caps, and the
	// default TTL.
	minter.mu.Lock()
	defer minter.mu.Unlock()
	if len(minter.mintCalls) != 1 {
		t.Fatalf("mint calls = %d, want 1", len(minter.mintCalls))
	}
	c := minter.mintCalls[0]
	if c.ownerID != InstanceTokenOwnerID {
		t.Errorf("ownerID = %q, want %q", c.ownerID, InstanceTokenOwnerID)
	}
	if len(c.caps) != 2 || c.caps[0] != "cap-a" || c.caps[1] != "cap-b" {
		t.Errorf("caps = %v, want [cap-a cap-b]", c.caps)
	}
	if c.ttl != InstanceTokenTTL {
		t.Errorf("ttl = %v, want %v (default)", c.ttl, InstanceTokenTTL)
	}
}

func TestInstanceMCPConfig_RefreshReusesTokenWithinHalfTTL(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()
	fc := &fakeClock{now: time.Unix(1_000_000, 0).UTC()}
	minter := &fakeMinter{}
	cfg := &InstanceMCPConfig{
		DataDir:       dir,
		ChannelJSPath: "/path/to/channel.js",
		Minter:        minter,
		clock:         fc.Now,
	}

	_, err := cfg.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	minter.mu.Lock()
	count := len(minter.mintCalls)
	minter.mu.Unlock()
	if count != 1 {
		t.Fatalf("mint calls = %d, want 1", count)
	}

	// Half the TTL minus a second: still more than half left — reuse,
	// no new mint.
	fc.set(fc.now.Add(InstanceTokenTTL/2 - time.Second))
	if _, err := cfg.Refresh(); err != nil {
		t.Fatal(err)
	}
	minter.mu.Lock()
	count = len(minter.mintCalls)
	minter.mu.Unlock()
	if count != 1 {
		t.Fatalf("mint calls after reuse refresh = %d, want 1 (still more than half the TTL left)", count)
	}

	// The file was rewritten on the reuse refresh as well (idempotent
	// skip only when env matches — here env matches, so no rewrite is
	// fine either way; the token on disk must still be the first one).
	m := readInstanceMCP(t, dir)
	if got := m.MCPServers[ProjectMCPEntryName].Env["DATAWATCH_TOKEN"]; got != tokOf(minter, 0) {
		t.Errorf("token on disk = %q, want the reused first token %q", got, tokOf(minter, 0))
	}

	// After half the TTL has elapsed: re-mint.
	fc.set(fc.now.Add(InstanceTokenTTL/2 + time.Second))
	if _, err := cfg.Refresh(); err != nil {
		t.Fatal(err)
	}
	minter.mu.Lock()
	count = len(minter.mintCalls)
	minter.mu.Unlock()
	if count != 2 {
		t.Fatalf("mint calls after TTL/2 elapse = %d, want 2 (re-mint)", count)
	}
	m = readInstanceMCP(t, dir)
	if got := m.MCPServers[ProjectMCPEntryName].Env["DATAWATCH_TOKEN"]; got == tokOf(minter, 0) {
		t.Error("token on disk is still the first token after re-mint; want the fresh one")
	}
}

func tokOf(f *fakeMinter, i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i < len(f.tokens) {
		return f.tokens[i]
	}
	return "tok-" + string(rune('0'+i))
}

func TestInstanceMCPConfig_RefreshRewritesFileWhenDeleted(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()
	fc := &fakeClock{now: time.Unix(1_000_000, 0).UTC()}
	cfg := &InstanceMCPConfig{
		DataDir:       dir,
		ChannelJSPath: "/path/to/channel.js",
		Minter:        &fakeMinter{tokens: []string{"reused-tok"}},
		clock:         fc.Now,
	}
	if _, err := cfg.Refresh(); err != nil {
		t.Fatal(err)
	}
	// Someone deleted the file; a refresh within half the TTL must
	// rewrite it with the same token (not a fresh mint).
	if err := os.Remove(filepath.Join(dir, ".mcp.json")); err != nil {
		t.Fatal(err)
	}
	fc.set(fc.now.Add(time.Minute))
	updated, err := cfg.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Error("expected updated=true when re-creating a deleted file")
	}
	m := readInstanceMCP(t, dir)
	if got := m.MCPServers[ProjectMCPEntryName].Env["DATAWATCH_TOKEN"]; got != "reused-tok" {
		t.Errorf("token = %q, want the reused token", got)
	}
}

func TestInstanceMCPConfig_RefreshMintErrorWritesNothingAndRemovesStale(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()
	// Stale datawatch entry carrying an old (possibly expired) token,
	// plus an operator-added server that must survive.
	writeInstanceMCP(t, dir, map[string]MCPServerSpec{
		ProjectMCPEntryName: {Command: "/bin/old", Env: map[string]string{"DATAWATCH_TOKEN": "stale-old-token"}},
		"operator-svc":      {Command: "/bin/operator"},
	})
	minter := &fakeMinter{err: errors.New("minter unavailable")}
	cfg := &InstanceMCPConfig{
		DataDir:       dir,
		ChannelJSPath: "/path/to/channel.js",
		Minter:        minter,
	}
	if _, err := cfg.Refresh(); err == nil {
		t.Fatal("expected a mint error to propagate")
	}
	minter.mu.Lock()
	if len(minter.mintCalls) != 1 {
		t.Fatalf("mint calls = %d, want 1", len(minter.mintCalls))
	}
	minter.mu.Unlock()

	// No stale credential stays on disk — the datawatch entry (and its
	// old token) is gone — but the operator's entry survives.
	m := readInstanceMCP(t, dir)
	if _, ok := m.MCPServers[ProjectMCPEntryName]; ok {
		t.Errorf("stale datawatch entry survived a mint error: %+v", m)
	}
	if _, ok := m.MCPServers["operator-svc"]; !ok {
		t.Errorf("operator entry was removed: %+v", m)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if got := string(raw); contains(got, "stale-old-token") {
		t.Error("stale token still present on disk after mint error")
	}
}

func TestInstanceMCPConfig_RefreshMintErrorDeletesFileWhenOnlyEntry(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()
	writeInstanceMCP(t, dir, map[string]MCPServerSpec{
		ProjectMCPEntryName: {Command: "/bin/old", Env: map[string]string{"DATAWATCH_TOKEN": "stale-old-token"}},
	})
	cfg := &InstanceMCPConfig{
		DataDir:       dir,
		ChannelJSPath: "/path/to/channel.js",
		Minter:        &fakeMinter{err: errors.New("minter unavailable")},
	}
	if _, err := cfg.Refresh(); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("file with only the stale datawatch entry should be deleted, got stat err=%v", err)
	}
}

func TestInstanceMCPConfig_RefreshNilMinterErrors(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()
	cfg := &InstanceMCPConfig{
		DataDir:       dir,
		ChannelJSPath: "/path/to/channel.js",
		// Minter is nil on purpose.
	}
	if _, err := cfg.Refresh(); err == nil {
		t.Fatal("expected an error when Minter is nil")
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Error("no file may be written when the minter is nil")
	}
}

func TestInstanceMCPConfig_ReleaseRevokesAndRemovesKeepingOperator(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()
	writeInstanceMCP(t, dir, map[string]MCPServerSpec{
		ProjectMCPEntryName: {Command: "/bin/dw", Env: map[string]string{"DATAWATCH_TOKEN": "scoped-token-1"}},
		"operator-svc":      {Command: "/bin/operator"},
	})
	minter := &fakeMinter{}
	cfg := &InstanceMCPConfig{DataDir: dir, Minter: minter}
	if err := cfg.Release(); err != nil {
		t.Fatal(err)
	}
	minter.mu.Lock()
	if len(minter.revokeCalls) != 1 || minter.revokeCalls[0] != InstanceTokenOwnerID {
		t.Errorf("revoke calls = %v, want [%s]", minter.revokeCalls, InstanceTokenOwnerID)
	}
	minter.mu.Unlock()

	m := readInstanceMCP(t, dir)
	if _, ok := m.MCPServers[ProjectMCPEntryName]; ok {
		t.Errorf("datawatch entry survived Release: %+v", m)
	}
	if _, ok := m.MCPServers["operator-svc"]; !ok {
		t.Errorf("operator entry was removed by Release: %+v", m)
	}
}

func TestInstanceMCPConfig_ReleaseDeletesFileWhenOnlyEntry(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()
	minter := &fakeMinter{tokens: []string{"scoped-token-1"}}
	cfg := &InstanceMCPConfig{
		DataDir:       dir,
		ChannelJSPath: "/path/to/channel.js",
		Minter:        minter,
	}
	if _, err := cfg.Refresh(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("file with only the datawatch entry should be deleted, got stat err=%v", err)
	}
	minter.mu.Lock()
	if len(minter.revokeCalls) != 1 {
		t.Errorf("revoke calls = %d, want 1", len(minter.revokeCalls))
	}
	minter.mu.Unlock()
}

func TestInstanceMCPConfig_ReleaseTwiceIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	minter := &fakeMinter{}
	cfg := &InstanceMCPConfig{DataDir: dir, Minter: minter}
	if err := cfg.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := cfg.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
	minter.mu.Lock()
	// The real store is idempotent; the fake records both calls — the
	// point is neither Release errors.
	if len(minter.revokeCalls) == 0 {
		t.Error("expected Release to call Revoke")
	}
	minter.mu.Unlock()
}

func TestInstanceMCPConfig_ReleaseWithoutMinterStillRemoves(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()
	writeInstanceMCP(t, dir, map[string]MCPServerSpec{
		ProjectMCPEntryName: {Command: "/bin/dw"},
	})
	cfg := &InstanceMCPConfig{DataDir: dir} // nil Minter
	if err := cfg.Release(); err != nil {
		t.Fatalf("Release with nil Minter should still clean up, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}
}

func TestInstanceMCPConfig_ReleaseRevokedThenRefreshRemints(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()
	minter := &fakeMinter{}
	cfg := &InstanceMCPConfig{
		DataDir:       dir,
		ChannelJSPath: "/path/to/channel.js",
		Minter:        minter,
	}
	if _, err := cfg.Refresh(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Release(); err != nil {
		t.Fatal(err)
	}
	minter.mu.Lock()
	first := tokOfLocked(minter, 0)
	countBefore := len(minter.mintCalls)
	minter.mu.Unlock()

	// After Release the in-memory token is dropped, so the next
	// Refresh must mint again — even immediately (no half-TTL reuse of
	// a revoked token).
	if _, err := cfg.Refresh(); err != nil {
		t.Fatal(err)
	}
	minter.mu.Lock()
	if len(minter.mintCalls) != countBefore+1 {
		t.Errorf("mint calls = %d, want %d (re-mint after revoke)", len(minter.mintCalls), countBefore+1)
	}
	minter.mu.Unlock()
	m := readInstanceMCP(t, dir)
	if got := m.MCPServers[ProjectMCPEntryName].Env["DATAWATCH_TOKEN"]; got == first {
		t.Errorf("token after re-mint is still the revoked one: %q", got)
	}
}

func tokOfLocked(f *fakeMinter, i int) string {
	if i < len(f.tokens) {
		return f.tokens[i]
	}
	return "tok-" + string(rune('0'+i))
}

func TestScrubInstanceMCPToken_RemovesLegacyTokenEntry(t *testing.T) {
	dir := t.TempDir()
	// Migration scenario (throwaway dir only): an earlier daemon held
	// its long-lived server token in the instance config.
	const legacy = "LEGACY-DAEMON-SERVER-TOKEN"
	writeInstanceMCP(t, dir, map[string]MCPServerSpec{
		ProjectMCPEntryName: {Command: "/bin/dw", Env: map[string]string{"DATAWATCH_TOKEN": legacy}},
	})
	removed, err := ScrubInstanceMCPToken(dir, "unrelated", legacy, "") // empty value ignored
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected removed=true when the entry carries a legacy token")
	}
	// Only entry — file must be gone.
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("file with only the scrubbed entry should be deleted, got stat err=%v", err)
	}
}

func TestScrubInstanceMCPToken_KeeperEntrySurvivesScrub(t *testing.T) {
	dir := t.TempDir()
	writeInstanceMCP(t, dir, map[string]MCPServerSpec{
		ProjectMCPEntryName: {Command: "/bin/dw", Env: map[string]string{"DATAWATCH_TOKEN": "LEGACY"}},
		"operator-svc":      {Command: "/bin/operator"},
	})
	removed, err := ScrubInstanceMCPToken(dir, "LEGACY")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected removed=true")
	}
	m := readInstanceMCP(t, dir)
	if _, ok := m.MCPServers[ProjectMCPEntryName]; ok {
		t.Errorf("datawatch entry survived scrub: %+v", m)
	}
	if _, ok := m.MCPServers["operator-svc"]; !ok {
		t.Errorf("operator entry was removed by scrub: %+v", m)
	}
}

func TestScrubInstanceMCPToken_DifferentTokenUntouched(t *testing.T) {
	dir := t.TempDir()
	writeInstanceMCP(t, dir, map[string]MCPServerSpec{
		ProjectMCPEntryName: {Command: "/bin/dw", Env: map[string]string{"DATAWATCH_TOKEN": "current-scoped-token"}},
	})
	rawBefore, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	infoBefore, _ := os.Stat(filepath.Join(dir, ".mcp.json"))
	removed, err := ScrubInstanceMCPToken(dir, "some-other-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("entry with a non-legacy token must be left alone")
	}
	rawAfter, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	infoAfter, _ := os.Stat(filepath.Join(dir, ".mcp.json"))
	if string(rawBefore) != string(rawAfter) || infoBefore.ModTime() != infoAfter.ModTime() {
		t.Error("file was modified although no legacy token was present")
	}
}

func TestScrubInstanceMCPToken_MissingAndUnparsableFile(t *testing.T) {
	dir := t.TempDir()
	if removed, err := ScrubInstanceMCPToken(dir, "any"); err != nil || removed {
		t.Errorf("missing file: removed=%v err=%v, want false, nil", removed, err)
	}
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, ".mcp.json"), []byte("not json at all"), 0600); err != nil {
		t.Fatal(err)
	}
	if removed, err := ScrubInstanceMCPToken(bad, "any"); err != nil || removed {
		t.Errorf("unparsable file: removed=%v err=%v, want false, nil", removed, err)
	}
}

func TestWriteInstanceMCPConfig_RewritesWhenOnlyTokenDiffers(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()

	// First write.
	updated, err := WriteInstanceMCPConfig(dir, "/path/to/channel.js", map[string]string{"DATAWATCH_TOKEN": "token-A"})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Error("expected updated=true on first write")
	}

	// Same token — idempotent skip still works.
	updated, err = WriteInstanceMCPConfig(dir, "/path/to/channel.js", map[string]string{"DATAWATCH_TOKEN": "token-A"})
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Error("same command+args+env must skip the rewrite")
	}

	// Only the token differs — this MUST rewrite (the stale-skip fix
	// for rotating scoped tokens).
	updated, err = WriteInstanceMCPConfig(dir, "/path/to/channel.js", map[string]string{"DATAWATCH_TOKEN": "token-B"})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Error("expected updated=true when only env DATAWATCH_TOKEN differs")
	}
	m := readInstanceMCP(t, dir)
	if got := m.MCPServers[ProjectMCPEntryName].Env["DATAWATCH_TOKEN"]; got != "token-B" {
		t.Errorf("token on disk = %q, want token-B", got)
	}

	// nil env vs the on-disk token-B entry differs — rewrite.
	updated, err = WriteInstanceMCPConfig(dir, "/path/to/channel.js", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Error("expected updated=true: nil env differs from the token-B env")
	}
	m = readInstanceMCP(t, dir)
	if got := m.MCPServers[ProjectMCPEntryName].Env["DATAWATCH_TOKEN"]; got != "" {
		t.Errorf("token on disk = %q, want gone (nil env write)", got)
	}
}

func TestWriteInstanceMCPConfig_NilAndEmptyEnvEqual(t *testing.T) {
	fakeNode(t)
	noBridge(t)
	dir := t.TempDir()
	if updated, err := WriteInstanceMCPConfig(dir, "/path/to/channel.js", nil); err != nil || !updated {
		t.Fatalf("first write: updated=%v err=%v", updated, err)
	}
	// Empty map: equal to the nil env already on disk — skip.
	if updated, err := WriteInstanceMCPConfig(dir, "/path/to/channel.js", map[string]string{}); err != nil || updated {
		t.Fatalf("empty==nil must skip: updated=%v err=%v", updated, err)
	}
	// And back to nil — still skipped.
	if updated, err := WriteInstanceMCPConfig(dir, "/path/to/channel.js", nil); err != nil || updated {
		t.Fatalf("nil after empty must skip: updated=%v err=%v", updated, err)
	}
}
