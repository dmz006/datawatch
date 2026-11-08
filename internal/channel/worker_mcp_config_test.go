package channel

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/workerprofile"
)

func testServers() map[string]MCPServerSpec {
	return map[string]MCPServerSpec{
		"datawatch": {Command: "/usr/local/bin/datawatch-mcp", Env: map[string]string{"DATAWATCH_TOKEN": "t"}},
		"github":    {Command: "npx", Args: []string{"@modelcontextprotocol/server-github"}},
		"fs":        {Command: "npx", Args: []string{"@modelcontextprotocol/server-filesystem"}, Env: map[string]string{"ROOT": "/tmp"}},
	}
}

func TestWriteWorkerMCPConfig_None(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteWorkerMCPConfig(dir, workerprofile.MCPSelection{Mode: workerprofile.MCPNone}, testServers())
	if err != nil {
		t.Fatalf("WriteWorkerMCPConfig: %v", err)
	}
	if want := filepath.Join(dir, WorkerMCPConfigFileName); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var cfg MCPProjectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.MCPServers == nil {
		t.Fatal("mcpServers must be present (empty object), not null")
	}
	if len(cfg.MCPServers) != 0 {
		t.Fatalf("none mode must have zero servers, got %d: %v", len(cfg.MCPServers), cfg.MCPServers)
	}
}

func TestWriteWorkerMCPConfig_Datawatch(t *testing.T) {
	dir := t.TempDir()
	sel := workerprofile.MCPSelection{Mode: workerprofile.MCPDatawatch}
	path, err := WriteWorkerMCPConfig(dir, sel, testServers())
	if err != nil {
		t.Fatalf("WriteWorkerMCPConfig: %v", err)
	}
	var cfg MCPProjectConfig
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("datawatch mode must keep exactly one server, got %d", len(cfg.MCPServers))
	}
	spec, ok := cfg.MCPServers[ProjectMCPEntryName]
	if !ok {
		t.Fatalf("expected entry %q, got %v", ProjectMCPEntryName, cfg.MCPServers)
	}
	if spec.Command != "/usr/local/bin/datawatch-mcp" {
		t.Fatalf("command = %q, want /usr/local/bin/datawatch-mcp", spec.Command)
	}
}

func TestWriteWorkerMCPConfig_Datawatch_MissingEntry(t *testing.T) {
	// "datawatch" absent from servers: the intersection is empty; the
	// config must contain no servers rather than error.
	dir := t.TempDir()
	_, err := WriteWorkerMCPConfig(dir, workerprofile.MCPSelection{Mode: workerprofile.MCPDatawatch},
		map[string]MCPServerSpec{"github": {Command: "npx"}})
	if err != nil {
		t.Fatalf("WriteWorkerMCPConfig: %v", err)
	}
	path := filepath.Join(dir, WorkerMCPConfigFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var cfg MCPProjectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.MCPServers) != 0 {
		t.Fatalf("expected 0 servers, got %d", len(cfg.MCPServers))
	}
}

func TestWriteWorkerMCPConfig_List(t *testing.T) {
	dir := t.TempDir()
	sel := workerprofile.MCPSelection{Mode: workerprofile.MCPList, Servers: []string{"github", "fs"}}
	path, err := WriteWorkerMCPConfig(dir, sel, testServers())
	if err != nil {
		t.Fatalf("WriteWorkerMCPConfig: %v", err)
	}
	var cfg MCPProjectConfig
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.MCPServers) != 2 {
		t.Fatalf("list mode must keep exactly the named servers, got %d: %v", len(cfg.MCPServers), cfg.MCPServers)
	}
	for _, name := range []string{"github", "fs"} {
		if _, ok := cfg.MCPServers[name]; !ok {
			t.Fatalf("expected entry %q, got %v", name, cfg.MCPServers)
		}
	}
	if _, ok := cfg.MCPServers[ProjectMCPEntryName]; ok {
		t.Fatal("datawatch entry must not be present in list mode")
	}
	if got := cfg.MCPServers["github"].Args; len(got) != 1 || got[0] != "@modelcontextprotocol/server-github" {
		t.Fatalf("github args = %v", got)
	}
}

func TestWriteWorkerMCPConfig_List_MissingServers(t *testing.T) {
	servers := testServers()
	delete(servers, "fs")
	sel := workerprofile.MCPSelection{Mode: workerprofile.MCPList, Servers: []string{"fs", "nope"}}
	dir := t.TempDir()
	_, err := WriteWorkerMCPConfig(dir, sel, servers)
	if err == nil {
		t.Fatal("expected an error for missing servers")
	}
	// Every missing name must be named, in a stable order.
	if !strings.Contains(err.Error(), "fs") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error must name every missing server, got: %v", err)
	}
	if i, j := strings.Index(err.Error(), "fs"), strings.Index(err.Error(), "nope"); i > j {
		t.Fatalf("missing names should be sorted: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, WorkerMCPConfigFileName)); !os.IsNotExist(statErr) {
		t.Fatal("no file must be written when servers are missing")
	}
}

func TestWriteWorkerMCPConfig_ModeAndPermissions(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteWorkerMCPConfig(dir, workerprofile.MCPSelection{Mode: workerprofile.MCPDatawatch}, testServers())
	if err != nil {
		t.Fatalf("WriteWorkerMCPConfig: %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := st.Mode().Perm(); got != 0600 {
		t.Fatalf("mode = %o, want 0600", got)
	}
}

func TestWriteWorkerMCPConfig_BodyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteWorkerMCPConfig(dir, workerprofile.MCPSelection{Mode: workerprofile.MCPList, Servers: []string{"fs"}}, testServers())
	if err != nil {
		t.Fatalf("WriteWorkerMCPConfig: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := json.Unmarshal(b, &MCPProjectConfig{}); err != nil {
		t.Fatalf("file %q is not valid JSON: %v\n%s", path, err, b)
	}
	// Parse back and compare against the input spec.
	var cfg MCPProjectConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	want := testServers()["fs"]
	if got := cfg.MCPServers["fs"]; got.Command != want.Command || got.Args[0] != want.Args[0] || got.Env["ROOT"] != want.Env["ROOT"] {
		t.Fatalf("parsed server = %+v, want %+v", got, want)
	}
}

func TestWriteWorkerMCPConfig_DeterministicBytes(t *testing.T) {
	sel := workerprofile.MCPSelection{Mode: workerprofile.MCPList, Servers: []string{"fs", "github", "datawatch"}}
	servers := testServers()

	d1 := t.TempDir()
	d2 := t.TempDir()
	p1, err := WriteWorkerMCPConfig(d1, sel, servers)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	p2, err := WriteWorkerMCPConfig(d2, sel, servers)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	b1, err := os.ReadFile(p1)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := os.ReadFile(p2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("two writes with the same input differ:\n%s\n---\n%s", b1, b2)
	}
}

func TestWriteWorkerMCPConfig_RequiresDir(t *testing.T) {
	if _, err := WriteWorkerMCPConfig("", workerprofile.MCPSelection{Mode: workerprofile.MCPNone}, nil); err == nil {
		t.Fatal("expected an error for empty dir")
	}
}
