// Audit tests for the usage-calculator record events (AGENT.md Audit
// Logging Rule): a JSON-lines round-trip and a CEF escape test for each
// new action, with pipe, '=', backslash and newline in the header AND
// extension values.

package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var usageCalcEvents = []struct {
	action string
	sigID  string
	name   string
	sev    string
}{
	{ActionUsageReadingAdded, "56", "UsageReadingAdded", "3"},
	{ActionUsageReadingDeleted, "57", "UsageReadingDeleted", "4"},
	{ActionUsageExtraCreditSet, "58", "UsageExtraCreditSet", "3"},
	{ActionUsageRemainingWorkSet, "59", "UsageRemainingWorkSet", "2"},
}

func TestUsageCalcEvents_Names(t *testing.T) {
	want := map[string]string{
		ActionUsageReadingAdded:     "usage_reading_added",
		ActionUsageReadingDeleted:   "usage_reading_deleted",
		ActionUsageExtraCreditSet:   "usage_extra_credit_set",
		ActionUsageRemainingWorkSet: "usage_remaining_work_set",
	}
	for got, w := range want {
		if got != w {
			t.Errorf("action constant = %q, want %q", got, w)
		}
	}
}

func TestUsageCalcEvents_JSONLinesRoundTrip(t *testing.T) {
	for _, ev := range usageCalcEvents {
		t.Run(ev.action, func(t *testing.T) {
			dir := t.TempDir()
			l, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			in := Entry{
				Timestamp: time.Now().UTC().Truncate(time.Millisecond),
				Actor:     "operator",
				Action:    ev.action,
				Details:   map[string]any{"points": "25|points=x\nnew"},
			}
			if err := l.Write(in); err != nil {
				t.Fatal(err)
			}
			_ = l.Close()

			f, err := os.Open(filepath.Join(dir, "audit.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			sc := bufio.NewScanner(f)
			var lines []string
			for sc.Scan() {
				lines = append(lines, sc.Text())
			}
			if len(lines) != 1 {
				t.Fatalf("want 1 JSON line, got %d: %q", len(lines), lines)
			}
			var out Entry
			if err := json.Unmarshal([]byte(lines[0]), &out); err != nil {
				t.Fatalf("line is not valid JSON: %v (%q)", err, lines[0])
			}
			if out.Action != ev.action || out.Actor != "operator" || !out.Timestamp.Equal(in.Timestamp) {
				t.Fatalf("round-trip mismatch: %+v", out)
			}
			if out.Details["points"] != "25|points=x\nnew" {
				t.Fatalf("details lost: %+v", out.Details)
			}
		})
	}
}

func TestUsageCalcEvents_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	for _, ev := range usageCalcEvents {
		t.Run(ev.action, func(t *testing.T) {
			line := FormatCEFLine(Entry{
				Timestamp: time.Now(),
				Actor:     "ope|rator=x",
				Action:    ev.action,
				Details: map[string]any{
					"points": "25=a\\b\nCEF:0|evil",
				},
			})
			wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|` + ev.sigID + `|` + ev.name + `|` + ev.sev + `|`
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
			}
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1Label=action deviceCustomString1="+ev.action) {
				t.Errorf("action extension missing: %q", line)
			}
			// The points extension carries '=', backslash and a newline; it must be
			// escaped per the CEF extension rules (pipes are fine raw).
			if !strings.Contains(line, `Label=points deviceCustomString3=25\=a\\b\nCEF:0|evil`) {
				t.Errorf("points extension not escaped: %q", line)
			}
		})
	}
}

// TestUsageCalcEvents_CEFDetailKeys pins where the emitters’ detail keys land:
// at most three details carry labeled custom-string extensions (cs3–cs5,
// sorted, the codebase norm where resource_type/resource_id push the rest
// over), and the remainder overflows into msg= as “k=v” text.
func TestUsageCalcEvents_CEFDetailKeys(t *testing.T) {
	SetCEFVersionFn(func() string { return "v?" })
	defer SetCEFVersionFn(func() string { return "v?" })

	for _, ev := range []struct {
		action  string
		details map[string]any
	}{
		{ActionUsageReadingAdded, map[string]any{"source": "typed", "at": "2026-10-25T00:00:00Z", "percent": "61.5", "lanes": "3"}},
		{ActionUsageReadingDeleted, map[string]any{}},
		{ActionUsageExtraCreditSet, map[string]any{"points": "100"}},
		{ActionUsageRemainingWorkSet, map[string]any{"points": "72.25"}},
	} {
		line := FormatCEFLine(Entry{
			Timestamp: time.Now(),
			Actor:     "operator",
			Action:    ev.action,
			Details:   ev.details,
		})
		keys := make([]string, 0, len(ev.details))
		for k := range ev.details {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			v := fmt.Sprintf("%v", ev.details[k])
			if i < 3 {
				if !strings.Contains(line, "deviceCustomString"+itoa(3+i)+"Label="+k+" ") {
					t.Errorf("%s: %q not labeled at custom string %d:\n%s", ev.action, k, 3+i, line)
				}
			} else {
				// Overflow rides in msg= as “k=v” with the '=' escaped (k\=v);
				// the value here is plain, so the escaped pair stays a simple substring.
				if !strings.Contains(line, "msg=") || !strings.Contains(line, k+"\\="+v) {
					t.Errorf("%s: %q not in msg overflow:\n%s", ev.action, k, line)
				}
			}
		}
		if strings.ContainsAny(line, "\n\r") {
			t.Errorf("%s: raw newline in CEF line: %q", ev.action, line)
		}
	}
}
