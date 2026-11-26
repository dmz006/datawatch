package acceptance

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	askBullet      = regexp.MustCompile(`^\s*(?:[-*•]|\(?\d{1,3}[.)]|\(?[a-zA-Z][)])\s+`)
	askSentenceEnd = regexp.MustCompile(`[.!?;]+(\s+|$)`)
)

// AskSentences splits an ask into the statements a list has to cover. It errs
// toward too many: an extra open item can be removed with a recorded change,
// while a missed ask would let a request read done.
func AskSentences(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(askBullet.ReplaceAllString(line, ""))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, s := range askSentenceEnd.Split(line, -1) {
			s = strings.TrimSpace(s)
			// A fragment ending in ':' only introduces what follows.
			if strings.HasSuffix(s, ":") || normalizeAsk(s) == "" {
				continue
			}
			if n := normalizeAsk(s); !seen[n] {
				seen[n] = true
				out = append(out, s)
			}
		}
	}
	return out
}

// normalizeAsk keeps letters and digits, lower case, single spaced.
func normalizeAsk(s string) string {
	f := func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), f), " ")
}

// covers reports whether an item's quoted words contain the whole sentence.
// Quoting only part of a sentence does not cover it.
func covers(from, sentence string) bool {
	f, s := normalizeAsk(from), normalizeAsk(sentence)
	return s != "" && strings.Contains(" "+f+" ", " "+s+" ")
}

// askFullText is the ask with every later addition.
func askFullText(a Ask) string {
	parts := []string{a.Text}
	for _, ad := range a.Additions {
		parts = append(parts, ad.Text)
	}
	return strings.Join(parts, "\n")
}

func askHash(a Ask) string {
	texts := []string{a.Text}
	for _, ad := range a.Additions {
		texts = append(texts, ad.Text)
	}
	return hashOf(texts)
}

// askItemID is stable for a sentence, so the same unmapped words keep their id.
func askItemID(sentence string) string { return "ask-" + hashOf(normalizeAsk(sentence))[:10] }

// unmappedAsks returns the ask sentences no item other than the automatic
// ones covers.
func unmappedAsks(a Ask, items []Item) []string {
	var out []string
	for _, s := range AskSentences(askFullText(a)) {
		mapped := false
		for _, it := range items {
			if it.Source != SourceAsk && it.Source != SourceDefault && covers(it.From, s) {
				mapped = true
				break
			}
		}
		if !mapped {
			out = append(out, s)
		}
	}
	return out
}

// syncAskItems makes every unmapped ask sentence an open item. With prune it
// also drops automatic items whose sentence another item now covers; that is
// only done before freeze, while edits are free.
func syncAskItems(l *List, prune bool) {
	if l.Kind == KindDelivery {
		// A message to deliver is not a list of asks.
		return
	}
	want := map[string]string{}
	for _, s := range unmappedAsks(l.Ask, l.Items) {
		want[askItemID(s)] = s
	}
	kept := l.Items[:0:0]
	have := map[string]bool{}
	for _, it := range l.Items {
		if it.Source == SourceAsk {
			base := strings.TrimSuffix(it.ID, "-open")
			if _, ok := want[base]; ok {
				have[base] = true
			} else if prune {
				delete(l.State, it.ID)
				continue
			}
		}
		kept = append(kept, it)
	}
	l.Items = kept
	for _, s := range AskSentences(askFullText(l.Ask)) {
		id := askItemID(s)
		if _, ok := want[id]; ok && !have[id] {
			key := id
			if _, taken := l.item(id); taken != nil {
				// An edited item kept the id but no longer quotes the sentence.
				id = key + "-open"
			}
			l.Items = append(l.Items, Item{ID: id, Statement: s, From: s, Source: SourceAsk, Method: MethodConfirm, Checker: "operator"})
		}
	}
}

// extraAnswerWords returns what an answer says beyond the option it chose, or
// "" when it only names the option.
func extraAnswerWords(option, answer string) string {
	o, a := normalizeAsk(option), normalizeAsk(answer)
	if a == "" || a == o {
		return ""
	}
	trimmed := strings.TrimSpace(answer)
	if opt := strings.TrimSpace(option); opt != "" && len(trimmed) > len(opt) && strings.EqualFold(trimmed[:len(opt)], opt) {
		next := []rune(trimmed[len(opt):])[0]
		if !unicode.IsLetter(next) && !unicode.IsDigit(next) {
			trimmed = strings.TrimLeft(trimmed[len(opt):], " ,.;:)-")
		}
	}
	return trimmed
}
