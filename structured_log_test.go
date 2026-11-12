package main

import "testing"

func TestJSONObjectsInTextSurvivesUnmatchedPlainLogBrace(t *testing.T) {
	content := `plain log with stray {
more plain text
{"event":"node_status_log","schema":"bdag.nodeworker.status_log.v1","native_sync":{"status":"synced","current_block":10,"target_block":10},"evm_sync":{"status":"synced"}}`

	objects := jsonObjectsInText(content)

	if len(objects) != 1 {
		t.Fatalf("objects len = %d, want 1: %#v", len(objects), objects)
	}
	if objects[0] == "" || objects[0][0] != '{' {
		t.Fatalf("object = %q, want JSON object", objects[0])
	}
}

func TestJSONObjectsInTextKeepsPrettyPrintedObject(t *testing.T) {
	content := `prefix {
  "event": "pool_status",
  "schema": "bdag.pool.status.v1",
  "pool_status": {
    "status": "degraded"
  }
}
suffix`

	objects := jsonObjectsInText(content)

	if len(objects) != 1 {
		t.Fatalf("objects len = %d, want 1: %#v", len(objects), objects)
	}
	var payload map[string]any
	if err := decodeJSONObject(objects[0], &payload); err != nil {
		t.Fatalf("decode object: %v", err)
	}
	if got := mapString(payload, "event"); got != "pool_status" {
		t.Fatalf("event = %q, want pool_status", got)
	}
}
