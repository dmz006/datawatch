// Supervisor watch host — the daemon-side checks the supervisor watch
// runs on each coordinator poll, with no model call. The checks read
// host facts through an injected probe (SupervisorWatchHostProbe) so
// tests run against fakes and no real command ever runs in them; the
// production probe (ShellSupervisorWatchHostProbe) may shell out, but
// it resolves every tool with `command -v` first and degrades to
// "unknown, no finding" when a fact cannot be read: a probe error
// means the check stays silent, never a guessed finding.
//
// Mirror of the repo CI watch structure: the findings these checks
// return are the []SupervisorWatchFinding the coordinator poll records
// in the triage store (supervisor_watch_record.go) and raises through
// the existing alert and notify channels when due. In shadow mode (the
// release default, SupervisorWatchConfig.EffectiveMode) the watch
// records only, never acts.
package autonomous

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

// SupervisorWatchTmuxUnitName — the user service unit of the local
// tmux server the watch's tmux_restart check compares against its
// stored baseline.
const SupervisorWatchTmuxUnitName = "datawatch-tmux"

// SupervisorWatchDaemonUnitName — the user service unit of the local
// Automaton daemon the daemon_inactive check asks about.
const SupervisorWatchDaemonUnitName = "datawatch"

// supervisorWatchBaselineSep separates the restart count and the
// active-since stamp in the stored tmux baseline. The unit-separator
// byte cannot appear in either field, so the join is lossless — the
// same convention as SupervisorWatchKey.
const supervisorWatchBaselineSep = "\x1f"

// SupervisorWatchHostProbe is the injected source of host facts for
// the supervisor watch checks:
//
//   - ServiceRestartCount + ActiveSince: the tmux unit's
//     NRestarts and ActiveEnterTimestamp (tmux_restart).
//
//   - DaemonActive: whether the Automaton daemon is active on a
//     registered node (daemon_inactive).
//
//   - ResidentModels: the models the registered node reports resident
//     (model_not_resident).
//
//   - GitStatus: `git status --short` lines for the main checkout
//     (main_checkout_dirty).
//
//   - FileMode: the file mode of a path (verify_script_not_executable).
//
// Every method returns an error when the fact is unknown (tool missing,
// node unreachable, path unreadable). The checks treat that as "unknown,
// no finding": an unknown fact must never produce a finding.
type SupervisorWatchHostProbe interface {
	// ServiceRestartCount returns the tmux unit's restart count.
	ServiceRestartCount() (int, error)
	// ActiveSince returns the tmux unit's ActiveEnterTimestamp stamp.
	ActiveSince() (string, error)
	// DaemonActive reports whether the Automaton daemon is active on
	// node. An error means there is no view of that daemon at all.
	DaemonActive(node string) (bool, error)
	// ResidentModels returns the models node reports resident. An
	// error means node unreachable or its resident list unreadable.
	ResidentModels(node string) ([]string, error)
	// GitStatus returns the `git status --short` lines of the main
	// checkout at dir. An error means git missing or dir not a repo.
	GitStatus(dir string) ([]string, error)
	// FileMode returns the mode bits of path, like os.Stat. A
	// os.IsNotExist error must be preserved so callers can tell a
	// missing file from a degraded probe.
	FileMode(path string) (os.FileMode, error)
}

// SupervisorWatchNodeLister is the minimal view of the existing
// compute node registry the watch needs: the registered node names.
// A non-nil *compute.Registry satisfies it (List returns the nodes
// sorted by name); tests use a fake.
type SupervisorWatchNodeLister interface {
	List() []*compute.Node
}

// SupervisorWatchTLSReporter is an optional interface a probe may also
// implement. UnverifiedNode reports whether the last probe of node ran
// with certificate verification turned off, so a finding raised from
// that probe can say so. SupervisorWatchHostProbe itself is unchanged;
// the host type-asserts for this interface.
type SupervisorWatchTLSReporter interface {
	UnverifiedNode(node string) bool
}

