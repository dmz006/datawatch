package autonomous

import (
	"strings"
	"testing"
	"time"
)

// The body's node:a pool carries a usage object; the host pool does not.
const capacityNodeUsageBody = `{
"pools":[
{"name":"host","limit":9,"held":2,"holders":["t1","t2"]},
{"name":"node:a","limit":1,"held":1,"holders":["t7"],
 "usage":%s}],
"leases":[],"waiting":[],
"tasks":{"waiting":[],"running":[]}}`

func capacityNodeUsageClock(t *testing.T) {
	t.Helper()
	CapacityTextClock = func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { CapacityTextClock = time.Now })
}

func renderCapacityNodeUsage(t *testing.T, usage string) string {
	t.Helper()
	out, err := FormatCapacityText([]byte(strings.Replace(capacityNodeUsageBody, "%s", usage, 1)))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// 5 GiB (5368709120 bytes) so the one-decimal GiB figure is exact.
const fiveGiB = "5368709120"

func TestCapacityNodeUsage_Present(t *testing.T) {
	capacityNodeUsageClock(t)
	// Clock is 17:00; expires_at 17:29 -> "expires in 29m0s".
	out := renderCapacityNodeUsage(t, `{"state":"ok","checked_at":"2026-10-22T16:59:00Z","models_supported":true,
 "models":[{"name":"qwen3:8b","size_bytes":`+fiveGiB+`,"size_vram_bytes":5162643968,"expires_at":"2026-10-22T17:29:00Z","no_expiry":false}],
 "gpu":[{"name":"NVIDIA A100","util_pct":12.5,"mem_used_bytes":10737418240,"mem_total_bytes":42949672960}]}`)
	for _, want := range []string{
		"node:a: 1/1 held by t7",
		"  model: qwen3:8b 5.0 GiB in memory, expires in 29m0s",
		"  GPU NVIDIA A100: 12.5% util, 10.0 of 40.0 GiB memory",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// The usage lines sit directly under the node pool line.
	iPool := strings.Index(out, "node:a: 1/1")
	iModel := strings.Index(out, "  model: qwen3:8b")
	if iPool < 0 || iModel < 0 || iModel <= iPool {
		t.Fatalf("usage lines not under the node pool line:\n%s", out)
	}
	// The host pool (no usage) is untouched.
	if !strings.Contains(out, "host: 2/9 held by t1,t2") {
		t.Fatalf("host pool line changed:\n%s", out)
	}
}

func TestCapacityNodeUsage_Unreachable(t *testing.T) {
	capacityNodeUsageClock(t)
	out := renderCapacityNodeUsage(t, `{"state":"unreachable","checked_at":"2026-10-22T16:59:00Z","error":"connection refused","models_supported":true,"models":[],
 "gpu":[{"name":"NVIDIA A100","util_pct":25,"mem_used_bytes":10737418240,"mem_total_bytes":42949672960}]}`)
	for _, want := range []string{
		"  unreachable: connection refused",
		"  GPU NVIDIA A100: 25% util, 10.0 of 40.0 GiB memory",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "  model: ") {
		t.Fatalf("no model lines for a failed probe:\n%s", out)
	}

	// An empty error still names the state.
	out = renderCapacityNodeUsage(t, `{"state":"unreachable","checked_at":"2026-10-22T16:59:00Z","error":"","models":[],"gpu":[]}`)
	if !strings.Contains(out, "  unreachable\n") {
		t.Fatalf("missing bare unreachable line:\n%s", out)
	}
}

func TestCapacityNodeUsage_Pending(t *testing.T) {
	capacityNodeUsageClock(t)
	out := renderCapacityNodeUsage(t, `{"state":"pending","models_supported":true,"models":[],"gpu":[]}`)
	if !strings.Contains(out, "  checking...") {
		t.Fatalf("missing checking line:\n%s", out)
	}
	if strings.Contains(out, "  model: ") || strings.Contains(out, "  GPU ") {
		t.Fatalf("no model or gpu lines while pending:\n%s", out)
	}
}

func TestCapacityNodeUsage_TwoModels(t *testing.T) {
	capacityNodeUsageClock(t)
	out := renderCapacityNodeUsage(t, `{"state":"ok","checked_at":"2026-10-22T16:59:00Z","models_supported":true,
 "models":[
  {"name":"qwen3:8b","size_bytes":`+fiveGiB+`,"expires_at":"2026-10-22T17:30:00Z","no_expiry":false},
  {"name":"llama3.1:70b","size_bytes":42949672960,"expires_at":"2026-10-22T17:05:00Z","no_expiry":false}],
 "gpu":[]}`)
	for _, want := range []string{
		"  model: qwen3:8b 5.0 GiB in memory, expires in 30m0s",
		"  model: llama3.1:70b 40.0 GiB in memory, expires in 5m0s",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "  GPU ") {
		t.Fatalf("no gpu lines without gpu data:\n%s", out)
	}
}

func TestCapacityNodeUsage_NeverExpiring(t *testing.T) {
	capacityNodeUsageClock(t)
	out := renderCapacityNodeUsage(t, `{"state":"ok","checked_at":"2026-10-22T16:59:00Z","models_supported":true,
 "models":[{"name":"mistral:7b","size_bytes":`+fiveGiB+`,"no_expiry":true}],"gpu":[]}`)
	if !strings.Contains(out, "  model: mistral:7b 5.0 GiB in memory, stays loaded") {
		t.Fatalf("missing stays-loaded line:\n%s", out)
	}
	if strings.Contains(out, "expires in") {
		t.Fatalf("no expiry for a no_expiry model:\n%s", out)
	}
}

func TestCapacityNodeUsage_NoGPU(t *testing.T) {
	capacityNodeUsageClock(t)
	out := renderCapacityNodeUsage(t, `{"state":"ok","checked_at":"2026-10-22T16:59:00Z","models_supported":true,
 "models":[{"name":"qwen3:8b","size_bytes":`+fiveGiB+`,"no_expiry":true}],"gpu":[]}`)
	if strings.Contains(out, "  GPU ") {
		t.Fatalf("GPU line printed for an empty gpu array:\n%s", out)
	}
}

func TestCapacityNodeUsage_NoDataPrintsNothing(t *testing.T) {
	capacityNodeUsageClock(t)
	out := renderCapacityNodeUsage(t, `{"state":"no_data","models":[],"gpu":[]}`)
	for _, frag := range []string{"  model: ", "  GPU ", "  unreachable", "  checking..."} {
		if strings.Contains(out, frag) {
			t.Fatalf("no_data node printed %q:\n%s", frag, out)
		}
	}
}

func TestCapacityNodeUsage_AbsentUnchanged(t *testing.T) {
	out1, err := FormatCapacityText([]byte(`{
"pools":[{"name":"host","limit":9,"held":2,"holders":["t1","t2"]},
{"name":"node:a","limit":1,"held":1,"holders":["t7"]}],
"leases":[],"waiting":[],
"tasks":{"waiting":[],"running":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out1, "  model: ") || strings.Contains(out1, "  GPU ") ||
		strings.Contains(out1, "  checking...") || strings.Contains(out1, "unreachable") {
		t.Fatalf("absence of usage must print no extra lines:\n%s", out1)
	}
	for _, want := range []string{
		"host: 2/9 held by t1,t2",
		"node:a: 1/1 held by t7",
	} {
		if !strings.Contains(out1, want) {
			t.Fatalf("missing %q in:\n%s", want, out1)
		}
	}
}
