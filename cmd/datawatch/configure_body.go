package main

import "encoding/json"

// configureRequestBody builds the PUT /api/config body for one `configure
// <key>=<value>` chat command. true/false and values that are a complete JSON
// number go out as JSON literals; everything else, including a date such as
// 2026-01-01 or a path that merely starts with digits, goes out as a JSON string
// with the key and value escaped.
func configureRequestBody(key, value string) string {
	var val any = value
	if value == "true" || value == "false" {
		val = json.RawMessage(value)
	} else {
		var n json.Number
		if json.Unmarshal([]byte(value), &n) == nil {
			val = json.RawMessage(value)
		}
	}
	b, err := json.Marshal(map[string]any{key: val})
	if err != nil {
		return "{}"
	}
	return string(b)
}