// supervisorWatchNodeNames returns the registered node names sorted,
// so check output is deterministic. A nil lister reads as no
// registered nodes: the node-scoped checks then find nothing.
func supervisorWatchNodeNames(lister SupervisorWatchNodeLister) []string {
	if lister == nil {
		return nil
	}
	nodes := lister.List()
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && strings.TrimSpace(n.Name) != "" {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	return names
}

// SupervisorWatchHostUnit is one running Automaton the
// verify_script_not_executable check inspects: its id, its registered
// worktree (the directory verification scripts resolve inside), and
// every one of its tasks' verification_script paths (repo-relative,
// blanks already dropped).
type SupervisorWatchHostUnit struct {
	AutomatonID string
	Worktree    string
	ScriptPaths []string
}

// SupervisorWatchRunningUnits flattens the running Automata into watch
// units: one unit per Automaton whose status is running
// (PRDRunning), with its registered worktree (PRD.ProjectDir) and the
// non-blank verification_script path of every one of its tasks.
// Automata that are not running, have no id, or have no registered
// worktree are skipped — the check resolves script paths inside the
// worktree only, and never for a work item that is not running. Order
// is by Automaton id so the findings are deterministic.
func SupervisorWatchRunningUnits(prds []*PRD) []SupervisorWatchHostUnit {
	seen := map[string]bool{}
	var units []SupervisorWatchHostUnit
	for _, p := range prds {
		if p == nil || p.Status != PRDRunning || strings.TrimSpace(p.ID) == "" {
			continue
		}
		worktree := strings.TrimSpace(p.ProjectDir)
		if worktree == "" || seen[p.ID] {
			continue
		}
		var scripts []string
		for i := range p.Story {
			for j := range p.Story[i].Tasks {
				if rel := strings.TrimSpace(p.Story[i].Tasks[j].VerificationScript); rel != "" {
					scripts = append(scripts, rel)
				}
			}
		}
		seen[p.ID] = true
		units = append(units, SupervisorWatchHostUnit{
			AutomatonID: p.ID,
			Worktree:    worktree,
			ScriptPaths: scripts,
		})
	}
	sort.Slice(units, func(i, j int) bool { return units[i].AutomatonID < units[j].AutomatonID })
	return units
}

// SupervisorWatchHost runs the daemon-side supervisor watch checks. It
// holds the injected probe, the watch config (the only place defaults
// are applied, via the config's Effective* accessors), the compute
// node registry view, and the in-memory tmux baseline the
// tmux_restart check stores on first sight. Checks return
// []SupervisorWatchFinding; they never act, in shadow or in on.
type SupervisorWatchHost struct {
	Probe  SupervisorWatchHostProbe
	Config SupervisorWatchConfig
	Nodes  SupervisorWatchNodeLister

	mu           sync.Mutex
	tmuxBaseline string
}

// NewSupervisorWatchHost wires a supervisor watch host. A nil probe or
// a nil node lister degrades every check to "unknown, no finding"
// rather than panicking — a broken dependency must never mint a
// finding. cfg.TmuxRestartBaseline, when set, seeds the stored
// tmux_restart baseline.
func NewSupervisorWatchHost(probe SupervisorWatchHostProbe, cfg SupervisorWatchConfig, nodes SupervisorWatchNodeLister) *SupervisorWatchHost {
	h := &SupervisorWatchHost{
		Probe:        probe,
		Config:       cfg,
		Nodes:        nodes,
		tmuxBaseline: strings.TrimSpace(cfg.TmuxRestartBaseline),
	}
	return h
}

// CheckTmuxRestart compares the tmux unit's restart count and
// active-since stamp against the stored baseline. First sight (no
// baseline stored yet) stores the baseline and raises nothing, so a
// freshly started watch never reports a restart nobody ever saw. A
// difference stores the new state and raises one tmux_restart finding;
// a probe error leaves the baseline untouched and raises nothing
// (unknown, no finding).
func (h *SupervisorWatchHost) CheckTmuxRestart() []SupervisorWatchFinding {
	if h.Probe == nil {
		return nil
	}
	count, countErr := h.Probe.ServiceRestartCount()
	since, sinceErr := h.Probe.ActiveSince()
	if countErr != nil || sinceErr != nil {
		// Unknown fact: keep the baseline, raise nothing.
		return nil
	}
	baseline := strconv.Itoa(count) + supervisorWatchBaselineSep + since

	h.mu.Lock()
	previous := h.tmuxBaseline
	if previous == "" {
		// First sight: store the baseline, raise nothing.
		h.tmuxBaseline = baseline
		h.mu.Unlock()
		return nil
	}
	changed := baseline != previous
	h.tmuxBaseline = baseline
	h.mu.Unlock()

	if !changed {
		return nil
	}
	return []SupervisorWatchFinding{{
		Kind:    SupervisorWatchTmuxRestart,
		Subject: SupervisorWatchTmuxUnitName,
		Text:    supervisorWatchFindingText(SupervisorWatchTmuxRestart, SupervisorWatchTmuxUnitName),
	}}
}

// StoredTmuxBaseline returns the tmux baseline the host has stored
// ("" before the first sight), for inspection and debugging.
func (h *SupervisorWatchHost) StoredTmuxBaseline() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tmuxBaseline
}

