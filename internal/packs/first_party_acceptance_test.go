package packs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/skills"
)

// firstPartyName is the name of a registry the skills package flags first
// party, so the acceptance tests run against a real first-party source.
var firstPartyName = skills.CommunityDefaultRegistry.Name

// namedFirstPartyPack loads a pack with the given name and version, sourced
// from the first-party registry. extra is appended to the task list in the
// manifest, and script is the verification script's content, so the digest
// can be changed without changing the name or version.
func namedFirstPartyPack(t *testing.T, name, version, script, extra string) *Pack {
	t.Helper()
	manifest := "name: " + name + "\nversion: " + version + "\ndescription: first party\n" +
		"tasks:\n  - title: Check the tree\n    verification_script: verify/check.sh\n" + extra
	dir := writePackDir(t, manifest, map[string]string{"verify/check.sh": script})
	p, err := LoadPackFolder(dir)
	if err != nil {
		t.Fatalf("LoadPackFolder: %v", err)
	}
	p.Source = firstPartyName
	return p
}

// registerTestSource registers a first-party source whose lister returns packs
// (or listErr) and whose trust records go under root. The registration is
// cleared when the test ends.
func registerTestSource(t *testing.T, root string, packs []*Pack, listErr error) {
	t.Helper()
	RegisterFirstPartySource(FirstPartySource{
		InstallRoot: root,
		CurrentPacks: func(string) ([]*Pack, error) {
			return packs, listErr
		},
	})
	t.Cleanup(func() { RegisterFirstPartySource(FirstPartySource{}) })
}

// assertNoTrustFile fails the test when trust.json exists under root, which
// would mean a refused acceptance wrote something.
func assertNoTrustFile(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, trustFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("trust.json exists after a refused acceptance (stat err = %v)", err)
	}
}

func TestAcceptFirstPartyWritesOneRecordPerCurrentPack(t *testing.T) {
	root := t.TempDir()
	alpha := namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript, "")
	beta := namedFirstPartyPack(t, "beta-pack", "2.1.0", shellScript, "")
	registerTestSource(t, root, []*Pack{alpha, beta}, nil)

	now := time.Date(2026, 11, 10, 9, 30, 0, 0, time.FixedZone("EST", -5*3600))
	recs, err := AcceptFirstParty(firstPartyName, "operator", now)
	if err != nil {
		t.Fatalf("AcceptFirstParty: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("records returned = %d, want 2", len(recs))
	}

	store, err := LoadTrustStore(root)
	if err != nil {
		t.Fatalf("LoadTrustStore: %v", err)
	}
	if n := len(store.records); n != 2 {
		t.Fatalf("records on disk = %d, want 2", n)
	}
	for _, p := range []*Pack{alpha, beta} {
		rec, ok := store.Record(p.Name, p.Version)
		if !ok {
			t.Fatalf("no record for %s %s", p.Name, p.Version)
		}
		if rec.Digest != p.Digest {
			t.Errorf("%s digest = %q, want %q", p.Name, rec.Digest, p.Digest)
		}
		if rec.AcceptedBy != "operator" {
			t.Errorf("%s accepted_by = %q, want operator", p.Name, rec.AcceptedBy)
		}
		if !rec.AcceptedAt.Equal(now) || rec.AcceptedAt.Location() != time.UTC {
			t.Errorf("%s accepted_at = %v, want %v in UTC", p.Name, rec.AcceptedAt, now)
		}
	}
	// The accepted packs run without further checks.
	for _, p := range []*Pack{alpha, beta} {
		if err := store.Runnable(p); err != nil {
			t.Errorf("Runnable(%s) after first-party acceptance: %v", p.Name, err)
		}
	}
}

func TestAcceptFirstPartyRefusesNonFirstPartySource(t *testing.T) {
	root := t.TempDir()
	p := namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript, "")
	p.Source = "someone-else"
	registerTestSource(t, root, []*Pack{p}, nil)

	if _, err := AcceptFirstParty("someone-else", "operator", acceptedAt); err == nil {
		t.Fatal("AcceptFirstParty on a non-first-party source: want error, got nil")
	}
	assertNoTrustFile(t, root)
}

