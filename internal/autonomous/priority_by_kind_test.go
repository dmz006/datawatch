package autonomous

import "testing"

// shippedTable mirrors config.DefaultPriorityByKindMap (bug=80,
// maintenance=70, feature=20, other=0) — the table main.go wires as the
// effective default.
var shippedTable = map[string]int{
	PRDKindBug:         80,
	PRDKindMaintenance: 70,
	PRDKindFeature:     20,
	PRDKindOther:       0,
}

func TestResolvePriority_ExplicitWinsOverKind(t *testing.T) {
	p := &PRD{Priority: 5, PrioritySource: PrioritySourceExplicit}
	if got, src := ResolvePriority(p, shippedTable); got != 5 || src != PrioritySourceExplicit {
		t.Errorf("explicit 5 must win over the feature default, got (%d, %q)", got, src)
	}
	// Even when the table holds a higher value for the PRD's kind.
	p.Kind = PRDKindBug
	if got, src := ResolvePriority(p, shippedTable); got != 5 || src != PrioritySourceExplicit {
		t.Errorf("explicit 5 must win over the table's bug 80, got (%d, %q)", got, src)
	}
}

func TestResolvePriority_ExplicitZeroStillCounts(t *testing.T) {
	p := &PRD{Priority: 0, PrioritySource: PrioritySourceExplicit}
	if got, src := ResolvePriority(p, shippedTable); got != 0 || src != PrioritySourceExplicit {
		t.Errorf("an explicit 0 must count as explicit, got (%d, %q)", got, src)
	}
}

func TestResolvePriority_ExplicitBeatsOperatorOverride(t *testing.T) {
	p := &PRD{Priority: 7, PrioritySource: PrioritySourceExplicit}
	table := map[string]int{PRDKindBug: 1, PRDKindOther: 2}
	if got, src := ResolvePriority(p, table); got != 7 || src != PrioritySourceExplicit {
		t.Errorf("explicit 7 must win over the operator's table, got (%d, %q)", got, src)
	}
}

func TestResolvePriority_KindDefaultFromShippedTable(t *testing.T) {
	for kind, want := range map[string]int{
		PRDKindBug:         80,
		PRDKindMaintenance: 70,
		PRDKindFeature:     20,
		PRDKindOther:       0,
	} {
		p := &PRD{Title: "Add a new capability", Kind: kind}
		if got, src := ResolvePriority(p, shippedTable); got != want || src != PrioritySourceKind {
			t.Errorf("kind %q: want (%d, %q), got (%d, %q)", kind, want, PrioritySourceKind, got, src)
		}
	}
}

func TestResolvePriority_MaintenanceIsItsOwnKey(t *testing.T) {
	// A table that deliberately separates maintenance from other: the
	// maintenance key must be used, not the other fallback.
	table := map[string]int{PRDKindMaintenance: 60, PRDKindOther: 5}
	p := &PRD{Title: "Tidy the docs", Kind: PRDKindMaintenance}
	if got, src := ResolvePriority(p, table); got != 60 || src != PrioritySourceKind {
		t.Errorf("maintenance key must be looked up as its own entry, got (%d, %q)", got, src)
	}
}

func TestResolvePriority_OperatorOverridesShippedValues(t *testing.T) {
	table := map[string]int{PRDKindBug: 3, PRDKindFeature: 2, PRDKindOther: 1}
	for _, kind := range []string{PRDKindBug, PRDKindFeature, PRDKindOther} {
		p := &PRD{Title: "Fix the leak", Kind: kind}
		got, src := ResolvePriority(p, table)
		if got != table[kind] || src != PrioritySourceKind {
			t.Errorf("kind %q: want (%d, %q), got (%d, %q)", kind, table[kind], PrioritySourceKind, got, src)
		}
	}
}

func TestResolvePriority_UnknownKindFallsToOther(t *testing.T) {
	// Neither feature nor maintenance is in this table: both must resolve
	// through the "other" entry.
	table := map[string]int{PRDKindBug: 9, PRDKindOther: 4}
	for _, kind := range []string{PRDKindFeature, PRDKindMaintenance} {
		p := &PRD{Title: "Introduce a rewrite", Kind: kind}
		if got, src := ResolvePriority(p, table); got != 4 || src != PrioritySourceKind {
			t.Errorf("kind %q absent from the table: want (4, %q), got (%d, %q)", kind, PrioritySourceKind, got, src)
		}
	}
	// No "other" entry at all: a kind the table doesn't know reads
	// (0, default).
	if got, src := ResolvePriority(&PRD{Kind: PRDKindFeature}, map[string]int{PRDKindBug: 9}); got != 0 || src != PrioritySourceDefault {
		t.Errorf("no \"other\" entry: want (0, %q), got (%d, %q)", PrioritySourceDefault, got, src)
	}
}

