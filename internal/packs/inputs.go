package packs

import "fmt"

// ResolveInputs returns the value of every input the pack declares. For each
// input the first level that defines it wins, in the order Automaton, project,
// global; a level defines an input when its map holds the input's name. When no
// level defines it the declared default is used. A required input that has no
// value at any level and no default is an error. Nil maps are treated as empty.
func ResolveInputs(pack *Pack, automaton, project, global map[string]string) (map[string]string, error) {
	out := map[string]string{}
	if pack == nil {
		return out, nil
	}
	for _, in := range pack.Inputs {
		if v, ok := automaton[in.Name]; ok {
			out[in.Name] = v
			continue
		}
		if v, ok := project[in.Name]; ok {
			out[in.Name] = v
			continue
		}
		if v, ok := global[in.Name]; ok {
			out[in.Name] = v
			continue
		}
		if in.Default != "" || !in.Required {
			out[in.Name] = in.Default
			continue
		}
		return nil, fmt.Errorf("pack input %s is required and has no value; set it at the automaton, project or global level", in.Name)
	}
	return out, nil
}
