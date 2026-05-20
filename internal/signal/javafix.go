// Package signal — Java/JRE compatibility helper.
//
// signal-cli 0.14+ ships compiled with Java 25 (class file 69). Many
// systems still default to Java 21 (class file 65) on PATH (notably any
// host running SDKMAN with `current` set to Java 21). When that happens
// every signal-cli invocation fails with `UnsupportedClassVersionError`.
//
// EnsureCompatibleJava runs once at process startup. If `signal-cli` does
// not run under the default PATH, it scans common JRE install locations
// for a compatible JRE and rewrites the process-wide JAVA_HOME and PATH
// so every subsequent signal-cli child inherits a working Java.
//
// Operator-filed 2026-05-09: setup-signal failed silently with the cryptic
// LinkageError trace. Pre-v7.0 fix.
package signal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var (
	javaFixOnce sync.Once
	javaFixErr  error
	javaFixHome string
)

// EnsureCompatibleJava is idempotent. It returns the JAVA_HOME path that
// was injected ("" when the default Java was already fine), and any
// preflight error (when no compatible JRE could be found).
func EnsureCompatibleJava() (string, error) {
	javaFixOnce.Do(func() {
		javaFixHome, javaFixErr = ensureCompatibleJavaOnce()
		if javaFixHome != "" {
			_ = os.Setenv("JAVA_HOME", javaFixHome)
			_ = os.Setenv("PATH", filepath.Join(javaFixHome, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
	})
	return javaFixHome, javaFixErr
}

func ensureCompatibleJavaOnce() (string, error) {
	out, runErr := exec.Command("signal-cli", "--version").CombinedOutput()
	if runErr == nil {
		return "", nil
	}
	s := string(out)
	if !strings.Contains(s, "UnsupportedClassVersionError") {
		// Some other failure (signal-cli not installed, perms). Don't
		// turn this into a hard error here — leave it to whichever
		// caller actually tries to use signal-cli.
		return "", nil
	}
	needMajor := parseRequiredJavaMajor(s)
	if needMajor == 0 {
		needMajor = 25
	}
	if jh := findJREForMajor(needMajor); jh != "" {
		return jh, nil
	}
	jver := "unknown"
	if jout, jerr := exec.Command("java", "-version").CombinedOutput(); jerr == nil {
		lines := strings.SplitN(string(jout), "\n", 2)
		if len(lines) > 0 {
			jver = strings.TrimSpace(lines[0])
		}
	}
	return "", fmt.Errorf("signal-cli requires Java %d but no compatible JRE was found\n"+
		"  Default `java` on PATH is: %s\n\n"+
		"Fix one of:\n"+
		"  1) Install Java %d:\n"+
		"       apt:    sudo apt install openjdk-%d-jre-headless\n"+
		"       brew:   brew install openjdk@%d\n"+
		"       sdkman: sdk install java %d-tem  &&  sdk use java %d-tem\n"+
		"  2) Downgrade signal-cli to a Java-21-compatible release (0.13.x):\n"+
		"       https://github.com/AsamK/signal-cli/releases/tag/v0.13.4",
		needMajor, jver, needMajor, needMajor, needMajor, needMajor, needMajor)
}

// parseRequiredJavaMajor extracts the Java major version implied by an
// `UnsupportedClassVersionError ... class file version 69.0` message
// (class file = Java major + 44).
func parseRequiredJavaMajor(s string) int {
	i := strings.Index(s, "class file version ")
	if i < 0 {
		return 0
	}
	rest := s[i+len("class file version "):]
	j := strings.IndexAny(rest, " .\n")
	if j < 0 {
		return 0
	}
	cf, err := strconv.Atoi(rest[:j])
	if err != nil || cf <= 44 {
		return 0
	}
	return cf - 44
}

// findJREForMajor scans common install locations for a JRE whose major
// version >= want. Returns the JAVA_HOME path or "".
func findJREForMajor(want int) string {
	roots := []string{"/usr/lib/jvm", "/opt/java", "/opt", "/Library/Java/JavaVirtualMachines"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".sdkman/candidates/java"))
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			cand := filepath.Join(root, e.Name())
			javaBin := filepath.Join(cand, "bin", "java")
			if _, statErr := os.Stat(javaBin); statErr != nil {
				macBin := filepath.Join(cand, "Contents", "Home", "bin", "java")
				if _, statErr2 := os.Stat(macBin); statErr2 != nil {
					continue
				}
				cand = filepath.Join(cand, "Contents", "Home")
				javaBin = macBin
			}
			if jreMajor(javaBin) >= want {
				return cand
			}
		}
	}
	return ""
}

func jreMajor(javaBin string) int {
	out, err := exec.Command(javaBin, "-version").CombinedOutput()
	if err != nil {
		return 0
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	q1 := strings.Index(line, "\"")
	q2 := strings.LastIndex(line, "\"")
	if q1 < 0 || q2 <= q1 {
		return 0
	}
	ver := line[q1+1 : q2]
	if dot := strings.Index(ver, "."); dot > 0 {
		ver = ver[:dot]
	}
	n, _ := strconv.Atoi(ver)
	return n
}
