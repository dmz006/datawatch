// BL37 — system diagnostics.
//
// GET /api/diagnose returns a health snapshot: each check is {name,
// ok, detail}. Composite ok=true iff all checks pass. Used by comm
// channels, MCP, and ops tooling to answer "is datawatch healthy?".

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// DiagnoseCheck is one per-subsystem result.
type DiagnoseCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// DiagnoseResult is the top-level response.
type DiagnoseResult struct {
	OK        bool            `json:"ok"`
	Hostname  string          `json:"hostname"`
	Version   string          `json:"version"`
	Checks    []DiagnoseCheck `json:"checks"`
	Timestamp time.Time       `json:"timestamp"`
}

func (s *Server) handleDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res := s.runDiagnose()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// runDiagnose collects health signals the ops dashboard cares about.
func (s *Server) runDiagnose() DiagnoseResult {
	out := DiagnoseResult{
		Hostname:  s.hostname,
		Version:   Version,
		Timestamp: time.Now(),
	}

	// 1. tmux available?
	if _, err := exec.LookPath("tmux"); err != nil {
		out.Checks = append(out.Checks, DiagnoseCheck{
			Name: "tmux_available", OK: false, Detail: err.Error(),
		})
	} else {
		out.Checks = append(out.Checks, DiagnoseCheck{Name: "tmux_available", OK: true})
	}

	// 2. session manager initialized?
	if s.manager == nil {
		out.Checks = append(out.Checks, DiagnoseCheck{
			Name: "session_manager", OK: false, Detail: "nil manager",
		})
	} else {
		count := len(s.manager.ListSessions())
		out.Checks = append(out.Checks, DiagnoseCheck{
			Name: "session_manager", OK: true,
			Detail: fmt.Sprintf("%d session(s) loaded", count),
		})
	}

	// 3. config readable?
	if s.cfgPath != "" {
		if _, err := os.Stat(s.cfgPath); err != nil {
			out.Checks = append(out.Checks, DiagnoseCheck{
				Name: "config_file", OK: false, Detail: err.Error(),
			})
		} else {
			out.Checks = append(out.Checks, DiagnoseCheck{
				Name: "config_file", OK: true, Detail: s.cfgPath,
			})
		}
	}

	// 4. data dir writable?
	if s.cfg != nil && s.cfg.DataDir != "" {
		out.Checks = append(out.Checks, checkDirWritable("data_dir", s.cfg.DataDir))
	}

	// 5. disk space on data dir (warn when < 1GB free).
	if s.cfg != nil && s.cfg.DataDir != "" {
		out.Checks = append(out.Checks, checkDiskSpace("disk_space", s.cfg.DataDir, 1<<30))
	}

	// 6. Go runtime sanity — flag if goroutine count looks unbounded.
	gc := runtime.NumGoroutine()
	ok := gc < 5000
	detail := fmt.Sprintf("%d goroutines", gc)
	if !ok {
		detail += " (>=5000 suggests a leak)"
	}
	out.Checks = append(out.Checks, DiagnoseCheck{
		Name: "goroutines", OK: ok, Detail: detail,
	})

	out.OK = allOK(out.Checks)
	return out
}

func checkDirWritable(name, dir string) DiagnoseCheck {
	info, err := os.Stat(dir)
	if err != nil {
		return DiagnoseCheck{Name: name, OK: false, Detail: err.Error()}
	}
	if !info.IsDir() {
		return DiagnoseCheck{Name: name, OK: false, Detail: "not a directory"}
	}
	probe, err := os.CreateTemp(dir, ".diagnose-*")
	if err != nil {
		return DiagnoseCheck{Name: name, OK: false, Detail: "not writable: " + err.Error()}
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return DiagnoseCheck{Name: name, OK: true, Detail: dir}
}

func allOK(cs []DiagnoseCheck) bool {
	for _, c := range cs {
		if !c.OK {
			return false
		}
	}
	return true
}
