// Lean MCP end-to-end test with a fake MCP server.
//
// The lean supervisor launch pins the session with --strict-mcp-config --mcp-config
// worker-mcp.json and ENABLE_TOOL_SEARCH=true. This test closes the loop nobody
// exercised before: it actually STARTS the MCP server configured in worker-mcp.json
// (a fake server) and asks it for its tool list, then asserts a fake claude-code
// binary would see the datawatch tools.
//
// How the fake server works: the test binary re-executes itself. The MCP config
// entry's command is this process's own test executable (os.Executable) with
// -test.run=^TestLeanFakeMCPServer$, and TestLeanFakeMCPServer speaks
// newline-delimited JSON-RPC on stdio when the LEAN_FAKE_MCP_TEST_SERVER env set
// in the config's "env" block is present; otherwise it skips. It handles
// "initialize", "notifications/initialized" (no response), and "tools/list".
//
// The test also writes a fake claude binary (a shell script) that honours the
// --mcp-config flag exactly like the real binary would: it starts the configured
// server, sends initialize / initialized / tools/list, and records the tool names
// it saw plus the ENABLE_TOOL_SEARCH flag into a result file. The parent test
// then runs the recorded launch command with bash and checks the result.

package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// leanFakeMCPToolNames is the datawatch tool set the fake MCP server advertises
// — the tools a lean supervisor worker is expected to reach through ToolSearch.
var leanFakeMCPToolNames = []string{
	"memory_recall",
	"question_list",
	"supervisor_watch_stats",
	"usage_backoff_set",
}

// leanFakeMCPServerEnv is set on the fake server's env (via worker-mcp.json's
// "env" block) and tells the re-executed test binary to serve JSON-RPC instead
// of skipping.
const leanFakeMCPServerEnv = "LEAN_FAKE_MCP_TEST_SERVER"

// fakeClaudeScript is the shell fake that stands in for the claude binary. It
// resolves --mcp-config, starts the configured server, plays a minimal MCP
// client (initialize / initialized / tools/list) and records what it observed.
func fakeClaudeScript() string {
	return `#!/bin/sh
# fake claude-code for the LeanFakeMCP test.
set -u

mcp_config=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--mcp-config" ]; then mcp_config="$a"; fi
  prev="$a"
done

result="${LEAN_FAKE_MCP_RESULT_FILE:-}"
if [ -z "$result" ]; then
  echo 'fake claude: LEAN_FAKE_MCP_RESULT_FILE is required' >&2
  exit 3
fi

if [ -z "$mcp_config" ] || [ ! -f "$mcp_config" ]; then
  printf 'TOOLS=\nENABLE_TOOL_SEARCH=%s\nSERVER_RC=2\n' "${ENABLE_TOOL_SEARCH:-}" >"$result"
  exit 0
fi

command=$(sed -n 's/.*"command"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$mcp_config" | sed -n 1p)
set --
while IFS= read -r line; do
  [ -n "$line" ] && set -- "$@" "$line"
done <<ARGEOF
$(sed -n '/"args"[[:space:]]*:/,/^[[:space:]]*\]/p' "$mcp_config" | sed '1d;$d' | sed 's/^[[:space:]]*"//; s/",\?[[:space:]]*$//' )
ARGEOF
envkv=""
while IFS= read -r kv; do
  [ -n "$kv" ] && envkv="$envkv $kv"
done <<ENVEOF
$(sed -n '/"env"[[:space:]]*:/,/^[[:space:]]*}/p' "$mcp_config" | sed '1d;$d' | sed -n 's/^[[:space:]]*"\([A-Za-z_][A-Za-z0-9_]*\)":[[:space:]]*"\([^"]*\)".*/\1=\2/p')
ENVEOF

reqs=$(mktemp); out=$(mktemp); errlog=$(mktemp)
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"fake-claude","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' >"$reqs"

timeout 30 env $envkv "$command" "$@" <"$reqs" >"$out" 2>"$errlog"
rc=$?

tools=$(grep '"tools"' "$out" 2>/dev/null | grep -o '"name":"[A-Za-z0-9_]*"' | sed 's/"name":"//; s/"$//' | tr '\n' ' ')
printf 'TOOLS=%s\nENABLE_TOOL_SEARCH=%s\nSERVER_RC=%s\nSERVER_ERR=%s\n' \
  "$tools" "${ENABLE_TOOL_SEARCH:-}" "$rc" "$(tail -n 1 "$errlog" 2>/dev/null)" >"$result"
exit 0
`
}

