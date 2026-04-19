// BL103 — validator agent check logic.
//
// S7.5 ships the trigger: when a worker session ends and its profile
// has AutoValidate=true, the parent spawns a small validator agent
// against the ValidateProfile. The validator reads the worker's
// task + result + audit trail from the parent's REST API and emits
// a Pass / Fail / Inconclusive verdict.
//
// What the validator checks (additive over time):
//
//   1. The worker's RecordResult was set (worker reported back at all)
//   2. RecordResult.Status == "ok"
//   3. The worker emitted at least one memory write (proves it did
//      ANY work, even if the diff is empty)
//   4. The worker's audit trail contains a "spawn" event followed by
//      a non-failed terminal event
//   5. (when known) the declared Task is non-empty so RecordResult
//      has something to attest to
//
// All checks are read-only — the validator never mutates worker
// state. The verdict is reported back via POST /api/agents/{id}/result
// on the validator's own agent ID so the parent's audit trail picks
// it up alongside the original worker's events.

package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Verdict is the validator's pass/fail signal.
type Verdict string

const (
	VerdictPass         Verdict = "pass"
	VerdictFail         Verdict = "fail"
	VerdictInconclusive Verdict = "inconclusive"
)

// Result is the structured verdict + reasoning the validator emits.
type Result struct {
	Verdict Verdict  `json:"verdict"`
	Reasons []string `json:"reasons"`
	// CheckedAt is when the validation pass completed.
	CheckedAt time.Time `json:"checked_at"`
	// WorkerID is the agent ID being validated.
	WorkerID string `json:"worker_id"`
}

// Config bundles the validator's runtime knobs.
type Config struct {
	// ParentURL is the parent's base URL (DATAWATCH_BOOTSTRAP_URL).
	ParentURL string
	// Token is the optional bearer token for parent API calls.
	Token string
	// WorkerID is the agent ID this validator is checking. Validators
	// receive it via DATAWATCH_VALIDATE_TARGET_AGENT_ID env.
	WorkerID string
	// HTTP is the http.Client to use; defaults to a 15s client.
	HTTP *http.Client
}

// agentSnapshot mirrors the json:"-"-redacted shape of agents.Agent
// returned by GET /api/agents/{id}.
type agentSnapshot struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Task  string `json:"task"`

	Result *struct {
		Status   string `json:"status"`
		Summary  string `json:"summary,omitempty"`
		Findings []any  `json:"findings,omitempty"`
	} `json:"result,omitempty"`
}

// auditPayload mirrors the response of GET /api/agents/audit.
type auditPayload struct {
	Path   string `json:"path"`
	Events []struct {
		Event   string `json:"event"`
		AgentID string `json:"agent_id"`
		State   string `json:"state"`
		Note    string `json:"note,omitempty"`
	} `json:"events"`
}

// Validate runs every check against the parent's REST API and
// returns the structured verdict. The function is safe to call
// repeatedly; each call is one read-only sweep.
func Validate(ctx context.Context, cfg Config) (*Result, error) {
	if cfg.WorkerID == "" {
		return nil, fmt.Errorf("validator: worker_id required")
	}
	if cfg.ParentURL == "" {
		return nil, fmt.Errorf("validator: parent_url required")
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 15 * time.Second}
	}

	res := &Result{
		WorkerID:  cfg.WorkerID,
		CheckedAt: time.Now().UTC(),
	}

	snap, err := fetchAgent(ctx, cfg)
	if err != nil {
		res.Verdict = VerdictInconclusive
		res.Reasons = append(res.Reasons,
			fmt.Sprintf("could not fetch worker agent: %v", err))
		return res, nil
	}

	// Check 5 first: declared task is required to attest to anything.
	if strings.TrimSpace(snap.Task) == "" {
		res.Reasons = append(res.Reasons,
			"worker has no declared task to attest to")
	}

	// Check 1 + 2: result reported and status is ok.
	switch {
	case snap.Result == nil:
		res.Reasons = append(res.Reasons,
			"worker did not report a Result before terminal state")
	case snap.Result.Status != "ok":
		res.Reasons = append(res.Reasons,
			fmt.Sprintf("worker result status = %q (want ok)", snap.Result.Status))
	}

	// Checks 3 + 4: audit trail must show progress.
	audit, err := fetchAudit(ctx, cfg)
	if err != nil {
		res.Reasons = append(res.Reasons,
			fmt.Sprintf("audit fetch failed; cannot verify activity: %v", err))
	} else {
		var sawSpawn, sawTerminalOK, sawMemoryWrite bool
		for _, ev := range audit.Events {
			if ev.AgentID != cfg.WorkerID {
				continue
			}
			switch ev.Event {
			case "spawn":
				sawSpawn = true
			case "terminate":
				if ev.State != "failed" {
					sawTerminalOK = true
				}
			case "memory_write", "memory_save":
				sawMemoryWrite = true
			}
		}
		if !sawSpawn {
			res.Reasons = append(res.Reasons,
				"audit shows no spawn event for this worker")
		}
		if !sawTerminalOK {
			res.Reasons = append(res.Reasons,
				"audit shows no clean terminal event")
		}
		if !sawMemoryWrite {
			res.Reasons = append(res.Reasons,
				"audit shows no memory writes — worker may not have done any work")
		}
	}

	switch {
	case len(res.Reasons) == 0:
		res.Verdict = VerdictPass
	case len(res.Reasons) <= 2 && snap.Result != nil && snap.Result.Status == "ok":
		// Soft fail: result is OK but some non-critical signals missing.
		res.Verdict = VerdictInconclusive
	default:
		res.Verdict = VerdictFail
	}
	return res, nil
}

func fetchAgent(ctx context.Context, cfg Config) (*agentSnapshot, error) {
	endpoint := strings.TrimRight(cfg.ParentURL, "/") + "/api/agents/" + cfg.WorkerID
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	resp, err := cfg.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	var snap agentSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &snap, nil
}

func fetchAudit(ctx context.Context, cfg Config) (*auditPayload, error) {
	q := url.Values{}
	q.Set("agent_id", cfg.WorkerID)
	q.Set("limit", "200")
	endpoint := strings.TrimRight(cfg.ParentURL, "/") + "/api/agents/audit?" + q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	resp, err := cfg.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	var payload auditPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &payload, nil
}

// Report POSTs the verdict back to the parent as the validator's
// own RecordResult (the validator is itself a spawned agent). The
// validator's own agent ID is read from DATAWATCH_AGENT_ID by the
// caller.
func (r *Result) Report(ctx context.Context, cfg Config, validatorAgentID string) error {
	if validatorAgentID == "" {
		return fmt.Errorf("validator.Report: validator_agent_id required")
	}
	body, _ := json.Marshal(map[string]any{
		"status":  string(r.Verdict),
		"summary": strings.Join(r.Reasons, "; "),
		"findings": map[string]any{
			"verdict":    r.Verdict,
			"reasons":    r.Reasons,
			"worker_id":  r.WorkerID,
			"checked_at": r.CheckedAt,
		},
	})
	endpoint := strings.TrimRight(cfg.ParentURL, "/") + "/api/agents/" + validatorAgentID + "/result"
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, raw)
	}
	return nil
}
