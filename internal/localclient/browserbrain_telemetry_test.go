package localclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrowserBrainToolForOperation(t *testing.T) {
	cases := map[string]string{
		"run_command":          "terminal",
		"exec_poll":            "terminal",
		"git_status":           "git",
		"write_file":           "edit",
		"read_file":            "read",
		"search_code":          "search",
		"verify_changes":       "verify",
		"context_for_task":     "context",
		"list_workspaces":      "workspace",
		"mcp_call":             "mcp",
		"browser_open":         "browser",
		"computer_click":       "computer",
		"social_read":          "social",
		"blog_publish":         "blog",
	}
	for operation, want := range cases {
		if got := browserBrainToolForOperation(operation); got != want {
			t.Fatalf("operation %s mapped to %q, want %q", operation, got, want)
		}
	}
}

func TestBrowserBrainTelemetrySanitizesSensitiveInput(t *testing.T) {
	e := &Engine{runtimeRedact: []string{"super-secret-value"}}
	value := map[string]any{
		"command":  "echo super-secret-value",
		"password": "plain-password",
		"content":  "hello world",
	}
	got := e.sanitizeBrowserBrainTelemetry(value, 0).(map[string]any)
	if got["password"] != "[redacted]" {
		t.Fatalf("password was not redacted: %#v", got["password"])
	}
	if got["content"] != "[11 bytes]" {
		t.Fatalf("content should be size-only: %#v", got["content"])
	}
	command, _ := got["command"].(string)
	if strings.Contains(command, "super-secret-value") {
		t.Fatalf("runtime secret leaked in command: %q", command)
	}
}

func TestBrowserBrainTelemetryEmitsStartedAndCompleted(t *testing.T) {
	events := make(chan browserBrainTelemetryEvent, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var event browserBrainTelemetryEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("decode event: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		events <- event
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	t.Setenv("CODELOCAL_BROWSERBRAIN_TELEMETRY_URL", server.URL)

	e := &Engine{WorkspaceKey: "workspace-test"}
	span := e.beginBrowserBrainTelemetry("run_command", map[string]any{"command": "echo hello"}, HandleOptions{RequestID: "request-test", SessionID: "session-test"})
	e.finishBrowserBrainTelemetry(span, map[string]any{"stdout": map[string]any{"text": "hello"}, "exitCode": 0}, nil)

	seen := map[string]browserBrainTelemetryEvent{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case event := <-events:
			seen[event.Phase] = event
		case <-deadline:
			t.Fatalf("timed out waiting for telemetry, got phases: %#v", seen)
		}
	}
	if seen["started"].Tool != "terminal" || seen["completed"].Tool != "terminal" {
		t.Fatalf("unexpected tool mapping: %#v", seen)
	}
	if seen["completed"].WorkspaceKey != "workspace-test" {
		t.Fatalf("workspace key missing: %#v", seen["completed"])
	}
}

func TestBrowserBrainTelemetryKeyNormalization(t *testing.T) {
	for _, key := range []string{"oldText", "old_text", "newText", "new_text", "request_payload", "stdin"} {
		if !browserBrainSizeOnlyKey(key) {
			t.Fatalf("expected %q to be size-only", key)
		}
	}
	for _, key := range []string{"command", "detail", "text", "shell_command"} {
		if !browserBrainCommandKey(key) {
			t.Fatalf("expected %q to be command-like", key)
		}
	}
}
