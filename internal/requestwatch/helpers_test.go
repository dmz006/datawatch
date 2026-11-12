package requestwatch

import (
	"encoding/json"
	"strings"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func containsKey(b []byte, key string) bool {
	return strings.Contains(string(b), `"`+key+`":`)
}
