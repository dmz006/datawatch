package autonomous

import (
	"strings"
	"testing"
)

// BL469: two PRDs on one tree must not both be admitted to run. The key is
// per directory when the dir is not a git repo, so a plain temp dir is enough.
func TestAdmitTree_RefusesSecondRunningPRDOnSameTree(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := t.TempDir()
	first, err := m.CreatePRD("first", tree, "", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.CreatePRD("second", tree, "", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	first.Status = PRDRunning

	err = m.admitTree(second)
	if err == nil {
		t.Fatal("second PRD on a running tree must be refused")
	}
	if !strings.Contains(err.Error(), "tree busy") || !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("error should name the holder: %v", err)
	}
}

func TestAdmitTree_AdmitsAfterHolderFinishes(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := t.TempDir()
	first, _ := m.CreatePRD("first", tree, "", "", EffortNormal)
	second, _ := m.CreatePRD("second", tree, "", "", EffortNormal)
	first.Status = PRDRunning
	first.Status = PRDCompleted
	if err := m.admitTree(second); err != nil {
		t.Fatalf("second PRD should be admitted once the holder completes: %v", err)
	}
}

func TestAdmitTree_DifferentTreesDoNotCollide(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := m.CreatePRD("first", t.TempDir(), "", "", EffortNormal)
	second, _ := m.CreatePRD("second", t.TempDir(), "", "", EffortNormal)
	first.Status = PRDRunning
	if err := m.admitTree(second); err != nil {
		t.Fatalf("PRDs on different trees must both be admitted: %v", err)
	}
}

// A recursive child PRD runs on its parent's tree while the parent waits on
// it. The running parent must not block its own child.
func TestAdmitTree_ChildOfRunningParentIsAdmitted(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := t.TempDir()
	parent, _ := m.CreatePRD("parent", tree, "", "", EffortNormal)
	child, _ := m.CreatePRD("child", tree, "", "", EffortNormal)
	parent.Status = PRDRunning
	child.ParentPRDID = parent.ID
	if err := m.admitTree(child); err != nil {
		t.Fatalf("child of the running parent must be admitted: %v", err)
	}
}