func TestAcceptFirstPartyRefusesEmptySourceName(t *testing.T) {
	root := t.TempDir()
	registerTestSource(t, root, []*Pack{namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript, "")}, nil)
	if _, err := AcceptFirstParty("", "operator", acceptedAt); err == nil {
		t.Fatal("AcceptFirstParty with empty source name: want error, got nil")
	}
	assertNoTrustFile(t, root)
}

func TestAcceptFirstPartyRefusesWhenNoSourceRegistered(t *testing.T) {
	RegisterFirstPartySource(FirstPartySource{})
	if _, err := AcceptFirstParty(firstPartyName, "operator", acceptedAt); err == nil {
		t.Fatal("AcceptFirstParty with no registered source: want error, got nil")
	}
}

func TestAcceptFirstPartyRefusesWhenListerMissing(t *testing.T) {
	RegisterFirstPartySource(FirstPartySource{InstallRoot: t.TempDir()})
	if _, err := AcceptFirstParty(firstPartyName, "operator", acceptedAt); err == nil {
		t.Fatal("AcceptFirstParty with no lister: want error, got nil")
	}
}

func TestAcceptFirstPartyPropagatesListerError(t *testing.T) {
	root := t.TempDir()
	registerTestSource(t, root, nil, errors.New("registry clone missing"))
	if _, err := AcceptFirstParty(firstPartyName, "operator", acceptedAt); err == nil {
		t.Fatal("AcceptFirstParty with failing lister: want error, got nil")
	}
	assertNoTrustFile(t, root)
}

func TestAcceptFirstPartyRefusesSourceWithNoCurrentPacks(t *testing.T) {
	root := t.TempDir()
	registerTestSource(t, root, nil, nil)
	if _, err := AcceptFirstParty(firstPartyName, "operator", acceptedAt); err == nil {
		t.Fatal("AcceptFirstParty with no packs: want error, got nil")
	}
	assertNoTrustFile(t, root)
}

func TestAcceptFirstPartyRefusesNilPackWritesNothing(t *testing.T) {
	root := t.TempDir()
	good := namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript, "")
	registerTestSource(t, root, []*Pack{good, nil}, nil)
	if _, err := AcceptFirstParty(firstPartyName, "operator", acceptedAt); err == nil {
		t.Fatal("AcceptFirstParty with a nil pack: want error, got nil")
	}
	assertNoTrustFile(t, root)
}

func TestAcceptFirstPartyRefusesPackFromOtherSourceWritesNothing(t *testing.T) {
	root := t.TempDir()
	good := namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript, "")
	foreign := namedFirstPartyPack(t, "beta-pack", "1.0.0", shellScript, "")
	foreign.Source = "pai"
	registerTestSource(t, root, []*Pack{good, foreign}, nil)
	if _, err := AcceptFirstParty(firstPartyName, "operator", acceptedAt); err == nil {
		t.Fatal("AcceptFirstParty with a pack from another source: want error, got nil")
	}
	assertNoTrustFile(t, root)
}

func TestAcceptFirstPartyRefusesDigestlessPackWritesNothing(t *testing.T) {
	root := t.TempDir()
	good := namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript, "")
	bad := namedFirstPartyPack(t, "beta-pack", "1.0.0", shellScript, "")
	bad.Digest = ""
	registerTestSource(t, root, []*Pack{good, bad}, nil)
	if _, err := AcceptFirstParty(firstPartyName, "operator", acceptedAt); err == nil {
		t.Fatal("AcceptFirstParty with a digestless pack: want error, got nil")
	}
	// The valid pack must not be recorded either: the batch is all or nothing.
	assertNoTrustFile(t, root)
}

func TestAcceptFirstPartyRefusesBlankAcceptedBy(t *testing.T) {
	root := t.TempDir()
	registerTestSource(t, root, []*Pack{namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript, "")}, nil)
	if _, err := AcceptFirstParty(firstPartyName, "   ", acceptedAt); err == nil {
		t.Fatal("AcceptFirstParty with blank accepted_by: want error, got nil")
	}
	assertNoTrustFile(t, root)
}