func writeFakeClaude(t *testing.T, dir string) string {
	t.Helper()
	resultFile := filepath.Join(dir, "fake-claude-result")
	script := filepath.Join(dir, "fake-claude")
	if err := os.WriteFile(script, []byte(fakeClaudeScript()), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake claude script: %v", err)
	}
	return resultFile
}

// leanFakeMCPResult parses the fake claude script's result file into a map.
func leanFakeMCPResult(t *testing.T, resultFile string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(resultFile)
	if err != nil {
		t.Fatalf("fake claude produced no result file: %v", err)
	}
	res := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("unparseable result line %q", line)
		}
		res[key] = val
	}
	return res
}

// launchLeanFakeMCP launches a lean supervisor session whose datawatch MCP
// server entry is serverSpec, runs the recorded launch command with bash
// against the fake claude binary, and returns the result the fake observed.
func launchLeanFakeMCP(t *testing.T, serverSpec channel.MCPServerSpec, session string) map[string]string {
	t.Helper()
	t.Setenv("TMUX", "")
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatalf("unset TMUX: %v", err)
	}

	p, _ := workerprofile.Resolve(workerprofile.ResolveInput{Role: "worker", Kind: workerprofile.KindSupervisor})
	if p.Name == "" {
		t.Fatal("no lean supervisor profile resolved")
	}
	w := WorkerProfileLaunch{
		Profile:    p,
		MCPServers: map[string]channel.MCPServerSpec{channel.ProjectMCPEntryName: serverSpec},
		Starting:   autonomous.StartingContextInput{Role: "worker", TaskSpec: "supervise the PRD"},
	}

	d := t.TempDir()
	resultFile := writeFakeClaude(t, d)
	b := New(filepath.Join(d, "fake-claude")).(*Backend)

	argv := startFakeTmux(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if reason, err := b.LaunchWithProfileFallback(ctx, w, "", session, d, "/nonexistent/log"); err != nil {
		t.Fatalf("lean launch: %v", err)
	} else if reason != "" {
		t.Fatalf("lean profile must apply, got fallback reason: %s", reason)
	}
	cmd := recordedCmd(t, argv)

	// Run what the fake tmux recorded against the fake claude binary.
	rctx, rcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer rcancel()
	run := exec.CommandContext(rctx, "bash", "-c", cmd)
	run.Env = append(os.Environ(), "LEAN_FAKE_MCP_RESULT_FILE="+resultFile)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run recorded launch command: %v\n%s", err, out)
	}
	t.Log(string(out))
	return leanFakeMCPResult(t, resultFile)
}

func assertToolSet(t *testing.T, res map[string]string, wantComplete bool) {
	t.Helper()
	tools := map[string]bool{}
	for _, n := range strings.Fields(res["TOOLS"]) {
		tools[n] = true
	}
	if wantComplete && res["SERVER_RC"] != "0" {
		t.Errorf("fake MCP server exited with rc %s (stderr: %s)", res["SERVER_RC"], res["SERVER_ERR"])
	}
	for _, want := range leanFakeMCPToolNames {
		if got := tools[want]; got != wantComplete {
			verb := "missing"
			if !wantComplete {
				verb = "served, want none"
			}
			t.Errorf("tool %q %s; fake claude saw %q, want %s", want, verb, res["TOOLS"], map[bool]string{true: "all four lean tools", false: "an empty tool list"}[wantComplete])
		}
	}
	if got := res["ENABLE_TOOL_SEARCH"]; got != "true" {
		t.Errorf("ENABLE_TOOL_SEARCH = %q, want true: the lean profile must always defer MCP tools behind ToolSearch", got)
	}
}

