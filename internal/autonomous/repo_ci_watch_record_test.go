// Repo CI watch record — builder and key tests (pure, no files).
package autonomous

import (
	"strings"
	"testing"
	"time"
)

func testRepoCIWatchInputs(status, classification string) map[string]string {
	return map[string]string{
		"repo":           "dmz006/datawatch",
		"ref_kind":       "main",
		"ref_name":       "main",
		"sha":            "f1f20051deadbeefcafebabe0123456789abcdef",
		"workflow":       "ci.yml",
		"run_id":         "1234567890",
		"run_url":        "https://github.com/dmz006/datawatch/actions/runs/1234567890",
		"status":         status,
		"classification": classification,
	}
}

func buildTestRepoCIWatchRecord(t *testing.T, status, classification string, now time.Time) *RepoCIWatchRecord {
	t.Helper()
	in := testRepoCIWatchInputs(status, classification)
	rec, err := BuildRepoCIWatchRecord(
		in["repo"], in["ref_kind"], in["ref_name"], in["sha"], in["workflow"], in["run_id"],
		in["run_url"], status, classification, now)
	if err != nil {
		t.Fatalf("BuildRepoCIWatchRecord(status=%q): %v", status, err)
	}
	return rec
}

// TestRepoCIWatchRecordBuilder covers one record per status in the
// record's vocabulary, plus the classification default.
func TestRepoCIWatchRecordBuilder(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	for _, status := range RepoCIWatchStatuses {
		t.Run(status, func(t *testing.T) {
			rec := buildTestRepoCIWatchRecord(t, status, "", now)
			if rec.Status != status {
				t.Fatalf("Status = %q, want %q", rec.Status, status)
			}
			if rec.Classification != "none" {
				t.Fatalf("Classification = %q, want \"none\"", rec.Classification)
			}
			if rec.Repo != "dmz006/datawatch" || rec.RefKind != "main" || rec.RefName != "main" {
				t.Fatalf("identity fields wrong: %+v", rec)
			}
			if rec.SHA != "f1f20051deadbeefcafebabe0123456789abcdef" || rec.Workflow != "ci.yml" || rec.RunID != "1234567890" {
				t.Fatalf("identity fields wrong: %+v", rec)
			}
			if rec.RunURL == "" {
				t.Fatal("RunURL not set")
			}
			if !rec.FirstSeenAt.Equal(now) || !rec.UpdatedAt.Equal(now) {
				t.Fatalf("timestamps: FirstSeenAt=%v UpdatedAt=%v, want %v", rec.FirstSeenAt, rec.UpdatedAt, now)
			}
			if rec.AlertedAt != nil {
				t.Fatal("AlertedAt must be nil until the alert is raised")
			}
			if rec.RerunLog != nil || rec.WouldDo != nil || rec.SupersededRunIDs != nil || rec.Notes != nil || rec.JudgeVerdict != nil {
				t.Fatalf("fresh record must start empty: %+v", rec)
			}

			// A second status with an explicit classification.
		})
	}

	// Every classification in the vocabulary builds.
	for _, c := range RepoCIWatchClassifications {
		if rec := buildTestRepoCIWatchRecord(t, "failing", c, now); rec.Classification != c {
			t.Fatalf("Classification = %q, want %q", rec.Classification, c)
		}
	}

	// A green record with its resolved classification.
	green := buildTestRepoCIWatchRecord(t, "green", "infra", now)
	if green.Status != "green" || green.Classification != "infra" {
		t.Fatalf("green record = %+v", green)
	}
}

