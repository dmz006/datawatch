package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The milestone flag on the Automation (arc) create and update paths over
// /api/automations. Create takes the flag as given; update changes it only
// when the field is present, so an omitted flag keeps the stored value.

type milestoneWire struct {
	ID        string `json:"id"`
	Version   int64  `json:"version"`
	Milestone bool   `json:"milestone"`
}

func TestAutomationMilestone_CreateTakesFlag(t *testing.T) {
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	members := memberWire(makeMemberPRD(t, mgr))

	body := doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "1.0 arc", "actor": "operator", "members": members, "milestone": true,
	}, http.StatusCreated)
	var created milestoneWire
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.ID == "" {
		t.Fatalf("decode create: %v (%s)", err, body)
	}
	if !created.Milestone {
		t.Fatalf("create with milestone:true must return milestone true: %s", body)
	}
	if stored, ok := mgr.GetAutomation(created.ID); !ok || !stored.Milestone {
		t.Fatal("milestone not stored on the Automation")
	}

	body = doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "plain arc", "actor": "operator", "members": memberWire(makeMemberPRD(t, mgr)),
	}, http.StatusCreated)
	var plain milestoneWire
	if err := json.Unmarshal([]byte(body), &plain); err != nil {
		t.Fatalf("decode plain create: %v", err)
	}
	if plain.Milestone {
		t.Fatal("omitted milestone on create must be false")
	}
}

func TestAutomationMilestone_UpdateSetClearAndKeep(t *testing.T) {
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	members := memberWire(makeMemberPRD(t, mgr))
	created := doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "arc", "actor": "operator", "members": members,
	}, http.StatusCreated)
	var a milestoneWire
	if err := json.Unmarshal([]byte(created), &a); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	// Set the flag on update.
	updated := doAutomation(t, s, http.MethodPut, "/api/automations/"+a.ID, map[string]any{
		"name": "arc", "actor": "operator", "expected_version": a.Version, "members": members, "milestone": true,
	}, http.StatusOK)
	var u milestoneWire
	if err := json.Unmarshal([]byte(updated), &u); err != nil || !u.Milestone {
		t.Fatalf("update with milestone:true: err=%v body=%s", err, updated)
	}

	// An update that omits the field keeps the stored value.
	kept := doAutomation(t, s, http.MethodPut, "/api/automations/"+a.ID, map[string]any{
		"name": "arc renamed", "actor": "operator", "expected_version": u.Version, "members": members,
	}, http.StatusOK)
	var k milestoneWire
	if err := json.Unmarshal([]byte(kept), &k); err != nil || !k.Milestone {
		t.Fatalf("omitted milestone must keep the stored value: err=%v body=%s", err, kept)
	}

	// An explicit false clears it.
	cleared := doAutomation(t, s, http.MethodPut, "/api/automations/"+a.ID, map[string]any{
		"name": "arc renamed", "actor": "operator", "expected_version": k.Version, "members": members, "milestone": false,
	}, http.StatusOK)
	var c milestoneWire
	if err := json.Unmarshal([]byte(cleared), &c); err != nil || c.Milestone {
		t.Fatalf("milestone:false must clear the flag: err=%v body=%s", err, cleared)
	}
}

func TestAutomationMilestone_RejectsNonBoolean(t *testing.T) {
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	members := memberWire(makeMemberPRD(t, mgr))
	req := map[string]any{"name": "arc", "actor": "operator", "members": members, "milestone": "yes"}
	doAutomation(t, s, http.MethodPost, "/api/automations", req, http.StatusBadRequest)
}