func TestResolvePriority_EmptyMapFallsToDefault(t *testing.T) {
	p := &PRD{Title: "Fix the leak", Kind: PRDKindBug}
	for _, table := range []map[string]int{map[string]int{}, nil} {
		if got, src := ResolvePriority(p, table); got != 0 || src != PrioritySourceDefault {
			t.Errorf("table %v: want (0, %q), got (%d, %q)", table, PrioritySourceDefault, got, src)
		}
	}
}

func TestResolvePriority_NilPRD(t *testing.T) {
	if got, src := ResolvePriority(nil, nil); got != 0 || src != PrioritySourceDefault {
		t.Errorf("nil PRD: want (0, %q), got (%d, %q)", PrioritySourceDefault, got, src)
	}
}

func TestCreatePRD_ResolvesPriorityFromKindTable(t *testing.T) {
	m, err := NewManager(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPriorityByKind(shippedTable)
	prd, err := m.CreatePRD("Add a new capability", "", "", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	if prd.Priority != 20 || prd.PrioritySource != PrioritySourceKind {
		t.Fatalf("feature PRD at creation: got (%d, %q), want (20, %q)", prd.Priority, prd.PrioritySource, PrioritySourceKind)
	}
	// An explicit pin recorded right after creation still wins when the
	// capacity reader resolves the same PRD.
	if err := m.SetPRDPriority(prd.ID, 30); err != nil {
		t.Fatal(err)
	}
	got, src := ResolvePriority(prd, m.PriorityByKind())
	if got != 30 || src != PrioritySourceExplicit {
		t.Errorf("after SetPRDPriority(30): want (30, %q), got (%d, %q)", PrioritySourceExplicit, got, src)
	}
	// ...and an explicit zero still reads 0, not the table's 20.
	if err := m.SetPRDPriority(prd.ID, 0); err != nil {
		t.Fatal(err)
	}
	if got, src := ResolvePriority(prd, m.PriorityByKind()); got != 0 || src != PrioritySourceExplicit {
		t.Errorf("after SetPRDPriority(0): want (0, %q), got (%d, %q)", PrioritySourceExplicit, got, src)
	}
}

func TestSetPRDPriority_RecordsExplicitSourceInStore(t *testing.T) {
	m, err := NewManager(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPriorityByKind(shippedTable)
	prd, err := m.CreatePRD("Fix the leak", "", "", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetPRDPriority(prd.ID, 0); err != nil {
		t.Fatal(err)
	}
	stored, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing from store")
	}
	if stored.Priority != 0 || stored.PrioritySource != PrioritySourceExplicit {
		t.Errorf("stored record: got (%d, %q), want (0, %q)", stored.Priority, stored.PrioritySource, PrioritySourceExplicit)
	}
	// A brand-new reader (fresh ResolvePriority against the stored record,
	// as the capacity queue does) must still see the explicit zero.
	if got, src := ResolvePriority(stored, m.PriorityByKind()); got != 0 || src != PrioritySourceExplicit {
		t.Errorf("resolved from stored record: want (0, %q), got (%d, %q)", PrioritySourceExplicit, got, src)
	}
}

func TestSetPriorityByKind_RewiresResolution(t *testing.T) {
	m, err := NewManager(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPriorityByKind(map[string]int{PRDKindBug: 5, PRDKindOther: 2})
	if got, src := ResolvePriority(&PRD{Kind: PRDKindBug}, m.PriorityByKind()); got != 5 || src != PrioritySourceKind {
		t.Errorf("operator table: want (5, %q), got (%d, %q)", PrioritySourceKind, got, src)
	}
	// Clearing the table turns unresolved priorities back to 0.
	m.SetPriorityByKind(nil)
	if got, src := ResolvePriority(&PRD{Kind: PRDKindBug}, m.PriorityByKind()); got != 0 || src != PrioritySourceDefault {
		t.Errorf("cleared table: want (0, %q), got (%d, %q)", PrioritySourceDefault, got, src)
	}
	// Explicit pins survive the table change and the clearing.
	p := &PRD{Kind: PRDKindBug, Priority: 9, PrioritySource: PrioritySourceExplicit}
	if got, src := ResolvePriority(p, m.PriorityByKind()); got != 9 || src != PrioritySourceExplicit {
		t.Errorf("explicit after clear: want (9, %q), got (%d, %q)", PrioritySourceExplicit, got, src)
	}
}