// TestLeanFakeMCPLaunch_ServesDatawatchTools: the lean supervisor launch
// configures a real (fake) MCP server in worker-mcp.json, the fake claude
// binary starts it, and the four datawatch tools come back over stdio.
func TestLeanFakeMCPLaunch_ServesDatawatchTools(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test binary path: %v", err)
	}
	spec := channel.MCPServerSpec{
		Command: exe,
		Args:    []string{"-test.run=^TestLeanFakeMCPServer$", "-test.count=1"},
		Env:     map[string]string{leanFakeMCPServerEnv: "1"},
	}
	res := launchLeanFakeMCP(t, spec, "cs-leanfake-ok")
	assertToolSet(t, res, true)
}

// TestLeanFakeMCPLaunch_WrongServerCommandYieldsNoTools: a worker-mcp.json whose
// server command does not exist must not crash the launch or the fake claude —
// it reports no tools instead (which is what "connected but tools not listed"
// looks like to the operator).
func TestLeanFakeMCPLaunch_WrongServerCommandYieldsNoTools(t *testing.T) {
	d := t.TempDir()
	spec := channel.MCPServerSpec{
		Command: filepath.Join(d, "no-such-mcp-server"),
		Args:    []string{"serve"},
	}
	res := launchLeanFakeMCP(t, spec, "cs-leanfake-bad")
	if res["SERVER_RC"] == "0" {
		t.Errorf("server rc = 0, want non-zero: the configured server command does not exist")
	}
	assertToolSet(t, res, false)
}

// TestLeanFakeMCPServer is the re-executed test binary itself. In server mode
// (LEAN_FAKE_MCP_TEST_SERVER set) it speaks newline-delimited JSON-RPC on
// stdio: "initialize" is answered with protocol metadata,
// "notifications/initialized" gets no response, and "tools/list" returns the
// lean datawatch tool set. In any other mode (e.g. a plain `go test` run of
// this package) it simply skips.
func TestLeanFakeMCPServer(t *testing.T) {
	if os.Getenv(leanFakeMCPServerEnv) == "" {
		t.Skip("not in fake MCP server mode (re-executed test binary); nothing to serve")
	}
	r := bufio.NewReader(os.Stdin)
	for {
		line, rerr := r.ReadString('\n')
		if strings.TrimSpace(line) != "" {
			if resp := leanFakeMCPHandleLine([]byte(line)); resp != "" {
				if _, werr := os.Stdout.WriteString(resp + "\n"); werr != nil {
					t.Errorf("write JSON-RPC response: %v", werr)
					return
				}
			}
		}
		if rerr != nil {
			if !errorsIsEOF(rerr) {
				t.Errorf("read JSON-RPC request: %v", rerr)
			}
			return
		}
	}
}

// errorsIsEOF reports whether err is io.EOF (the fake client closing the pipe
// after the tool list — the expected end of the conversation).
func errorsIsEOF(err error) bool { return err == io.EOF }

// leanFakeMCPHandleLine answers one newline-delimited JSON-RPC line and
// returns the response payload ("", never, for notifications).
func leanFakeMCPHandleLine(line []byte) string {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error"}}`
	}
	switch req.Method {
	case "initialize":
		return leanFakeMCPResponse(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "lean-fake-mcp", "version": "0.0.1"},
		})
	case "tools/list":
		tools := make([]any, 0, len(leanFakeMCPToolNames))
		for _, name := range leanFakeMCPToolNames {
			tools = append(tools, map[string]any{
				"name":        name,
				"description": "fake datawatch tool served by the lean fake MCP server",
				"inputSchema": map[string]any{"type": "object"},
			})
		}
		return leanFakeMCPResponse(req.ID, map[string]any{"tools": tools})
	default:
		if req.ID == nil || strings.HasPrefix(req.Method, "notifications/") {
			return "" // notifications never get a response
		}
		return leanFakeMCPError(req.ID, -32601, "method not found: "+req.Method)
	}
}

func leanFakeMCPResponse(id json.RawMessage, result any) string {
	out, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Result  any    `json:"result"`
	}{"2.0", id, result})
	if err != nil {
		return `{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`
	}
	return string(out)
}

func leanFakeMCPError(id json.RawMessage, code int, msg string) string {
	out, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{"2.0", id, struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{code, msg}})
	if err != nil {
		return `{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`
	}
	return string(out)
}
