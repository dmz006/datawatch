// Secrets scanner — covers hardcoded credentials across common file types.
// Patterns inspired by gitleaks; always-on for .git repos.

package scan

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SecretsScanner detects hardcoded credentials and secret material.
type SecretsScanner struct{}

// NewSecretsScanner returns a new secrets scanner.
func NewSecretsScanner() Scanner { return SecretsScanner{} }

func (SecretsScanner) Name() string { return "secrets" }

var secretPatterns = []struct {
	re     *regexp.Regexp
	msg    string
	ruleID string
}{
	{
		regexp.MustCompile(`(?i)(?:API_KEY|APIKEY|API_SECRET|SECRET_KEY|SECRET|PASSWORD|PASSWD|TOKEN|ACCESS_TOKEN|AUTH_TOKEN)\s*(?:=|:)\s*["'][^"']{8,}["']`),
		"Possible hardcoded credential",
		"SEC001",
	},
	{
		regexp.MustCompile(`(?i)(?:aws_access_key_id|aws_secret_access_key)\s*(?:=|:)\s*["']?[A-Za-z0-9+/]{16,}["']?`),
		"Possible AWS credential",
		"SEC002",
	},
	{
		regexp.MustCompile(`sk-[A-Za-z0-9]{40,}`),
		"Possible OpenAI API key",
		"SEC003",
	},
	{
		regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`),
		"Possible Google API key",
		"SEC004",
	},
	{
		regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36}`),
		"Possible GitHub token",
		"SEC005",
	},
	{
		regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY`),
		"Private key material in source",
		"SEC006",
	},
	{
		regexp.MustCompile(`(?i)(?:database_url|db_url|db_password|postgres_password|mysql_password|mongo_uri)\s*(?:=|:)\s*["'][^"']{4,}["']`),
		"Possible database credential",
		"SEC007",
	},
	{
		regexp.MustCompile(`(?i)(?:slack_token|slack_webhook|slack_signing_secret)\s*(?:=|:)\s*["'][^"']{8,}["']`),
		"Possible Slack credential",
		"SEC008",
	},
}

var secretExts = map[string]bool{
	".go": true, ".py": true,
	".js": true, ".ts": true, ".jsx": true, ".tsx": true, ".mjs": true,
	".yaml": true, ".yml": true,
	".env": true, ".cfg": true, ".conf": true, ".ini": true,
	".json": true, ".toml": true,
	".sh": true, ".bash": true,
	".tf": true, ".hcl": true,
}

var secretSkipFiles = map[string]bool{
	"go.sum":           true,
	"package-lock.json": true,
	"yarn.lock":        true,
	"pnpm-lock.yaml":   true,
}

func (SecretsScanner) Scan(dir string) ([]Finding, error) {
	// Always-on for .git repos — detect secrets before they escape
	var findings []Finding
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// Skip .git internals but note: the scanner itself is triggered
			// by the presence of a .git directory (gitleaks-style).
			if filepath.Base(path) == ".git" && path != dir {
				return filepath.SkipDir
			}
			if skipDirs[filepath.Base(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		base := filepath.Base(path)
		if secretSkipFiles[base] {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		isEnvLike := strings.HasPrefix(base, ".env") || base == "Dockerfile" || base == ".dockerenv"
		if ext != "" && !secretExts[ext] && !isEnvLike {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if len(data) > 1<<20 { // skip files > 1 MB
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		for lineNo, line := range strings.Split(string(data), "\n") {
			for _, p := range secretPatterns {
				if p.re.MatchString(line) {
					findings = append(findings, Finding{
						Scanner:  "secrets",
						File:     rel,
						Line:     lineNo + 1,
						Severity: SeverityCritical,
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
