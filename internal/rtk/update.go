package rtk

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// VersionStatus holds the result of a version check.
type VersionStatus struct {
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version"`
	UpdateAvailable bool      `json:"update_available"`
	AutoUpdatable   bool      `json:"auto_updatable"`
	LastChecked     time.Time `json:"last_checked"`
	DownloadURL     string    `json:"download_url,omitempty"`
	Error           string    `json:"error,omitempty"`
}

var (
	versionStatus   VersionStatus
	versionStatusMu sync.RWMutex
)

// GetVersionStatus returns the cached version status.
func GetVersionStatus() VersionStatus {
	versionStatusMu.RLock()
	defer versionStatusMu.RUnlock()
	return versionStatus
}

// CheckLatestVersion queries GitHub for the latest RTK release and compares
// with the installed version.
func CheckLatestVersion() VersionStatus {
	status := CheckInstalled()
	vs := VersionStatus{
		CurrentVersion: status.Version,
		LastChecked:    time.Now(),
	}

	if !status.Installed {
		vs.Error = "RTK not installed"
		versionStatusMu.Lock()
		versionStatus = vs
		versionStatusMu.Unlock()
		return vs
	}

	// Query GitHub releases API
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/rtk-ai/rtk/releases/latest")
	if err != nil {
		vs.Error = fmt.Sprintf("check failed: %v", err)
		versionStatusMu.Lock()
		versionStatus = vs
		versionStatusMu.Unlock()
		return vs
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		vs.Error = fmt.Sprintf("GitHub API: HTTP %d", resp.StatusCode)
		versionStatusMu.Lock()
		versionStatus = vs
		versionStatusMu.Unlock()
		return vs
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		vs.Error = fmt.Sprintf("parse release: %v", err)
		versionStatusMu.Lock()
		versionStatus = vs
		versionStatusMu.Unlock()
		return vs
	}

	vs.LatestVersion = release.TagName

	// Compare versions (strip "v" prefix and "rtk " prefix)
	current := strings.TrimPrefix(strings.TrimPrefix(vs.CurrentVersion, "rtk "), "v")
	latest := strings.TrimPrefix(vs.LatestVersion, "v")
	vs.UpdateAvailable = latest != current && latest > current

	// Find download URL for this platform
	if vs.UpdateAvailable {
		assetName := fmt.Sprintf("rtk-%s-%s", runtime.GOOS, runtime.GOARCH)
		for _, a := range release.Assets {
			if strings.Contains(a.Name, assetName) {
				vs.DownloadURL = a.BrowserDownloadURL
				break
			}
		}
		// Check if binary path is writable (auto-updatable)
		binaryPath, _ := exec_LookPath(binary)
		if binaryPath != "" && vs.DownloadURL != "" {
			if f, err := os.OpenFile(binaryPath, os.O_WRONLY, 0); err == nil {
				f.Close()
				vs.AutoUpdatable = true
			}
		}
	}

	versionStatusMu.Lock()
	versionStatus = vs
	versionStatusMu.Unlock()
	return vs
}

// exec_LookPath finds the binary path (avoids import cycle with os/exec).
func exec_LookPath(name string) (string, error) {
	if filepath.IsAbs(name) {
		if _, err := os.Stat(name); err == nil {
			return name, nil
		}
		return "", fmt.Errorf("not found: %s", name)
	}
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		full := filepath.Join(dir, name)
		if _, err := os.Stat(full); err == nil {
			return full, nil
		}
	}
	return "", fmt.Errorf("not found in PATH: %s", name)
}

// UpdateBinary downloads the latest RTK release and replaces the current binary.
// Returns the new version string on success.
func UpdateBinary() (string, error) {
	vs := GetVersionStatus()
	if !vs.UpdateAvailable {
		return vs.CurrentVersion, nil
	}
	if vs.DownloadURL == "" {
		return "", fmt.Errorf("no download URL for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	binaryPath, err := exec_LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("find binary: %w", err)
	}

	// Download to temp file
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(vs.DownloadURL)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(binaryPath), "rtk-update-*")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // clean up on failure

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("write: %w", err)
	}
	tmpFile.Close()

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return "", fmt.Errorf("chmod: %w", err)
	}

	// Replace binary
	if err := os.Rename(tmpPath, binaryPath); err != nil {
		return "", fmt.Errorf("replace: %w", err)
	}

	// Verify
	newStatus := CheckInstalled()
	if !newStatus.Installed {
		return "", fmt.Errorf("verification failed after update")
	}

	// Update cached status
	versionStatusMu.Lock()
	versionStatus.CurrentVersion = newStatus.Version
	versionStatus.UpdateAvailable = false
	versionStatus.AutoUpdatable = false
	versionStatusMu.Unlock()

	return newStatus.Version, nil
}

// StartUpdateChecker runs a background goroutine that periodically checks
// for RTK updates. Calls the callback with the version status.
func StartUpdateChecker(interval time.Duration, autoUpdate bool, callback func(VersionStatus)) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	go func() {
		// Check on startup
		vs := CheckLatestVersion()
		if callback != nil {
			callback(vs)
		}
		// Auto-update if enabled and available
		if autoUpdate && vs.UpdateAvailable && vs.AutoUpdatable {
			if newVer, err := UpdateBinary(); err == nil {
				vs.CurrentVersion = newVer
				vs.UpdateAvailable = false
				if callback != nil {
					callback(vs)
				}
			}
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			vs := CheckLatestVersion()
			if callback != nil {
				callback(vs)
			}
			if autoUpdate && vs.UpdateAvailable && vs.AutoUpdatable {
				if newVer, err := UpdateBinary(); err == nil {
					vs.CurrentVersion = newVer
					vs.UpdateAvailable = false
					if callback != nil {
						callback(vs)
					}
				}
			}
		}
	}()
}