// CheckDaemonInactive raises one daemon_inactive finding per registered
// node whose Automaton daemon reads as inactive. A probe error for a
// node (no view of that daemon) raises nothing for it: unknown, no
// finding.
func (h *SupervisorWatchHost) CheckDaemonInactive() []SupervisorWatchFinding {
	if h.Probe == nil {
		return nil
	}
	var out []SupervisorWatchFinding
	for _, node := range supervisorWatchNodeNames(h.Nodes) {
		active, err := h.Probe.DaemonActive(node)
		if err != nil {
			continue
		}
		if active {
			continue
		}
		out = append(out, SupervisorWatchFinding{
			Kind:    SupervisorWatchDaemonInactive,
			Subject: node,
			Text:    supervisorWatchFindingText(SupervisorWatchDaemonInactive, node),
		})
	}
	return out
}

// CheckModelNotResident raises one model_not_resident finding per
// registered node whose resident model list lacks the configured
// working model (config WorkingModel). An unset working model
// (EffectiveWorkingModel blank) disables the check: there is no
// default working model. Node names come from the compute node
// registry, never a hard-coded list. A probe error for a node raises
// nothing for it: unknown, no finding.
func (h *SupervisorWatchHost) CheckModelNotResident() []SupervisorWatchFinding {
	if h.Probe == nil {
		return nil
	}
	model := h.Config.EffectiveWorkingModel()
	if model == "" {
		return nil
	}
	var out []SupervisorWatchFinding
	for _, node := range supervisorWatchNodeNames(h.Nodes) {
		loaded, err := h.Probe.ResidentModels(node)
		if err != nil {
			continue
		}
		if supervisorWatchModelResident(model, loaded) {
			continue
		}
		text := supervisorWatchFindingText(SupervisorWatchModelNotResident, node)
		if rep, ok := h.Probe.(SupervisorWatchTLSReporter); ok && rep.UnverifiedNode(node) {
			text = SupervisorWatchUnverifiedNote(text)
		}
		out = append(out, SupervisorWatchFinding{
			Kind:    SupervisorWatchModelNotResident,
			Subject: node,
			Text:    text,
		})
	}
	return out
}

// supervisorWatchModelResident reports whether model is in the node's
// resident list. A match is exact, or the model's base name under an
// ollama context variant ("base:tag") — the working model counts when
// it is loaded under its base tag with the context suffix.
func supervisorWatchModelResident(model string, loaded []string) bool {
	for _, l := range loaded {
		l = strings.TrimSpace(l)
		if l == model {
			return true
		}
		if i := strings.Index(l, ":"); i > 0 {
			if l[:i] == model {
				return true
			}
		}
	}
	return false
}

