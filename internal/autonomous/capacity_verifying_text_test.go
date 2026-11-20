package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestCapacityTextVerifying(t *testing.T) {
	old := CapacityTextClock
	CapacityTextClock = func() time.Time { return time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC) }
	defer func() { CapacityTextClock = old }()
	body := `{"tasks":{"waiting":[],"running":[],"verifying":[
	 {"task_id":"t1","prd_id":"p1","readable":"Plan / Check (aaaa1111)","state":"verifying","since":"2026-10-22T16:00:00Z"},
	 {"task_id":"t2","prd_id":"p1","readable":"Plan / Other (bbbb2222)","state":"waiting_for_verifier","wait_reason":"pool llm:x full","since":"2026-10-22T16:00:00Z"}]}}`
	out, err := FormatCapacityText([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Verifying (2):", "Plan / Check (aaaa1111): verifying", "Plan / Other (bbbb2222): waiting_for_verifier - pool llm:x full"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	none, _ := FormatCapacityText([]byte(`{"tasks":{"verifying":[]}}`))
	if strings.Contains(none, "Verifying") {
		t.Errorf("empty verifying must print no section:\n%s", none)
	}
}
