// Role tags on llm_add / llm_update. The list is forwarded to REST under the
// same field name; REST normalises and validates it.

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// llmRolesOption declares the roles list argument shared by llm_add and
// llm_update.
func llmRolesOption() mcpsdk.ToolOption {
	return mcpsdk.WithArray("roles",
		mcpsdk.Description("Optional workload roles this LLM serves (list of strings; trimmed, lower-cased, de-duplicated). On llm_update, omit to keep the stored roles; an empty list clears them. An empty role string is rejected."),
		mcpsdk.WithStringItems(),
	)
}

// rolesFromReq returns the roles list to forward to REST and whether the
// caller supplied one. Presence is what matters: an empty list must reach REST
// so it can clear the stored roles.
func rolesFromReq(req mcpsdk.CallToolRequest) ([]string, bool, error) {
	args := req.GetArguments()
	if args == nil {
		return nil, false, nil
	}
	raw, ok := args["roles"]
	if !ok {
		return nil, false, nil
	}
	switch v := raw.(type) {
	case nil:
		return []string{}, true, nil
	case []string:
		return append([]string{}, v...), true, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			str, isStr := e.(string)
			if !isStr {
				return nil, false, fmt.Errorf("roles: expected a list of strings, got element %T", e)
			}
			out = append(out, str)
		}
		return out, true, nil
	case string:
		// Some clients hand an array argument over as its JSON text.
		text := strings.TrimSpace(v)
		if text == "" || text == "null" {
			return []string{}, true, nil
		}
		var list []string
		if err := json.Unmarshal([]byte(text), &list); err != nil {
			return nil, false, fmt.Errorf("roles: expected a list of strings: %v", err)
		}
		return list, true, nil
	default:
		return nil, false, fmt.Errorf("roles: expected a list of strings, got %T", raw)
	}
}

// attachRoles copies the caller's roles list, when one was given, into the LLM
// request body under the REST field name.
func attachRoles(body map[string]any, req mcpsdk.CallToolRequest) error {
	roles, ok, err := rolesFromReq(req)
	if err != nil {
		return err
	}
	if ok {
		body["roles"] = roles
	}
	return nil
}