// CheckVerifyScriptNotExecutable walks the running Automata's
// verification_script paths and raises one verify_script_not_executable
// finding per Automaton having at least one script that exists inside
// its registered worktree but lacks the exec bit. Only paths that
// resolve inside the Automaton's own worktree are inspected: absolute
// paths and paths that climb out of the worktree are skipped, as are
// scripts that do not exist (a missing script is the
// verify-gate's own problem, reported elsewhere). A probe error reads
// as "unknown, no finding".
func (h *SupervisorWatchHost) CheckVerifyScriptNotExecutable(units []SupervisorWatchHostUnit) []SupervisorWatchFinding {
	if h.Probe == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []SupervisorWatchFinding
	for _, u := range units {
		if strings.TrimSpace(u.AutomatonID) == "" || seen[u.AutomatonID] {
			continue
		}
		if !supervisorWatchAutomatonHasBadScript(h.Probe, u) {
			continue
		}
		seen[u.AutomatonID] = true
		out = append(out, SupervisorWatchFinding{
			Kind:    SupervisorWatchVerifyScriptNotExecutable,
			Subject: u.AutomatonID,
			Text:    supervisorWatchFindingText(SupervisorWatchVerifyScriptNotExecutable, u.AutomatonID),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

// supervisorWatchAutomatonHasBadScript reports whether any of the
// Automaton's verification scripts exists inside its registered
// worktree without the exec bit.
func supervisorWatchAutomatonHasBadScript(probe SupervisorWatchHostProbe, u SupervisorWatchHostUnit) bool {
	worktree := filepath.Clean(strings.TrimSpace(u.Worktree))
	for _, rel := range u.ScriptPaths {
		relClean := filepath.Clean(rel)
		if filepath.IsAbs(relClean) {
			continue // repo-relative paths only
		}
		resolved := filepath.Join(worktree, relClean)
		if rel, _ := filepath.Rel(worktree, resolved); rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue // climbs out of the registered worktree
		}
		mode, err := probe.FileMode(resolved)
		if err != nil {
			continue // missing or unknown: no finding from this path
		}
		if mode&os.ModeType == 0 && mode&0o111 == 0 {
			return true // exists, no exec bit
		}
	}
	return false
}

// CheckMainCheckoutDirty raises one main_checkout_dirty finding when
// the main checkout (config MainCheckout; unset disables the check
// altogether) has modified (or otherwise changed) tracked files, as
// reported by `git status --short`. Untracked files ("??") do not
// count: a ship reads the working tree's tracked state. A probe error
// raises nothing: unknown, no finding.
func (h *SupervisorWatchHost) CheckMainCheckoutDirty() []SupervisorWatchFinding {
	if h.Probe == nil {
		return nil
	}
	dir := strings.TrimSpace(h.Config.MainCheckout)
	if dir == "" {
		return nil
	}
	dir = filepath.Clean(dir)
	lines, err := h.Probe.GitStatus(dir)
	if err != nil {
		return nil
	}
	dirty := 0
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "??") {
			continue
		}
		dirty++
	}
	if dirty == 0 {
		return nil
	}
	return []SupervisorWatchFinding{{
		Kind:    SupervisorWatchMainCheckoutDirty,
		Subject: dir,
		Text:    supervisorWatchFindingText(SupervisorWatchMainCheckoutDirty, dir),
	}}
}

// supervisorWatchFindingText builds the finding line via the single
// wording home, SupervisorWatchText; the kinds used here are all
// known, so a non-nil error cannot occur and the empty string is fine.
func supervisorWatchFindingText(kind SupervisorWatchKind, subject string) string {
	text, _ := SupervisorWatchText(kind, subject)
	return text
}

// ---------------------------------------------------------------------
//
// Production probe: ShellSupervisorWatchHostProbe.
//
// Shells out for the local-system facts and reads the node's
// resident model list over HTTP. Every external tool is resolved with
// `command -v` (via exec.LookPath, Go's PATH lookup) before use; when
// a tool is missing, a node is unreachable, or a command fails, the
// method returns an error and the checks degrade to "unknown, no
// finding". No method ever reports a value it could not read.

// ShellSupervisorWatchHostProbe — the production SupervisorWatchHostProbe.
type ShellSupervisorWatchHostProbe struct {
	// nodeAddresses maps each registered node name to the http(s)
	// address of its model server, for ResidentModels. A node without
	// an address degrades to "unknown, no finding" for that node. It is
	// the fallback when nodeLookup is unset or has no record of a node.
	nodeAddresses map[string]string
	// localNode is the registered node name of this daemon's own
	// host; only that node has a local daemon view.
	localNode string
	// shell resolves the tools; "sh" by default.
	shell string
	// httpc is the client for plain http nodes and for https nodes with
	// no node record; it verifies against the system roots.
	httpc *http.Client

	mu sync.Mutex
	// nodeLookup resolves a node's live record so its address and TLS
	// settings are read at probe time, not frozen at startup.
	nodeLookup func(node string) (*compute.Node, bool)
	// tlsClients caches one client per https node, keyed on the node's
	// TLS settings so an edit builds a fresh one.
	tlsClients map[string]*supervisorWatchTLSClient
	// unverified records which nodes the last probe ran against with
	// certificate verification off.
	unverified map[string]bool
	// onUnverified is called once per node per TLS setting change the
	// first time the node is probed with verification off.
	onUnverified func(node string)
}

// supervisorWatchTLSClient is one cached per-node https client and the
// settings key it was built from.
type supervisorWatchTLSClient struct {
	key    string
	client *http.Client
	mode   compute.TLSMode
}

// NewShellSupervisorWatchHostProbe builds the production probe.
// localNode defaults to "local" when blank; nodeAddresses may be nil.
func NewShellSupervisorWatchHostProbe(nodeAddresses map[string]string, localNode string) *ShellSupervisorWatchHostProbe {
	if localNode == "" {
		localNode = "local"
	}
	return &ShellSupervisorWatchHostProbe{
		nodeAddresses: nodeAddresses,
		localNode:     localNode,
		shell:         "sh",
		httpc:         &http.Client{Timeout: 15 * time.Second},
	}
}

// SetNodeLookup makes ResidentModels resolve each node's record live
// (address and TLS settings), so a node added or edited after startup
// is honoured. The address map given to the constructor stays the
// fallback for a node the lookup does not know.
func (p *ShellSupervisorWatchHostProbe) SetNodeLookup(lookup func(node string) (*compute.Node, bool)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nodeLookup = lookup
}

// SetUnverifiedProbeNotice sets a hook called the first time a node is
// probed with certificate verification off, after startup or after the
// node's TLS settings change: once per setting change, not per probe.
// A nil hook is fine.
func (p *ShellSupervisorWatchHostProbe) SetUnverifiedProbeNotice(fn func(node string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onUnverified = fn
}

// UnverifiedNode reports whether the last probe of node ran with
// certificate verification turned off.
func (p *ShellSupervisorWatchHostProbe) UnverifiedNode(node string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.unverified[node]
}

// resolveTool returns the absolute path of the tool on PATH (equivalent
// to `command -v <tool>`), or an error when it is not installed.
func (p *ShellSupervisorWatchHostProbe) resolveTool(tool string) (string, error) {
	path, err := exec.LookPath(tool)
	if err != nil {
		return "", fmt.Errorf("supervisor watch host: %s not found on PATH (command -v %s failed)", tool, tool)
	}
	return path, nil
}

// runCmd runs the tool with a bounded context and returns its
// CombinedOutput. Any failure is an error: the caller degrades to
// "unknown, no finding".
func (p *ShellSupervisorWatchHostProbe) runCmd(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("supervisor watch host: %s: %w", strings.Join(append([]string{name}, args...), " "), err)
	}
	return string(out), nil
}

// ServiceRestartCount reads the tmux unit's NRestarts.
func (p *ShellSupervisorWatchHostProbe) ServiceRestartCount() (int, error) {
	systemctl, err := p.resolveTool("systemctl")
	if err != nil {
		return 0, err
	}
	out, err := p.runCmd(context.Background(), systemctl, "--user", "show", "-p", "NRestarts", SupervisorWatchTmuxUnitName)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "NRestarts=") {
			n, err := strconv.Atoi(strings.TrimPrefix(line, "NRestarts="))
			if err != nil {
				return 0, fmt.Errorf("supervisor watch host: cannot parse NRestarts %q: %v", line, err)
			}
			return n, nil
		}
	}
	return 0, fmt.Errorf("supervisor watch host: %s unit unreadable (no NRestarts in output %q)", SupervisorWatchTmuxUnitName, strings.TrimSpace(out))
}

