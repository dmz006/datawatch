// CEF (ArcSight Common Event Format) mirror output — AGENT.md's Audit
// Logging Rule: every audit-style event must be emittable in both
// JSON-lines (the primary, queryable format `/api/audit`/`/api/audit/
// access` read back) and CEF (for SOC/SIEM forwarding — Splunk, QRadar,
// ArcSight, Sentinel, Chronicle all parse it out of the box).
//
// Unlike internal/agents/audit.go's FileAuditor (one format per file,
// chosen at construction — fine for F10's low-volume agent events),
// this package's Log is read back by its own REST/MCP query surface, so
// CEF can't be a format SWITCH on the primary file (Read() would be
// unable to parse CEF lines back into Entry). Instead CEF is an
// additive MIRROR: when enabled, every Write also appends a CEF line to
// a second file (<path>.cef), which an operator points their SIEM
// forwarder at directly. The JSON-lines file stays the single source of
// truth for Read/Prune.

package audit

import (
	"fmt"
	"sort"
	"strings"
)

// cefVersionFn supplies the datawatch version for the CEF header.
// Set once at daemon startup (SetCEFVersionFn) to avoid an import cycle
// with cmd/datawatch; defaults to "v?" for tests/standalone use.
var cefVersionFn = func() string { return "v?" }

// SetCEFVersionFn lets the daemon inject its real version string into
// every CEF line this package writes.
func SetCEFVersionFn(f func() string) {
	if f != nil {
		cefVersionFn = f
	}
}

