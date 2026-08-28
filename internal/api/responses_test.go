package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestResponseDTOsPreserveLegacyJSONFields(t *testing.T) {
	testJSONFields(t, MessageResponse{Message: "ok"}, map[string]string{
		"message":  `"ok"`,
		"warnings": "null",
	})
	testJSONFields(t, ApplyResponse{Message: "saved", Status: "ready", Revision: 7}, map[string]string{
		"message":  `"saved"`,
		"status":   `"ready"`,
		"revision": "7",
		"warnings": "null",
	})
}

func TestCommandResponseDistinguishesOmittedAndNullFields(t *testing.T) {
	data, err := json.Marshal(CommandResponse{Command: "links"})
	if err != nil {
		t.Fatalf("marshal command response: %v", err)
	}
	var omitted map[string]json.RawMessage
	if err := json.Unmarshal(data, &omitted); err != nil {
		t.Fatalf("decode command response: %v", err)
	}
	if _, ok := omitted["projects"]; ok {
		t.Fatal("command-specific projects field should be omitted")
	}

	warnings := []string(nil)
	message := "ok"
	data, err = json.Marshal(CommandResponse{Command: "link", Message: &message, Warnings: &warnings})
	if err != nil {
		t.Fatalf("marshal command response with nullable fields: %v", err)
	}
	var nullable map[string]json.RawMessage
	if err := json.Unmarshal(data, &nullable); err != nil {
		t.Fatalf("decode nullable command response: %v", err)
	}
	if string(nullable["warnings"]) != "null" {
		t.Fatalf("warnings = %s, want null", nullable["warnings"])
	}
	if string(nullable["message"]) != `"ok"` {
		t.Fatalf("message = %s, want %q", nullable["message"], "ok")
	}
}

func TestWriteJSONAcceptsOnlyTransportDTOs(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeJSON(recorder, 200, ErrorResponse{Error: "bad request"})
	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Body.String(); got != "{\"error\":\"bad request\"}\n" {
		t.Fatalf("body = %q", got)
	}
}

func testJSONFields(t *testing.T, value any, want map[string]string) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for key, expected := range want {
		if got := string(fields[key]); got != expected {
			t.Errorf("%s = %s, want %s", key, got, expected)
		}
	}
}