// ActiveSince reads the tmux unit's ActiveEnterTimestamp stamp.
func (p *ShellSupervisorWatchHostProbe) ActiveSince() (string, error) {
	systemctl, err := p.resolveTool("systemctl")
	if err != nil {
		return "", err
	}
	out, err := p.runCmd(context.Background(), systemctl, "--user", "show", "-p", "ActiveEnterTimestamp", SupervisorWatchTmuxUnitName)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ActiveEnterTimestamp=") {
			stamp := strings.TrimPrefix(line, "ActiveEnterTimestamp=")
			if stamp == "n/a" {
				return "", fmt.Errorf("supervisor watch host: %s unit has no ActiveEnterTimestamp", SupervisorWatchTmuxUnitName)
			}
			return strings.TrimSpace(stamp), nil
		}
	}
	return "", fmt.Errorf("supervisor watch host: %s unit unreadable (no ActiveEnterTimestamp in output %q)", SupervisorWatchTmuxUnitName, strings.TrimSpace(out))
}

// DaemonActive: only the daemon's own host has a local view of a
// daemon; every other registered node degrades to an error (unknown,
// no finding) rather than an answer.
func (p *ShellSupervisorWatchHostProbe) DaemonActive(node string) (bool, error) {
	if node != p.localNode {
		return false, fmt.Errorf("supervisor watch host: no local view of the daemon on node %q", node)
	}
	systemctl, err := p.resolveTool("systemctl")
	if err != nil {
		return false, err
	}
	// systemctl is-active exits non-zero for every state but active (3 for
	// inactive), so a non-zero exit with a state word on its output is the
	// answer, not a failure to read it.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, systemctl, "--user", "is-active", SupervisorWatchDaemonUnitName).CombinedOutput()
	state := strings.TrimSpace(string(raw))
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return false, fmt.Errorf("supervisor watch host: systemctl --user is-active %s: %w", SupervisorWatchDaemonUnitName, err)
		}
		switch state {
		case "inactive", "failed", "activating", "deactivating", "reloading", "maintenance":
			return false, nil
		}
		// no recognised state (for example the user manager is unreachable):
		// unknown, no finding
		return false, fmt.Errorf("supervisor watch host: systemctl --user is-active %s: %w (output %q)", SupervisorWatchDaemonUnitName, err, state)
	}
	return state == "active", nil
}

