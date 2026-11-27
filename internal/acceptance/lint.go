package acceptance

import (
	"regexp"
	"strings"
)

// Flag is one weakness found in a list by a fixed rule. Flags are shown; they
// block nothing. Serious marks the gaps the open choice about waiting for
// approval would look at.
type Flag struct {
	Code    string `json:"code"`
	Item    string `json:"item,omitempty"`
	Text    string `json:"text"`
	Serious bool   `json:"serious,omitempty"`
}

var vagueOnly = regexp.MustCompile(`(?i)\b(works?|working|improved?|better|as expected|correctly|properly|fine)\b`)

// Flags returns the weaknesses of a list.
func Flags(l *List, checks map[string]ProjectCheck, hasReviewer bool) []Flag {
	var out []Flag
	add := func(code, item string, serious bool, text string) {
		out = append(out, Flag{Code: code, Item: item, Text: text, Serious: serious})
	}
	if len(l.Items) == 0 {
		add("no_items", "", true, "the list has no items, so the request can never read done")
		return out
	}
	if thinList(l) {
		add("only_default_items", "", true, "only default or receipt items on a real ask")
	}
	for _, s := range unmappedAsks(l.textToCover(), l.Items) {
		add("unmapped_ask", askItemID(s), true, "no item covers: "+s)
	}
	if !covered(l) && !allReceipt(l.Items) {
		add("not_reviewed", "", true, "nobody has checked that the list covers the whole ask")
	}
	effectItem := false
	for _, it := range l.Items {
		base, name := it.methodBase()
		if it.Effect != "" && it.Effect != "work" {
			effectItem = true
		}
		switch base {
		case MethodCommand:
			c := it.Check
			ref := name
			if c != nil && c.ProjectCheck != "" {
				ref = c.ProjectCheck
			}
			switch {
			case c != nil && (c.Command != "" || c.Script != ""):
				if len(c.Inputs) == 0 {
					add("no_inputs", it.ID, false, "the check lists no inputs, so a changed helper is not noticed")
				}
				if len(c.Expect) == 0 {
					add("no_must_run", it.ID, true, "the check names nothing that must have run")
				}
			case ref == "":
				add("missing_check", it.ID, true, "the command item names no check")
			default:
				if _, ok := checks[ref]; !ok {
					add("missing_check", it.ID, true, "unknown project check "+ref)
				}
			}
		case MethodState:
			if name == "file" {
				if it.State == nil || it.State.Path == "" {
					add("missing_place", it.ID, true, "the item names no file")
				} else if it.State.Contains == "" {
					add("existence_only", it.ID, false, "the check only tests that a file exists")
				}
				if !readsThePlace(it.Effect) {
					add("claim_only", it.ID, true, "a file the doer can write shows no effect outside the work")
				}
			}
		case MethodReview:
			if !hasReviewer {
				add("no_checker", it.ID, true, "no reviewer is available: this item reads cannot tell")
			}
		case MethodConfirm, MethodReceipt:
		default:
			add("unknown_method", it.ID, true, "datawatch does not know the method "+it.Method)
		}
		if it.Change && base == MethodCommand && it.Before != BeforeFailFirst {
			add("change_without_before", it.ID, true, "a change item needs a check that fails before the work")
		}
		if st := l.State[it.ID]; st != nil && st.Baseline != nil && st.Baseline.Passed {
			add("already_passing", it.ID, true, "the check already passed before the work")
		}
		if st := l.State[it.ID]; st != nil && st.Start != nil && st.Start.True && name != "build_commit" {
			add("already_true", it.ID, it.Change, "already true at the start")
		}
		if reportOnly(it) {
			add("report_only", it.ID, true, "can be satisfied by only reporting a failure; split the item")
		} else if it.Source != SourceDefault && strings.Contains(" "+strings.ToLower(it.Statement)+" ", " or ") {
			add("alternative", it.ID, false, "an \"or\" can weaken the ask")
		}
		if it.Source == SourceInherited {
			add("inherited_draft", it.ID, true, "an inherited \"done when\" is a draft: check it against the whole problem")
		}
		if it.Source != SourceDefault && it.Source != SourceAsk && vagueOnly.MatchString(it.Statement) && len(strings.Fields(it.Statement)) <= 5 {
			add("vague_words", it.ID, false, "vague wording")
		}
	}
	if l.Kind == KindChange && !effectItem {
		add("no_effect_item", "", true, "a change with no item about the place where it has to take effect")
	}
	return out
}
