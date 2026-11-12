package server

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// parseAllowedProjectRoots reads server.allowed_project_roots from a config
// body: a JSON array of paths, or a comma-separated string (the form CLI,
// chat and MCP send). An empty string or array clears the list. The result is
// validated, so a bad entry refuses the whole value.
func parseAllowedProjectRoots(v interface{}) ([]string, error) {
	var roots []string
	switch val := v.(type) {
	case nil:
	case string:
		roots = splitCSV(val)
	case []interface{}:
		for _, item := range val {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("server.allowed_project_roots: entries must be strings")
			}
			if s = strings.TrimSpace(s); s != "" {
				roots = append(roots, s)
			}
		}
	default:
		return nil, fmt.Errorf("server.allowed_project_roots: must be a list of paths or a comma-separated string")
	}
	if err := config.ValidateAllowedProjectRoots(roots); err != nil {
		return nil, err
	}
	return roots, nil
}