// ResidentModels reads the node's model report (GET /api/ps) and
// returns the resident model names. A node without a configured
// address, an unreadable report, an unusable TLS setting or a
// certificate that fails verification is an error: unknown, no
// finding.
func (p *ShellSupervisorWatchHostProbe) ResidentModels(node string) ([]string, error) {
	rec := p.lookupNode(node)
	addr := ""
	if rec != nil {
		addr = strings.TrimSpace(rec.Address)
	}
	if addr == "" {
		addr = strings.TrimSpace(p.nodeAddresses[node])
	}
	addr = strings.TrimRight(addr, "/")
	if addr == "" {
		return nil, fmt.Errorf("supervisor watch host: no model-server address for node %q", node)
	}
	u := addr
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "http://" + u
	}
	client, err := p.clientFor(node, rec, strings.HasPrefix(u, "https://"))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", u+"/api/ps", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		if supervisorWatchCertError(err) {
			return nil, fmt.Errorf("supervisor watch host: node %s certificate not trusted: %v: "+
				"set tls_ca_file or tls_cert_fingerprint on the node, or tls_insecure_skip_verify to turn verification off for it", node, err)
		}
		return nil, fmt.Errorf("supervisor watch host: node %s unreachable: %v", node, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("supervisor watch host: node %s report HTTP %d", node, resp.StatusCode)
	}
	var report struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, fmt.Errorf("supervisor watch host: node %s report unreadable: %v", node, err)
	}
	out := make([]string, 0, len(report.Models))
	for _, m := range report.Models {
		out = append(out, m.Name)
	}
	return out, nil
}

