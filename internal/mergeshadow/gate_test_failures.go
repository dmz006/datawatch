package mergeshadow

import (
	"bufio"
	"encoding/json"
	"strings"
)

// goTestEvent is one line of `go test -json` output.
type goTestEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

// ParseGoTestFailures reads the JSON output of a go test run and returns the
// names of the failing tests, in the order they failed, and the first
// assertion line. The assertion line is the first output line of a failing
// test that is not go test's own RUN, FAIL or PAUSE bookkeeping. Lines that
// are not JSON are ignored. Both results are empty when nothing failed.
func ParseGoTestFailures(output string) (names []string, firstAssertion string) {
	type key struct{ pkg, test string }
	lines := map[key][]string{}
	failed := map[key]bool{}
	var order []key

	sc := bufio.NewScanner(strings.NewReader(output))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev goTestEvent
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Test == "" {
			continue
		}
		k := key{ev.Package, ev.Test}
		switch ev.Action {
		case "output":
			lines[k] = append(lines[k], ev.Output)
		case "fail":
			if !failed[k] {
				failed[k] = true
				order = append(order, k)
			}
		}
	}
	for _, k := range order {
		names = append(names, k.test)
		if firstAssertion == "" {
			firstAssertion = assertionLine(lines[k])
		}
	}
	return names, firstAssertion
}

// assertionLine returns the first trimmed output line that is an assertion
// message rather than go test bookkeeping.
func assertionLine(out []string) string {
	for _, o := range out {
		s := strings.TrimSpace(o)
		if s == "" || strings.HasPrefix(s, "=== ") || strings.HasPrefix(s, "--- ") {
			continue
		}
		return s
	}
	return ""
}

// failingTestsDetail formats parsed failures for a gate's Detail. It returns
// an empty string when there are no failing tests.
func failingTestsDetail(output string) string {
	names, first := ParseGoTestFailures(output)
	if len(names) == 0 {
		return ""
	}
	d := "failing tests: " + strings.Join(names, ", ")
	if first != "" {
		d += "; first assertion: " + first
	}
	return d
}
