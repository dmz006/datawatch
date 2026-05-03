// SAST scanner — static analysis patterns covering Python, Go, JS/TS, Shell.
// Extended from the Python-only patterns in internal/autonomous/security.go.

package scan

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SASTScanner runs regex-based static analysis across source files.
type SASTScanner struct{}

// NewSASTScanner returns a new SAST scanner.
func NewSASTScanner() Scanner { return SASTScanner{} }

func (SASTScanner) Name() string { return "sast" }

var sastPatterns = []struct {
	re       *regexp.Regexp
	msg      string
	severity Severity
	ruleID   string
}{
	// Python — ported from security.go + extended
	{regexp.MustCompile(`\bos\.system\s*\(`), "os.system() — use subprocess with argument list", SeverityError, "SAST001"},
	{regexp.MustCompile(`\bos\.popen\s*\(`), "os.popen() — use subprocess with argument list", SeverityError, "SAST002"},
	{regexp.MustCompile(`subprocess\.\w+\([^)]*shell\s*=\s*True`), "subprocess shell=True — use argument list", SeverityError, "SAST003"},
	{regexp.MustCompile(`\bpickle\.loads?\s*\(`), "pickle.load() — untrusted deserialization", SeverityError, "SAST007"},
	{regexp.MustCompile(`requests\.(?:get|post|put|delete)\s*\(\s*["']https?://\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`), "HTTP to raw IP — possible exfiltration", SeverityWarning, "SAST008"},
	{regexp.MustCompile(`urllib\.request\.urlopen\s*\(\s*["']https?://\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`), "URL request to raw IP — possible exfiltration", SeverityWarning, "SAST009"},
	// eval / exec — cross-language
	{regexp.MustCompile(`\beval\s*\(`), "eval() — potential code injection", SeverityCritical, "SAST004"},
	{regexp.MustCompile(`\bexec\s*\(`), "exec() — potential code injection", SeverityCritical, "SAST005"},
	{regexp.MustCompile(`__import__\s*\(`), "__import__() — suspicious dynamic import", SeverityWarning, "SAST006"},
	// Go
	{regexp.MustCompile(`os\.StartProcess\b`), "os.StartProcess — prefer exec.Command", SeverityWarning, "SAST010"},
	{regexp.MustCompile(`\bunsafe\.Pointer\b`), "unsafe.Pointer — requires explicit justification", SeverityWarning, "SAST011"},
	// JS/TS
	{regexp.MustCompile(`document\.write\s*\(`), "document.write() — XSS risk", SeverityError, "SAST021"},
	{regexp.MustCompile(`\.innerHTML\s*=`), "innerHTML assignment — XSS risk", SeverityWarning, "SAST022"},
	{regexp.MustCompile(`new\s+Function\s*\(`), "new Function() — dynamic code execution", SeverityError, "SAST023"},
	{regexp.MustCompile(`dangerouslySetInnerHTML`), "dangerouslySetInnerHTML — XSS risk", SeverityWarning, "SAST024"},
	// Shell
	{regexp.MustCompile(`\beval\s+"?\$`), "eval with variable — shell injection risk", SeverityCritical, "SAST030"},
	{regexp.MustCompile(`curl\s+[^\n]*\|\s*(?:bash|sh)\b`), "curl|bash pattern — remote code execution risk", SeverityCritical, "SAST031"},
}

var sastExts = map[string]bool{
	".py": true, ".pyw": true,
	".go": true,
	".js": true, ".ts": true, ".jsx": true, ".tsx": true, ".mjs": true, ".cjs": true,
	".sh": true, ".bash": true,
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "venv": true,
	"__pycache__": true, "dist": true, "build": true, ".next": true,
	"vendor": true, "third_party": true, ".mypy_cache": true,
}

func (SASTScanner) Scan(dir string) ([]Finding, error) {
	var findings []Finding
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[filepath.Base(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		if !sastExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		for lineNo, line := range strings.Split(string(data), "\n") {
			for _, p := range sastPatterns {
				if p.re.MatchString(line) {
					findings = append(findings, Finding{
						Scanner:  "sast",
						File:     rel,
						Line:     lineNo + 1,
						Severity: p.severity,
						RuleID:   p.ruleID,
						Message:  p.msg,
					})
				}
			}
		}
		return nil
	})
	return findings, err
}
