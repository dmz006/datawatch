// BL110 — audit sink for AI-initiated self-modify config writes.

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SelfConfigAudit captures the operator-visible record of one
// AI-initiated config_set call. Written to stderr (always) + the
// JSON-lines file at MCPConfig.SelfConfigAuditPath (when non-empty).
type SelfConfigAudit struct {
	At    time.Time `json:"at"`
	Key   string    `json:"key"`
	Value string    `json:"value"`
	Note  string    `json:"note,omitempty"`
}

var selfConfigMu sync.Mutex

// auditSelfConfig logs the mutation. Failure to write the file is
// logged to stderr but never returned — the audit is a record, not a
// blocking dependency.
func (s *Server) auditSelfConfig(key, value string) {
	ev := SelfConfigAudit{
		At:    time.Now().UTC(),
		Key:   key,
		Value: value,
		Note:  "AI-initiated via MCP config_set (mcp.allow_self_config=true)",
	}
	raw, _ := json.Marshal(ev)
	fmt.Fprintf(os.Stderr, "[mcp/self-config] %s\n", raw)

	if s.cfg == nil || s.cfg.SelfConfigAuditPath == "" {
		return
	}
	selfConfigMu.Lock()
	defer selfConfigMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.cfg.SelfConfigAuditPath), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "[mcp/self-config] mkdir: %v\n", err)
		return
	}
	f, err := os.OpenFile(s.cfg.SelfConfigAuditPath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[mcp/self-config] open: %v\n", err)
		return
	}
	defer f.Close()
	_, _ = f.Write(append(raw, '\n'))
}
