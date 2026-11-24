package skills

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// loadProductTerms reads the allowlist file and returns its terms.
func loadProductTerms(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var terms []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		terms = append(terms, line)
	}
	return terms, nil
}

func termWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// productNameFindings removes every listed term (longest first, case-sensitive,
// with boundary checks) and reports each residual product-name occurrence.
func productNameFindings(text string, terms []string) []string {
	sorted := append([]string(nil), terms...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	rs := []rune(text)
	for _, term := range sorted {
		if term == "" {
			continue
		}
		tr := []rune(term)
		dunder := strings.HasSuffix(term, "__")
		var out []rune
		i := 0
		for i < len(rs) {
			if i+len(tr) <= len(rs) && string(rs[i:i+len(tr)]) == term {
				okBefore := i == 0 || !termWordChar(rs[i-1])
				okAfter := true
				if j := i + len(tr); j < len(rs) {
					c := rs[j]
					if dunder {
						okAfter = (c != '_' && c != '-') || !termWordChar(c)
					} else {
						okAfter = !termWordChar(c)
					}
				}
				if okBefore && okAfter {
					out = append(out, ' ')
					i += len(tr)
					continue
				}
			}
			out = append(out, rs[i])
			i++
		}
		rs = out
	}
	residual := string(rs)
	name := "data" + "watch"
	var findings []string
	lower := strings.ToLower(residual)
	for n := strings.Count(lower, name); n > 0; n-- {
		findings = append(findings, fmt.Sprintf("product name not on the allowlist %q", residual))
	}
	return findings
}

func TestProductTermFileLoads(t *testing.T) {
	terms, err := loadProductTerms("product-terms.txt")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(terms) == 0 {
		t.Fatal("no terms")
	}
	name := "data" + "watch"
	for _, term := range terms {
		if term == name {
			return
		}
	}
	t.Fatalf("bare command name %q missing", name)
}

func TestProductTermMatcher(t *testing.T) {
	name := "data" + "watch"
	terms := []string{name, "mcp__" + name + "__"}
	cases := []struct {
		label string
		text  string
		terms []string
		want  int
	}{
		{"listed command", "run " + name + " status now", terms, 0},
		{"listed mcp prefix", "call mcp__" + name + "__list_sessions", terms, 0},
		{"app suffix", name + "-app", terms, 1},
		{"x suffix", name + "x", terms, 1},
		{"new term", name + "-newthing", terms, 1},
		{"capitals", "Data" + "Watch", terms, 1},
	}
	for _, c := range cases {
		got := productNameFindings(c.text, c.terms)
		if len(got) != c.want {
			t.Errorf("%s: got %d findings %v, want %d", c.label, len(got), got, c.want)
		}
		for _, f := range got {
			if !strings.HasPrefix(f, "product name not on the allowlist") {
				t.Errorf("%s: bad finding %q", c.label, f)
			}
		}
	}
}