// FormatCEFLine renders e as a single CEF:0 line.
func FormatCEFLine(e Entry) string {
	sigID, name, severity := cefSignature(e.Action)
	header := fmt.Sprintf("CEF:0|datawatch|datawatch|%s|%d|%s|%d|",
		cefHeaderEscape(cefVersionFn()),
		sigID,
		cefHeaderEscape(name),
		severity)

	ext := []string{
		"rt=" + cefExtEscape(e.Timestamp.UTC().Format("Jan 02 2006 15:04:05.000")),
		"suser=" + cefExtEscape(e.Actor),
		"deviceCustomString1Label=action",
		"deviceCustomString1=" + cefExtEscape(e.Action),
	}
	if e.SessionID != "" {
		ext = append(ext, "deviceCustomString2Label=session_id",
			"deviceCustomString2="+cefExtEscape(e.SessionID))
	}

	if e.Subject != "" {
		// Fixed at cs6 so the detail slots below keep their positions whether
		// or not an event carries a subject.
		ext = append(ext, "deviceCustomString6Label=subject",
			"deviceCustomString6="+cefExtEscape(e.Subject))
	}

	// Known Details keys map to real CEF standard keys where one fits;
	// everything else falls back to numbered custom strings, sorted for
	// deterministic output (Details is a map).
	rest := make([]string, 0, len(e.Details))
	csSlot := 3
	for k := range e.Details {
		switch k {
		case "remote_ip", "user_agent", "method", "path", "status":
			// handled explicitly below
		default:
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)

	if v, ok := e.Details["remote_ip"]; ok {
		ext = append(ext, "src="+cefExtEscape(fmt.Sprintf("%v", v)))
	}
	if v, ok := e.Details["user_agent"]; ok {
		ext = append(ext, "requestClientApplication="+cefExtEscape(fmt.Sprintf("%v", v)))
	}
	if v, ok := e.Details["method"]; ok {
		ext = append(ext, "requestMethod="+cefExtEscape(fmt.Sprintf("%v", v)))
	}
	if v, ok := e.Details["path"]; ok {
		ext = append(ext, "request="+cefExtEscape(fmt.Sprintf("%v", v)))
	}
	if v, ok := e.Details["status"]; ok {
		ext = append(ext, "deviceCustomNumber1Label=status",
			fmt.Sprintf("deviceCustomNumber1=%v", v))
	}
	var overflow []string // details past the three custom-string slots
	for _, k := range rest {
		if csSlot > 5 {
			overflow = append(overflow, k+"="+fmt.Sprintf("%v", e.Details[k]))
			continue // cs1/cs2 are taken above and cs6 carries the subject
		}
		ext = append(ext, fmt.Sprintf("deviceCustomString%dLabel=%s", csSlot, cefExtEscape(k)),
			fmt.Sprintf("deviceCustomString%d=%s", csSlot, cefExtEscape(fmt.Sprintf("%v", e.Details[k]))))
		csSlot++
	}

	if len(overflow) > 0 {
		ext = append(ext, "msg="+cefExtEscape(strings.Join(overflow, "; ")))
	}

	return header + strings.Join(ext, " ")
}

// cefSignature maps an audit Action string to (signatureID, name,
// severity). Known actions get a specific, named signature; anything
// else (the many free-form Action strings the 12+ operator-audit call
// sites use today) gets a stable generic fallback — the same shape
// internal/agents/audit.go uses for its own unmapped events. Add a case
// here as each new Action is introduced rather than requiring an
// exhaustive upfront list.
func cefSignature(action string) (int, string, int) {
	switch action {
	case "http_access":
		return 10, "HTTPAccess", 2
	case "auth_failure":
		return 11, "AuthFailure", 7
	case "ws_connect":
		return 12, "WSConnect", 2
	case "ws_disconnect":
		return 13, "WSDisconnect", 2
	case "start":
		return 20, "SessionStart", 3
	case "kill":
		return 21, "SessionKill", 4
	case "send_input":
		return 22, "SessionSendInput", 2
	case "configure":
		return 23, "ConfigChange", 5
	case "rollback":
		return 24, "ConfigRollback", 6
	case "schedule":
		return 25, "ScheduleChange", 3
	case "autonomous_file_drift_detected":
		return 26, "AutonomousFileDriftDetected", 3
	case "config_deprecated_key":
		return 27, "ConfigDeprecatedKey", 2
	case "automation_create":
		return 28, "AutomationCreated", 3
	case "automation_update":
		return 29, "AutomationUpdated", 3
	case "automation_delete":
		return 30, "AutomationDeleted", 5
	case ActionUsageBackoffSet:
		return 31, "AutonomousUsageBackoffSet", 4
	case ActionUsageBackoffCleared:
		return 32, "AutonomousUsageBackoffCleared", 3
	case ActionUsageBackoffAutoCleared:
		return 33, "AutonomousUsageBackoffAutoCleared", 2
	case ActionUsageBackoffPatternsRechecked:
		return 34, "AutonomousUsageBackoffPatternsRechecked", 3
	case ActionBudgetCeilingWait:
		return 35, "AutonomousBudgetCeilingWait", 4
	case ActionBudgetWindowResumed:
		return 36, "AutonomousBudgetWindowResumed", 2
	case ActionAutomationBudgetBreach:
		return 37, "AutomationBudgetBreach", 5
	case ActionAutomationBudgetResumed:
		return 38, "AutomationBudgetResumed", 2
	case ActionCapacityWait:
		return 39, "AutonomousCapacityWait", 3
	case ActionCapacityAdmitted:
		return 40, "AutonomousCapacityAdmitted", 2
	case ActionBudgetWindowRollover:
		return 41, "AutonomousBudgetWindowRollover", 2
	case ActionFingerprintMatchConfirmed:
		return 42, "FingerprintMatchConfirmed", 4
	case ActionFingerprintMatchRejected:
		return 43, "FingerprintMatchRejected", 2
	case ActionAutomationImportGraph:
		return 44, "AutomationImportGraph", 3
	case ActionNotifyEventsChanged:
		return 45, "NotifyEventsChanged", 3
	case ActionLifecycleWebhookDelivery:
		return 46, "AutonomousLifecycleWebhookDelivery", 2
	case ActionLifecycleWebhookFailed:
		return 47, "AutonomousLifecycleWebhookFailed", 6
	case ActionLifecycleForwardDelivery:
		return 48, "AutonomousLifecycleForwardDelivery", 3
	case ActionLifecycleForwardReceive:
		return 49, "AutonomousLifecycleForwardReceive", 3
	case ActionQuestionRaised:
		return 50, "QuestionRaised", 3
	case ActionQuestionAnswered:
		return 51, "QuestionAnswered", 3
	case ActionQuestionWithdrawn:
		return 52, "QuestionWithdrawn", 3
	case ActionQuestionExpired:
		return 53, "QuestionExpired", 4
	case ActionAnswerSuperseded:
		return 54, "AnswerSuperseded", 4
	case ActionQuestionAnswerRefused:
		return 55, "QuestionAnswerRefused", 6
	case ActionUsageReadingAdded:
		return 56, "UsageReadingAdded", 3
	case ActionUsageReadingDeleted:
		return 57, "UsageReadingDeleted", 4
	case ActionUsageExtraCreditSet:
		return 58, "UsageExtraCreditSet", 3
	case ActionUsageRemainingWorkSet:
		return 59, "UsageRemainingWorkSet", 2
	case ActionStuckRunningBlocked:
		return 60, "AutonomousStuckRunningBlocked", 5
	default:
		return 0, "AuditEvent", 3
	}
}

// cefHeaderEscape escapes '|' and '\' per the CEF spec's header rules.
func cefHeaderEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '|' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

// cefExtEscape escapes '=', '\', '\n', '\r' per the CEF spec's
// extension-field rules (pipes are fine unescaped in extensions).
func cefExtEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\', '=':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