// lookupNode resolves the live record of node, or nil.
func (p *ShellSupervisorWatchHostProbe) lookupNode(node string) *compute.Node {
	p.mu.Lock()
	lookup := p.nodeLookup
	p.mu.Unlock()
	if lookup == nil {
		return nil
	}
	if n, ok := lookup(node); ok {
		return n
	}
	return nil
}

// clientFor picks the HTTP client for one probe of node. Plain http
// nodes, and https nodes with no record, use the default client (system
// roots). An https node with a record gets a cached client built from
// its TLS settings; a settings error is returned and the node counts
// as not unverified.
func (p *ShellSupervisorWatchHostProbe) clientFor(node string, rec *compute.Node, https bool) (*http.Client, error) {
	p.mu.Lock()
	if !https || rec == nil {
		p.dropTLSLocked(node)
		p.mu.Unlock()
		if p.httpc == nil {
			return http.DefaultClient, nil
		}
		return p.httpc, nil
	}
	key := rec.TLSSettingsKey()
	if c := p.tlsClients[node]; c != nil && c.key == key {
		client := c.client
		p.mu.Unlock()
		return client, nil
	}
	p.dropTLSLocked(node)
	cfg, mode, err := compute.NodeTLSConfig(rec)
	if err != nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("supervisor watch host: node %s TLS settings unusable: %w", node, err)
	}
	var tr *http.Transport
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = dt.Clone()
	} else {
		tr = &http.Transport{}
	}
	tr.TLSClientConfig = cfg
	client := &http.Client{Timeout: 15 * time.Second, Transport: tr}
	if p.tlsClients == nil {
		p.tlsClients = map[string]*supervisorWatchTLSClient{}
	}
	p.tlsClients[node] = &supervisorWatchTLSClient{key: key, client: client, mode: mode}
	var notice func(string)
	if mode == compute.TLSModeVerificationOff {
		if p.unverified == nil {
			p.unverified = map[string]bool{}
		}
		p.unverified[node] = true
		notice = p.onUnverified
	}
	p.mu.Unlock()
	if notice != nil {
		notice(node)
	}
	return client, nil
}

// dropTLSLocked forgets node's cached client and unverified mark. The
// caller holds p.mu.
func (p *ShellSupervisorWatchHostProbe) dropTLSLocked(node string) {
	if c := p.tlsClients[node]; c != nil {
		if tr, ok := c.client.Transport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
		delete(p.tlsClients, node)
	}
	delete(p.unverified, node)
}

// supervisorWatchCertError reports whether err is a certificate
// verification or pin failure from the TLS handshake.
func supervisorWatchCertError(err error) bool {
	var unknownAuth x509.UnknownAuthorityError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &unknownAuth) || errors.As(err, &host) || errors.As(err, &invalid) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "failed to verify certificate") ||
		strings.Contains(msg, "fingerprint mismatch") ||
		strings.Contains(msg, "verify peer certificate") ||
		strings.Contains(msg, "x509:")
}

// GitStatus returns the `git status --short` lines of the main
// checkout. git missing or the directory not readable is an error:
// unknown, no finding.
func (p *ShellSupervisorWatchHostProbe) GitStatus(dir string) ([]string, error) {
	git, err := p.resolveTool("git")
	if err != nil {
		return nil, err
	}
	out, err := p.runCmd(context.Background(), git, "-C", dir, "status", "--short")
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// FileMode stats the path, preserving os.IsNotExist so the checks can
// tell "the script is not there" from a degraded probe.
func (p *ShellSupervisorWatchHostProbe) FileMode(path string) (os.FileMode, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Mode(), nil
}
