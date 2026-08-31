// BL24 — security pattern scanner ported from nightwire/autonomous/
// quality_gates.py. Used by the executor as a pre-commit gate so a
// task that introduces eval()/os.system()/etc. fails verification
// before it lands.

package autonomous

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// dangerousPatterns lists regex → human description for known-bad
// constructs. Ported verbatim from nightwire's _DANGEROUS_PATTERNS;
// extending these is encouraged.
var dangerousPatterns = []struct {
	re  *regexp.Regexp
	msg string
}{
	{regexp.MustCompile(`\bos\.system\s*\(`), "os.system() call — use subprocess with argument list instead"},
	{regexp.MustCompile(`\bos\.popen\s*\(`), "os.popen() call — use subprocess with argument list instead"},
	{regexp.MustCompile(`subprocess\.\w+\([^)]*shell\s*=\s*True`), "subprocess with shell=True — use argument list"},
	{regexp.MustCompile(`\beval\s*\(`), "eval() call — potential code injection"},
	{regexp.MustCompile(`\bexec\s*\(`), "exec() call — potential code injection"},
	{regexp.MustCompile(`__import__\s*\(`), "__import__() call — suspicious dynamic import"},
	{regexp.MustCompile(`(?:API_KEY|SECRET|PASSWORD|TOKEN)\s*=\s*["'][^"']{8,}["']`), "Possible hardcoded secret/API key"},
	{regexp.MustCompile(`requests\.(?:get|post|put|delete)\s*\(\s*["']https?://\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`), "HTTP request to raw IP address — possible exfiltration"},
	{regexp.MustCompile(`urllib\.request\.urlopen\s*\(\s*["']https?://\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`), "URL request to raw IP address — possible exfiltration"},
	{regexp.MustCompile(`\bpickle\.loads?\s*\(`), "pickle.load() — deserializing untrusted data is dangerous"},
}

// scannedExts limits the walker to files whose patterns the regexes
// were designed for. Operators with non-Python projects can pass an
// override via SecurityScan(dir, exts...).
var defaultExts = []string{".py", ".pyw"}

// SecurityScan returns a list of human-readable findings (file:line:
// detail) for project files matching exts. Empty exts uses defaultExts.
func SecurityScan(projectDir string, exts ...string) ([]string, error) {
	if projectDir == "" {
		return nil, nil
	}
	if len(exts) == 0 {
		exts = defaultExts
	}
	var findings []string
	walkErr := filepath.WalkDir(projectDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable, don't abort whole scan
		}
		if d.IsDir() {
			// Skip standard noise dirs.
			base := filepath.Base(path)
			if base == ".git" || base == "node_modules" || base == ".venv" ||
				base == "venv" || base == "__pycache__" || base == "dist" ||
				base == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		matched := false
		for _, want := range exts {
			if ext == want {
				matched = true
				break
			}
		}
		if !matched {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for lineNo, line := range strings.Split(string(data), "\n") {
			for _, p := range dangerousPatterns {
				if p.re.MatchString(line) {
					findings = append(findings, scanFinding(projectDir, path, lineNo+1, p.msg))
				}
			}
		}
		return nil
	})
	return findings, walkErr
}

func scanFinding(root, path string, lineNo int, msg string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return rel + ":" + itoaInt(lineNo) + ": " + msg
}

// injectionPatterns detects known prompt injection phrases in user-supplied text.
var injectionPatterns = []struct {
	re  *regexp.Regexp
	msg string
}{
	{regexp.MustCompile(`(?i)ignore\s+(previous|all|prior)\s+(instructions?|rules?|context)`), "prompt injection: 'ignore previous instructions'"},
	{regexp.MustCompile(`(?i)disregard\s+(all|previous|prior)\s+(instructions?|context|rules?)`), "prompt injection: 'disregard previous instructions'"},
	{regexp.MustCompile(`(?i)forget\s+(?:everything|all)\s+(?:you|I)\s+(?:know|were|have)`), "prompt injection: 'forget everything you know'"},
	{regexp.MustCompile(`(?i)\byou\s+are\s+now\s+(?:a\s+|an\s+)?\w`), "prompt injection: role override ('you are now ...')"},
	{regexp.MustCompile(`(?i)\bact\s+as\s+(?:if\s+you\s+(?:are|were)\s+)?(?:a\s+|an\s+)?(?:different|new\s+)?(?:AI|assistant|model|system)\b`), "prompt injection: 'act as' role override"},
	{regexp.MustCompile(`<\s*\|?\s*(?:im_start|im_end|system|SYS)\s*\|?\s*>`), "prompt injection: chat template boundary (<|im_start|>)"},
	{regexp.MustCompile(`(?i)\[INST\]|\[/INST\]|<<SYS>>|<</SYS>>`), "prompt injection: LLaMA instruction boundary marker"},
	{regexp.MustCompile(`(?i)(?:^|\n)system:\s`), "prompt injection: spurious 'system:' role prefix"},
	{regexp.MustCompile(`(?i)(?:^|\n)assistant:\s`), "prompt injection: spurious 'assistant:' role prefix"},
	{regexp.MustCompile(`(?i)new\s+instructions?:?\s*\n`), "prompt injection: 'new instructions' override block"},
	{regexp.MustCompile(`(?i)override\s+(?:your\s+)?(?:previous\s+)?(?:instructions?|system\s+prompt|constraints?)`), "prompt injection: explicit override attempt"},
}

// ScanForInjection checks a single text string for known prompt injection phrases.
// Returns a (possibly empty) list of human-readable warnings.
func ScanForInjection(text string) []string {
	var findings []string
	for _, p := range injectionPatterns {
		if p.re.MatchString(text) {
			findings = append(findings, p.msg)
		}
	}
	return findings
}

func itoaInt(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
