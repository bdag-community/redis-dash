package main

import (
	"encoding/json"
	"strings"
)

func decodeJSONObject(raw string, target any) error {
	return json.Unmarshal([]byte(raw), target)
}

func jsonObjectsInText(content string) []string {
	objects := []string{}
	for i := 0; i < len(content); i++ {
		if content[i] != '{' {
			continue
		}
		var raw json.RawMessage
		decoder := json.NewDecoder(strings.NewReader(content[i:]))
		if err := decoder.Decode(&raw); err != nil || len(raw) == 0 || raw[0] != '{' {
			continue
		}
		objects = append(objects, string(raw))
		if len(raw) > 1 {
			i += len(raw) - 1
		}
	}
	return objects
}
