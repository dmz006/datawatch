package inference

// PoolNode is one compute node of an LLM entry's pool with the scheduling
// priority taken from its compute node record.
type PoolNode struct {
	Name     string
	Priority int
}

// OrderedPoolNodes returns the nodes of llm's compute_nodes pool in list
// order, each with the priority priority reports for it. A node the lookup
// does not know (second result false), an empty name and a repeated name are
// dropped. A nil lookup knows no node, so the result is empty.
func OrderedPoolNodes(llm LLM, priority func(node string) (int, bool)) []PoolNode {
	if priority == nil {
		return nil
	}
	var out []PoolNode
	seen := make(map[string]bool, len(llm.ComputeNodes))
	for _, name := range llm.ComputeNodes {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		p, ok := priority(name)
		if !ok {
			continue
		}
		out = append(out, PoolNode{Name: name, Priority: p})
	}
	return out
}