// TestRepoCIWatchRecordBuilderRejectsInvalid pins the builder's input
// validation: each identity field alone, a bad ref kind, an unknown
// status and an unknown classification must all be refused.
func TestRepoCIWatchRecordBuilderRejectsInvalid(t *testing.T) {
	now := time.Now()
	fields := []string{"repo", "ref_kind", "ref_name", "sha", "workflow", "run_id"}
	in := testRepoCIWatchInputs("failing", "")
	for _, name := range fields {
		broken := testRepoCIWatchInputs("failing", "")
		broken[name] = "  "
		if _, err := BuildRepoCIWatchRecord(
			broken["repo"], broken["ref_kind"], broken["ref_name"], broken["sha"], broken["workflow"], broken["run_id"],
			broken["run_url"], broken["status"], broken["classification"], now); err == nil {
			t.Errorf("builder accepted empty %s", name)
		} else if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name field %q", err, name)
		}
	}

	bad := in
	bad["ref_kind"] = "branch"
	if _, err := BuildRepoCIWatchRecord(bad["repo"], bad["ref_kind"], bad["ref_name"], bad["sha"], bad["workflow"], bad["run_id"], bad["run_url"], bad["status"], bad["classification"], now); err == nil {
		t.Error("builder accepted ref kind \"branch\"")
	}

	if _, err := BuildRepoCIWatchRecord(in["repo"], "main", in["ref_name"], in["sha"], in["workflow"], in["run_id"], in["run_url"], "exploded", "", now); err == nil {
		t.Error("builder accepted unknown status \"exploded\"")
	}

	if _, err := BuildRepoCIWatchRecord(in["repo"], in["ref_kind"], in["ref_name"], in["sha"], in["workflow"], in["run_id"], in["run_url"], "failing", "mystery", now); err == nil {
		t.Error("builder accepted unknown classification \"mystery\"")
	}
}

// TestRepoCIWatchRecordKey pins the composite key: each identity
// dimension distinguishes one record from the next, and the key is
// stable for the record's own accessor.
func TestRepoCIWatchRecordKey(t *testing.T) {
	now := time.Now()
	cloneInputs := func() map[string]string { return testRepoCIWatchInputs("failing", "") }

	baseKey := RepoCIWatchKey(
		"dmz006/datawatch", "main", "main",
		"f1f20051deadbeefcafebabe0123456789abcdef", "ci.yml", "1234567890")
	if baseKey == "" {
		t.Fatal("key from valid inputs is empty")
	}

	mutators := []struct {
		name   string
		mutate func(map[string]string)
		wantIn string
	}{
		{"repo", func(m map[string]string) { m["repo"] = "dmz006/other" }, "other"},
		{"ref_kind", func(m map[string]string) { m["ref_kind"] = "tag"; m["ref_name"] = "v2.1.0" }, "v2.1.0"},
		{"ref_name", func(m map[string]string) { m["ref_name"] = "v3.0.0" }, "v3.0.0"},
		{"sha", func(m map[string]string) { m["sha"] = "abcdef0123456789abcdef0123456789abcdef01" }, "abcdef0123456789abcdef"},
		{"workflow", func(m map[string]string) { m["workflow"] = "nightly.yml" }, "nightly.yml"},
		{"run_id", func(m map[string]string) { m["run_id"] = "9876543210" }, "9876543210"},
	}
	for _, tc := range mutators {
		m := cloneInputs()
		tc.mutate(m)
		key := RepoCIWatchKey(m["repo"], m["ref_kind"], m["ref_name"], m["sha"], m["workflow"], m["run_id"])
		if key == baseKey {
			t.Errorf("%s mutation produced the same key as the base", tc.name)
		}
		if !strings.Contains(key, tc.wantIn) {
			t.Errorf("%s key %q does not contain %q", tc.name, key, tc.wantIn)
		}
	}

	// A repo path containing a slash must not collide with a different
	// split of the same string.
	k1 := RepoCIWatchKey("a/b", "main", "c", "sha", "w.yaml", "1")
	k2 := RepoCIWatchKey("a", "main", "b/c", "sha", "w.yaml", "1")
	if k1 == k2 {
		t.Fatalf("slash-ambiguous repo/ref splits collide: %q", k1)
	}

	// The record's own accessor agrees with the composed key.
	rec, err := BuildRepoCIWatchRecord(
		"a/b", "main", "c", "sha", "w.yaml", "1", "", "pending", "", now)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if rec.Key() != k1 {
		t.Fatalf("rec.Key() = %q, want %q", rec.Key(), k1)
	}
}
