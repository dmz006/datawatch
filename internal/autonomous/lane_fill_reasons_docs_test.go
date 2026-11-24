package autonomous

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// laneFillChipCodes returns the value of every LaneFillReason* constant that can
// appear as a lane fill refusal, read from the source so a new constant cannot
// be added without the docs following. work_closed is not one of them: lane
// fill never lists closed work, so it appears in the refusal of a start and on
// the record, never as a lane fill chip.
func laneFillChipCodes(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "lane_fill_eligibility.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, sp := range gd.Specs {
			vs := sp.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if !strings.HasPrefix(n.Name, "LaneFillReason") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, _ := strconv.Unquote(lit.Value)
				if v != LaneFillReasonWorkClosed {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile("../../" + rel)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// linesFrom returns n lines starting at the first line that contains anchor.
func linesFrom(t *testing.T, text, anchor string, n int) string {
	t.Helper()
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.Contains(l, anchor) {
			end := i + n
			if end > len(lines) {
				end = len(lines)
			}
			return strings.Join(lines[i:end], "\n")
		}
	}
	t.Fatalf("anchor %q not found", anchor)
	return ""
}

func TestLaneFillEligibilityReasonListsMatchTheCode(t *testing.T) {
	codes := laneFillChipCodes(t)
	if len(codes) != 11 {
		t.Fatalf("the code has %d lane fill reasons %v; the docs say eleven. Update the docs and this count together", len(codes), codes)
	}
	surfaces := map[string]string{
		"docs/api/openapi.yaml enum":            linesFrom(t, readRepoFile(t, "docs/api/openapi.yaml"), "enum: [operator_hold,", 2),
		"internal/server/web/openapi.yaml enum": linesFrom(t, readRepoFile(t, "internal/server/web/openapi.yaml"), "enum: [operator_hold,", 2),
		"docs/datawatch-definitions.md":         linesFrom(t, readRepoFile(t, "docs/datawatch-definitions.md"), "**Eligibility reason**", 1),
		"docs/operations.md":                    linesFrom(t, readRepoFile(t, "docs/operations.md"), "**Eligibility reasons.**", 1),
		"docs/api/autonomous.md":                linesFrom(t, readRepoFile(t, "docs/api/autonomous.md"), "`decisions[].eligibility_reason` is the stable code", 1),
		"docs/commands.md":                      linesFrom(t, readRepoFile(t, "docs/commands.md"), "`[held] ...`; the codes are", 4),
		"docs/mcp.md":                           linesFrom(t, readRepoFile(t, "docs/mcp.md"), "**`autonomous_lane_fill_status`**", 1),
		"internal/mcp/lane_fill_tools.go":       linesFrom(t, readRepoFile(t, "internal/mcp/lane_fill_tools.go"), "carries eligibility_reason, one of", 1),
		"docs/howto/automation-lane-fill.md":    readRepoFile(t, "docs/howto/automation-lane-fill.md"),
	}
	for name, text := range surfaces {
		for _, code := range codes {
			if !strings.Contains(text, code) {
				t.Errorf("%s does not list the lane fill reason %q", name, code)
			}
		}
		if strings.Contains(name, "openapi") && strings.Contains(strings.SplitN(text, "\n", 2)[0], "work_closed") {
			t.Errorf("%s lists work_closed, which is never a lane fill chip", name)
		}
	}
	// The how-to has one table row per reason.
	howto := surfaces["docs/howto/automation-lane-fill.md"]
	for _, code := range codes {
		if !strings.Contains(howto, "| `"+code+"` |") {
			t.Errorf("the how-to table has no row for %q", code)
		}
	}
}

func TestLaneFillEligibilityReasonCountIsEleven(t *testing.T) {
	for file, want := range map[string]string{
		"docs/datawatch-definitions.md":      "There are eleven",
		"docs/operations.md":                 "checks eleven rules",
		"docs/howto/automation-lane-fill.md": "against eleven rules",
	} {
		if !strings.Contains(readRepoFile(t, file), want) {
			t.Errorf("%s must say %q", file, want)
		}
	}
	for _, file := range []string{"docs/datawatch-definitions.md", "docs/operations.md", "docs/howto/automation-lane-fill.md"} {
		text := readRepoFile(t, file)
		for _, stale := range []string{"There are ten: `operator_hold`", "checks ten rules", "against ten rules"} {
			if strings.Contains(text, stale) {
				t.Errorf("%s still says %q", file, stale)
			}
		}
	}
}
