package autonomous

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrGraphNotRepresentable is returned (wrapped, with the reason) when an old
// orchestrator graph has a shape an Automation cannot express. Match it with
// errors.Is.
var ErrGraphNotRepresentable = errors.New("graph not representable as an automation")

// ImportGraphNode is one node of the old orchestrator graph, as the converter
// needs it. It mirrors the orchestrator's node without importing that package.
type ImportGraphNode struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"` // "prd" | "guardrail"
	PRDID     string   `json:"prd_id,omitempty"`
	Guardrail string   `json:"guardrail,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// ImportGraphSource is the old orchestrator graph to convert.
type ImportGraphSource struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	ProjectDir string            `json:"project_dir,omitempty"`
	Nodes      []ImportGraphNode `json:"nodes"`
}

// ImportGraphResult is the outcome of Manager.ImportGraph. A real run carries
// the stored Automation; a dry run carries the AutomationInput that a real run
// would create.
type ImportGraphResult struct {
	Automation *Automation      `json:"automation,omitempty"`
	Input      *AutomationInput `json:"input,omitempty"`
	Warnings   []string         `json:"warnings,omitempty"`
	DryRun     bool             `json:"dry_run"`
}

func notRepresentable(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrGraphNotRepresentable, fmt.Sprintf(format, args...))
}

// ImportGraph converts src to an Automation draft. With dryRun nothing is
// stored and nothing is audited.
func (m *Manager) ImportGraph(src ImportGraphSource, dryRun bool) (*ImportGraphResult, error) {
	in, warnings, err := ImportGraphToInput(src)
	if err != nil {
		return nil, err
	}
	if dryRun {
		return &ImportGraphResult{Input: &in, Warnings: warnings, DryRun: true}, nil
	}
	a, err := m.CreateAutomation(in, importGraphActor(src.ID))
	if err != nil {
		return nil, err
	}
	return &ImportGraphResult{Automation: a, Warnings: warnings}, nil
}

func importGraphActor(graphID string) string { return "import:graph:" + graphID }

// ImportGraphToInput is the pure converter behind ImportGraph. It maps prd
// nodes to members, fan-out and fan-in to a paired fork and join, and drops
// guardrail nodes with one warning each.
func ImportGraphToInput(src ImportGraphSource) (AutomationInput, []string, error) {
	var zero AutomationInput
	byID := make(map[string]int, len(src.Nodes))
	for i, n := range src.Nodes {
		if n.ID == "" {
			return zero, nil, notRepresentable("node %d has an empty id", i)
		}
		if _, dup := byID[n.ID]; dup {
			return zero, nil, notRepresentable("duplicate node id %s", n.ID)
		}
		byID[n.ID] = i
		switch n.Kind {
		case "prd", "guardrail":
		default:
			return zero, nil, notRepresentable("node %s has unknown kind %q", n.ID, n.Kind)
		}
	}
	for _, n := range src.Nodes {
		for _, d := range n.DependsOn {
			if _, ok := byID[d]; !ok {
				return zero, nil, notRepresentable("node %s depends on unknown node %s", n.ID, d)
			}
		}
		if n.Kind == "prd" && strings.TrimSpace(n.PRDID) == "" {
			return zero, nil, notRepresentable("prd node %s has an empty prd id", n.ID)
		}
	}
	if err := graphCycleCheck(src.Nodes, byID); err != nil {
		return zero, nil, err
	}

	// Resolve each node's prd dependencies, looking through guardrail nodes.
	var resolve func(id string, seen map[string]bool) []string
	resolve = func(id string, seen map[string]bool) []string {
		var out []string
		for _, d := range src.Nodes[byID[id]].DependsOn {
			if seen[d] {
				continue
			}
			seen[d] = true
			if src.Nodes[byID[d]].Kind == "prd" {
				out = append(out, d)
			} else {
				out = append(out, resolve(d, seen)...)
			}
		}
		return out
	}
	var prdNodes []string
	preds := map[string][]string{}
	succs := map[string][]string{}
	for _, n := range src.Nodes {
		if n.Kind == "prd" {
			prdNodes = append(prdNodes, n.ID)
		}
	}
	if len(prdNodes) == 0 {
		return zero, nil, notRepresentable("the graph has no prd nodes")
	}
	for _, id := range prdNodes {
		ds := uniqueByOrder(resolve(id, map[string]bool{}), byID)
		preds[id] = ds
		for _, d := range ds {
			succs[d] = append(succs[d], id)
		}
	}
	for id := range succs {
		succs[id] = uniqueByOrder(succs[id], byID)
	}

	var warnings []string
	for _, n := range src.Nodes {
		if n.Kind != "guardrail" {
			continue
		}
		name := n.Guardrail
		if name == "" {
			name = n.ID
		}
		var attached []string
		for _, d := range n.DependsOn {
			attached = append(attached, graphAttachedPRDs(d, src.Nodes, byID)...)
		}
		attached = uniqueByOrder(attached, byID)
		if len(attached) == 0 {
			warnings = append(warnings, fmt.Sprintf("guardrail %q (node %s) was not attached to any prd node and was dropped; per-Automaton guardrails now cover it", name, n.ID))
			continue
		}
		for _, p := range attached {
			warnings = append(warnings, fmt.Sprintf("guardrail %q (node %s) was attached to prd node %s and was dropped; per-Automaton guardrails now cover it", name, n.ID, p))
		}
	}

	// A single connected component is required.
	if comps := graphComponents(prdNodes, preds, succs); comps > 1 {
		return zero, nil, notRepresentable("the graph has %d disconnected components; import each separately", comps)
	}

	b := &graphBuilder{
		actor:   importGraphActor(src.ID),
		preds:   preds,
		succs:   succs,
		byIndex: byID,
		src:     src.Nodes,
		index:   map[string]int{},
		used:    map[string]bool{},
	}
	for _, id := range prdNodes {
		b.used[id] = true
	}
	if err := b.build(prdNodes); err != nil {
		return zero, nil, err
	}
	if len(b.visited) != len(prdNodes) {
		return zero, nil, notRepresentable("%d prd nodes could not be placed in the chain", len(prdNodes)-len(b.visited))
	}

	name := strings.TrimSpace(src.Title)
	if name == "" {
		name = "Imported graph " + src.ID
	}
	in := AutomationInput{
		Name:          name,
		Description:   fmt.Sprintf("Imported from orchestrator graph %s.", src.ID),
		EntryMemberID: b.entry,
		Members:       b.members,
		ProjectDir:    src.ProjectDir,
	}
	check := &Automation{Name: in.Name, Status: AutomationDraft, EntryMemberID: in.EntryMemberID, Members: in.Members}
	if err := validateAutomation(check); err != nil {
		return zero, nil, notRepresentable("%v", err)
	}
	return in, warnings, nil
}

// graphAttachedPRDs returns the prd nodes a guardrail dependency refers to,
// looking through chained guardrail nodes.
func graphAttachedPRDs(id string, nodes []ImportGraphNode, byID map[string]int) []string {
	n := nodes[byID[id]]
	if n.Kind == "prd" {
		return []string{id}
	}
	var out []string
	for _, d := range n.DependsOn {
		out = append(out, graphAttachedPRDs(d, nodes, byID)...)
	}
	return out
}

// uniqueByOrder removes duplicates and orders ids by their node position, so
// output does not depend on map iteration or dependency list order.
func uniqueByOrder(ids []string, byID map[string]int) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return byID[out[i]] < byID[out[j]] })
	return out
}

func graphCycleCheck(nodes []ImportGraphNode, byID map[string]int) error {
	const (
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	var path []string
	var dfs func(id string) []string
	dfs = func(id string) []string {
		color[id] = grey
		path = append(path, id)
		for _, d := range nodes[byID[id]].DependsOn {
			switch color[d] {
			case grey:
				i := 0
				for path[i] != d {
					i++
				}
				return append([]string{}, path[i:]...)
			case 0:
				if c := dfs(d); c != nil {
					return c
				}
			}
		}
		color[id] = black
		path = path[:len(path)-1]
		return nil
	}
	for _, n := range nodes {
		if color[n.ID] == 0 {
			if c := dfs(n.ID); c != nil {
				return notRepresentable("dependency cycle through nodes [%s]", strings.Join(c, " "))
			}
		}
	}
	return nil
}

func graphComponents(ids []string, preds, succs map[string][]string) int {
	seen := map[string]bool{}
	count := 0
	for _, id := range ids {
		if seen[id] {
			continue
		}
		count++
		stack := []string{id}
		seen[id] = true
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, nb := range append(append([]string{}, preds[cur]...), succs[cur]...) {
				if !seen[nb] {
					seen[nb] = true
					stack = append(stack, nb)
				}
			}
		}
	}
	return count
}

type graphBuilder struct {
	actor   string
	preds   map[string][]string
	succs   map[string][]string
	byIndex map[string]int
	src     []ImportGraphNode
	index   map[string]int // member id -> position in members
	used    map[string]bool
	visited map[string]bool
	members []AutomationMember
	entry   string
}

func (b *graphBuilder) add(m AutomationMember) {
	m.AddedBy = b.actor
	m.Status = MemberPending
	b.index[m.ID] = len(b.members)
	b.members = append(b.members, m)
}

func (b *graphBuilder) setComplete(id, target string) {
	b.members[b.index[id]].OnComplete = target
}

func (b *graphBuilder) uniqueID(base string) string {
	id := base
	for b.used[id] {
		id += "_"
	}
	b.used[id] = true
	return id
}

func (b *graphBuilder) prdMember(id string) AutomationMember {
	return AutomationMember{ID: id, Kind: MemberKindPRD, PRDID: b.src[b.byIndex[id]].PRDID}
}

func sameIDs(a, c []string) bool {
	if len(a) != len(c) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), c...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func (b *graphBuilder) build(prdNodes []string) error {
	b.visited = map[string]bool{}
	var roots []string
	for _, id := range prdNodes {
		if len(b.preds[id]) == 0 {
			roots = append(roots, id)
		}
	}
	if len(roots) == 0 {
		return notRepresentable("the graph has no entry node")
	}
	if len(roots) == 1 {
		b.entry = roots[0]
		return b.walk(roots[0])
	}
	forkID := b.uniqueID("fork-entry")
	b.entry = forkID
	joinID, target, err := b.forkJoin("", forkID, roots)
	if err != nil {
		return err
	}
	return b.afterJoin(joinID, target)
}

// afterJoin wires the join's on_complete to target and keeps walking from it.
func (b *graphBuilder) afterJoin(joinID, target string) error {
	if target == "" {
		return nil
	}
	b.setComplete(joinID, target)
	return b.walk(target)
}

// walk emits node id and follows its successors until the chain ends.
func (b *graphBuilder) walk(id string) error {
	for {
		b.visited[id] = true
		b.add(b.prdMember(id))
		next := b.succs[id]
		switch len(next) {
		case 0:
			return nil
		case 1:
			s := next[0]
			if len(b.preds[s]) != 1 {
				return notRepresentable("node %s depends on %v but not on exactly one complete fork set (partial join)", s, b.preds[s])
			}
			b.setComplete(id, s)
			id = s
		default:
			forkID := b.uniqueID("fork-" + id)
			b.setComplete(id, forkID)
			joinID, target, err := b.forkJoin(id, forkID, next)
			if err != nil {
				return err
			}
			return b.afterJoin(joinID, target)
		}
	}
}

// forkJoin emits a fork over branches, the branch members and the paired join.
// pred is the node feeding the fork ("" for an entry fork). It returns the join
// member id and the node the join leads to ("" when the branches end).
func (b *graphBuilder) forkJoin(pred, forkID string, branches []string) (string, string, error) {
	target := ""
	for i, br := range branches {
		if pred != "" && !sameIDs(b.preds[br], []string{pred}) {
			return "", "", notRepresentable("fan-out branch %s also depends on %v (partial join)", br, b.preds[br])
		}
		ss := b.succs[br]
		if len(ss) > 1 {
			return "", "", notRepresentable("fan-out branch %s fans out again (nested fork is not supported)", br)
		}
		if len(ss) == 1 && len(b.preds[ss[0]]) == 1 {
			return "", "", notRepresentable("fan-out branch %s has its own successor %s before the join; branches must be single nodes", br, ss[0])
		}
		t := ""
		if len(ss) == 1 {
			t = ss[0]
		}
		if i == 0 {
			target = t
		} else if t != target {
			return "", "", notRepresentable("fan-out branches do not all feed the same join node (branch %s leads to %q, expected %q); partial join", br, t, target)
		}
	}
	if target != "" && !sameIDs(b.preds[target], branches) {
		return "", "", notRepresentable("node %s depends on %v, which is not exactly the fork set %v (partial join)", target, b.preds[target], branches)
	}
	b.add(AutomationMember{ID: forkID, Kind: MemberKindFork, ForkMembers: append([]string(nil), branches...)})
	for _, br := range branches {
		b.visited[br] = true
		b.add(b.prdMember(br))
	}
	joinID := b.uniqueID("join-" + strings.TrimPrefix(forkID, "fork-"))
	b.add(AutomationMember{ID: joinID, Kind: MemberKindJoin, JoinMembers: append([]string(nil), branches...)})
	return joinID, target, nil
}
