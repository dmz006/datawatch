package server

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// statePath returns the path of a datawatch-owned state file by joining elem
// in order under this server's configured data directory.
//
// The data directory resolves in this order: s.dataDir if non-empty, then
// s.cfg.DataDir, then the default data directory from config.DefaultConfig().
// A leading tilde in the resolved value is expanded with the process home
// directory, because the operator wrote it.
//
// Single-source rule: every datawatch-owned state file is built with this
// helper (or with dataDirPath where no Server is in scope). A new handler
// must not call os.UserHomeDir to build a state path. When a data directory
// is configured, this helper never calls os.UserHomeDir.
func (s *Server) statePath(elem ...string) string {
	dataDir := s.dataDir
	if dataDir == "" && s.cfg != nil {
		dataDir = s.cfg.DataDir
	}
	return dataDirPath(dataDir, elem...)
}

// dataDirPath holds the join and tilde-expansion logic shared by
// Server.statePath, for functions with no Server in scope.
//
// dataDir may be empty, in which case the default data directory from
// config.DefaultConfig() is used. A leading tilde (either a bare "~" or a
// "~/" prefix) is expanded with the process home directory — the operator
// wrote the tilde, so the expansion is theirs to claim. When dataDir is
// non-empty and has no leading tilde, os.UserHomeDir is never called.
//
// Single-source rule: every datawatch-owned state file is built via this
// function or Server.statePath. A new handler must not call os.UserHomeDir
// to build a state path.
func dataDirPath(dataDir string, elem ...string) string {
	if dataDir == "" {
		dataDir = config.DefaultConfig().DataDir
	}
	if dataDir == "~" {
		dataDir = "~/"
	}
	if strings.HasPrefix(dataDir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dataDir = filepath.Join(home, strings.TrimPrefix(dataDir, "~/"))
		}
	}
	parts := make([]string, 0, len(elem)+1)
	parts = append(parts, dataDir)
	parts = append(parts, elem...)
	return filepath.Join(parts...)
}
