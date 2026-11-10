// Worker profile status: the read-only worker_profile_status MCP tool. It
// reports the built-in worker profile names, the built-in defaults per role
// and per task kind, and the latest start-up averages from the stats block
// (worker_startup in GET /api/stats).

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/dmz006/datawatch/internal/workerprofile"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// workerProfileStatus is the body worker_profile_status returns.
type workerProfileStatus struct {
	// Profiles lists the built-in profile names, sorted.
	Profiles []string `json:"profiles"`
	// RoleDefaults maps each worker role to the built-in profile it starts with.
	RoleDefaults map[string]string `json:"role_defaults"`
	// KindDefaults maps each task kind to the built-in profile it starts with.
	KindDefaults map[string]string `json:"kind_defaults"`
	// Startup is the worker_startup block of the latest stats snapshot, as
	// the stats block reports it. Both maps are always present.
	Startup json.RawMessage `json:"startup"`
	// InForce lists the profile each worker role and task kind starts with,
	// built by workerprofile.InForce from the live daemon values (roles first,
	// then kinds). It is always present, never null.
	InForce []inForceRow `json:"in_force"`
}

// inForceRow is one in_force entry: the same shape GET /api/config reports.
type inForceRow struct {
	Scope   string `json:"scope"`
	Key     string `json:"key"`
	Profile string `json:"profile"`
	Tier    string `json:"tier"`
	Reason  string `json:"reason"`
}

// autonomousWorkerProfiles is the slice of GET /api/config's autonomous block
// that the profile-in-force read needs: the user profiles, the role and kind
// defaults and the global profile name, as the daemon reports them.
type autonomousWorkerProfiles struct {
	WorkerProfiles        map[string]workerprofile.WorkerProfile `json:"worker_profiles"`
	WorkerProfileDefaults struct {
		Role map[string]string `json:"role"`
		Kind map[string]string `json:"kind"`
	} `json:"worker_profile_defaults"`
	WorkerProfile string `json:"worker_profile"`
}

func (s *Server) toolWorkerProfileStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("worker_profile_status",
		mcpsdk.WithDescription("Read-only worker profile status: the built-in profile names (minimal, digest, full, datawatch-tools), the built-in default profile for each role and task kind, and the latest average start-up token size per profile and per role from the stats block (worker_startup). The in_force field lists, for each role and task kind, the profile it starts with now (scope, key, profile, tier and reason), built from the live config. Configure profiles with config_set on autonomous.worker_profiles, autonomous.worker_profile_defaults and autonomous.worker_profile; read them with get_config section=autonomous."),
	)
}

func (s *Server) handleWorkerProfileStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	startup, err := s.fetchWorkerStartup()
	if err != nil {
		return toolErrorf("worker_profile_status", "stats error: %v", err), nil
	}
	inForce, err := s.fetchInForce()
	if err != nil {
		return toolErrorf("worker_profile_status", "config error: %v", err), nil
	}
	out := workerProfileStatus{
		Profiles:     workerprofile.Names(),
		RoleDefaults: workerprofile.RoleDefaults(),
		KindDefaults: workerprofile.KindDefaults(),
		Startup:      startup,
		InForce:      inForce,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return toolErrorf("worker_profile_status", "encode error: %v", err), nil
	}
	return mcpsdk.NewToolResultText(string(b)), nil
}

// fetchInForce reads the autonomous worker profile settings from the daemon's
// config and resolves the profile in force for each role and task kind with
// workerprofile.InForce. It never returns a nil slice.
func (s *Server) fetchInForce() ([]inForceRow, error) {
	if s.webPort <= 0 {
		return nil, fmt.Errorf("web server not available")
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/api/config", s.webPort), nil)
	resp, err := s.webDo(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var cfg struct {
		Autonomous autonomousWorkerProfiles `json:"autonomous"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	a := cfg.Autonomous
	users := a.WorkerProfiles
	entries := workerprofile.InForce(workerprofile.InForceInput{
		RoleDefaults:  a.WorkerProfileDefaults.Role,
		KindDefaults:  a.WorkerProfileDefaults.Kind,
		GlobalProfile: a.WorkerProfile,
		Lookup: func(name string) (workerprofile.WorkerProfile, bool) {
			if p, ok := workerprofile.Builtin(name); ok {
				return p, true
			}
			p, ok := users[name]
			return p, ok
		},
	})
	out := make([]inForceRow, 0, len(entries))
	for _, e := range entries {
		out = append(out, inForceRow{Scope: e.Scope, Key: e.Key, Profile: e.Profile, Tier: e.Tier, Reason: e.Reason})
	}
	return out, nil
}

// fetchWorkerStartup reads the worker_startup block from the daemon's stats
// snapshot. A missing block reads as empty maps, never as an error.
func (s *Server) fetchWorkerStartup() (json.RawMessage, error) {
	if s.webPort <= 0 {
		return nil, fmt.Errorf("web server not available")
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/api/stats", s.webPort), nil)
	resp, err := s.webDo(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var snap struct {
		WorkerStartup json.RawMessage `json:"worker_startup"`
	}
	if err := json.Unmarshal(body, &snap); err != nil {
		return nil, fmt.Errorf("decode stats: %w", err)
	}
	if len(snap.WorkerStartup) == 0 || string(snap.WorkerStartup) == "null" {
		return json.RawMessage(`{"average_startup_tokens_by_profile":{},"average_startup_tokens_by_role":{}}`), nil
	}
	return snap.WorkerStartup, nil
}

// toolAutonomousPRDSetWorkerProfile sets the worker profile an Automaton's
// sessions start with. The name is checked by the daemon (built-in or
// user-defined); a refused name changes nothing.
func (s *Server) toolAutonomousPRDSetWorkerProfile() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_worker_profile",
		mcpsdk.WithDescription("Set the worker profile an Automaton's sessions start with: a built-in name (minimal, digest, full, datawatch-tools) or a user-defined autonomous.worker_profiles name. An empty name clears the override so the global layer and role/kind defaults apply again. An unknown name is refused and changes nothing."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("worker_profile", mcpsdk.Required(), mcpsdk.Description("Profile name; empty to clear")),
	)
}

func (s *Server) handleAutonomousPRDSetWorkerProfile(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return toolErrorf("autonomous_prd_set_worker_profile", "id is required"), nil
	}
	body, _ := json.Marshal(map[string]string{"worker_profile": req.GetString("worker_profile", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_worker_profile", body)
	return llmSetterResult(out, err)
}