func TestAcceptFirstPartyFailsWhenRootIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	registerTestSource(t, file, []*Pack{namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript, "")}, nil)
	if _, err := AcceptFirstParty(firstPartyName, "operator", acceptedAt); err == nil {
		t.Fatal("AcceptFirstParty with root as a file: want error, got nil")
	}
}

func TestAcceptFirstPartyReplacesRecordForChangedContent(t *testing.T) {
	root := t.TempDir()
	first := namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript, "")
	registerTestSource(t, root, []*Pack{first}, nil)
	if _, err := AcceptFirstParty(firstPartyName, "operator", acceptedAt); err != nil {
		t.Fatalf("first acceptance: %v", err)
	}

	// Same name and version, different script content: a new digest.
	changed := namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript+"echo changed\n", "")
	if changed.Digest == first.Digest {
		t.Fatal("fixture error: changed content kept the digest")
	}
	registerTestSource(t, root, []*Pack{changed}, nil)
	if _, err := AcceptFirstParty(firstPartyName, "second", acceptedAt.Add(time.Hour)); err != nil {
		t.Fatalf("second acceptance: %v", err)
	}

	store, err := LoadTrustStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(store.records); n != 1 {
		t.Fatalf("records on disk = %d, want 1", n)
	}
	rec, _ := store.Record("alpha-pack", "1.0.0")
	if rec.Digest != changed.Digest || rec.AcceptedBy != "second" {
		t.Errorf("record = %+v, want the changed digest accepted by second", rec)
	}
}

func TestAcceptFirstPartyLaterVersionGoesThroughDiffRules(t *testing.T) {
	root := t.TempDir()
	v1 := namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript, "")
	registerTestSource(t, root, []*Pack{v1}, nil)
	if _, err := AcceptFirstParty(firstPartyName, "operator", acceptedAt); err != nil {
		t.Fatalf("AcceptFirstParty: %v", err)
	}
	store, err := LoadTrustStore(root)
	if err != nil {
		t.Fatal(err)
	}

	// A later version is not covered by the acceptance: it has no record.
	if _, ok := store.Record("alpha-pack", "2.0.0"); ok {
		t.Fatal("a later version has a record from the first-party acceptance")
	}

	// A later version that adds a tool widens access, so the auto-accept rule
	// refuses it and it stays unaccepted.
	widening := namedFirstPartyPack(t, "alpha-pack", "2.0.0", shellScript, "    tools: [git]\n")
	if !DiffVersions(v1, widening).WidensAccess() {
		t.Fatal("fixture error: tool addition did not widen access")
	}
	if _, applied, err := AutoAcceptUpdate(store, v1, widening, true, firstPartyName, acceptedAt); err != nil || applied {
		t.Fatalf("widening version: applied=%v err=%v, want not applied", applied, err)
	}
	if _, ok := store.Record("alpha-pack", "2.0.0"); ok {
		t.Error("widening version was recorded despite the diff rule")
	}

	// A later version that only adds a task is auto-accepted by the rule.
	additive := namedFirstPartyPack(t, "alpha-pack", "3.0.0", shellScript,
		"  - title: Second check\n    verification_script: verify/check.sh\n")
	if DiffVersions(v1, additive).WidensAccess() {
		t.Fatal("fixture error: added task widened access")
	}
	rec, applied, err := AutoAcceptUpdate(store, v1, additive, true, firstPartyName, acceptedAt)
	if err != nil || !applied {
		t.Fatalf("additive version: applied=%v err=%v, want applied", applied, err)
	}
	if rec.AcceptedBy != "auto:"+firstPartyName {
		t.Errorf("accepted_by = %q, want auto:%s", rec.AcceptedBy, firstPartyName)
	}
}

func TestAcceptFirstPartyRefusesAfterUnregister(t *testing.T) {
	root := t.TempDir()
	registerTestSource(t, root, []*Pack{namedFirstPartyPack(t, "alpha-pack", "1.0.0", shellScript, "")}, nil)
	RegisterFirstPartySource(FirstPartySource{})
	if _, err := AcceptFirstParty(firstPartyName, "operator", acceptedAt); err == nil {
		t.Fatal("after unregistering, AcceptFirstParty: want error, got nil")
	}
	assertNoTrustFile(t, root)
}
