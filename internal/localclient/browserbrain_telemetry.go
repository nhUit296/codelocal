package localclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/0xmarkhydra/codelocal/internal/security"
)

const defaultBrowserBrainTelemetryURL = "http://127.0.0.1:8787/api/telemetry/tool"

var browserBrainTelemetrySeq atomic.Uint64

type browserBrainTelemetryEvent struct {
	InvocationID string `json:"invocation_id"`
	Tool         string `json:"tool"`
	Operation    string `json:"operation,omitempty"`
	Phase        string `json:"phase"`
	Timestamp    string `json:"timestamp"`
	StartedAt    string `json:"started_at,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`
	WorkspaceKey string `json:"workspace_key,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
	Input        any    `json:"input,omitempty"`
	Output       any    `json:"output,omitempty"`
	Error        string `json:"error,omitempty"`
}

type browserBrainTelemetrySpan struct {
	InvocationID string
	Tool         string
	Operation    string
	StartedAt    time.Time
	WorkspaceKey string
	SessionID    string
	RequestID    string
	Input        any
}

func (e *Engine) beginBrowserBrainTelemetry(operation string, args map[string]any, opts HandleOptions) browserBrainTelemetrySpan {
	tool := browserBrainToolForOperation(operation)
	if tool == "" {
		return browserBrainTelemetrySpan{}
	}
	started := time.Now()
	invocationID := strings.TrimSpace(opts.RequestID)
	if invocationID == "" {
		invocationID = fmt.Sprintf("cl_%d_%d", started.UnixNano(), browserBrainTelemetrySeq.Add(1))
	}
	span := browserBrainTelemetrySpan{
		InvocationID: invocationID,
		Tool:         tool,
		Operation:    strings.TrimSpace(operation),
		StartedAt:    started,
		WorkspaceKey: e.WorkspaceKey,
		SessionID:    strings.TrimSpace(opts.SessionID),
		RequestID:    strings.TrimSpace(opts.RequestID),
		Input:        e.sanitizeBrowserBrainTelemetry(args, 0),
	}
	emitBrowserBrainTelemetry(browserBrainTelemetryEvent{
		InvocationID: span.InvocationID,
		Tool:         span.Tool,
		Operation:    span.Operation,
		Phase:        "started",
		Timestamp:    started.UTC().Format(time.RFC3339Nano),
		StartedAt:    started.UTC().Format(time.RFC3339Nano),
		WorkspaceKey: span.WorkspaceKey,
		SessionID:    span.SessionID,
		RequestID:    span.RequestID,
		Input:        span.Input,
	})
	return span
}

func (e *Engine) finishBrowserBrainTelemetry(span browserBrainTelemetrySpan, result any, callErr error) {
	if span.InvocationID == "" || span.Tool == "" {
		return
	}
	finished := time.Now()
	phase := "completed"
	errText := ""
	if callErr != nil {
		phase = "error"
		errText = e.sanitizeBrowserBrainText(callErr.Error())
	}
	emitBrowserBrainTelemetry(browserBrainTelemetryEvent{
		InvocationID: span.InvocationID,
		Tool:         span.Tool,
		Operation:    span.Operation,
		Phase:        phase,
		Timestamp:    finished.UTC().Format(time.RFC3339Nano),
		StartedAt:    span.StartedAt.UTC().Format(time.RFC3339Nano),
		DurationMS:   finished.Sub(span.StartedAt).Milliseconds(),
		WorkspaceKey: span.WorkspaceKey,
		SessionID:    span.SessionID,
		RequestID:    span.RequestID,
		Input:        span.Input,
		Output:       e.sanitizeBrowserBrainTelemetry(result, 0),
		Error:        errText,
	})
}

func emitBrowserBrainTelemetry(event browserBrainTelemetryEvent) {
	endpoint := strings.TrimSpace(os.Getenv("CODELOCAL_BROWSERBRAIN_TELEMETRY_URL"))
	if strings.EqualFold(endpoint, "off") || strings.EqualFold(endpoint, "disabled") {
		return
	}
	if endpoint == "" {
		endpoint = defaultBrowserBrainTelemetryURL
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	go func() {
		client := &http.Client{Timeout: 750 * time.Millisecond}
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()
}

func browserBrainToolForOperation(operation string) string {
	op := strings.ToLower(strings.TrimSpace(operation))
	if op == "" {
		return ""
	}
	switch {
	case op == "run_command", op == "exec_start", op == "pty_start", op == "terminal_preflight", op == "terminal_history", op == "process_list", op == "artifact_publish", strings.HasPrefix(op, "exec_"), strings.HasPrefix(op, "pty_"):
		return "terminal"
	case strings.HasPrefix(op, "git_"):
		return "git"
	case op == "write_file", op == "edit_file", op == "apply_patch", op == "apply_edits", op == "format_changed_files":
		return "edit"
	case op == "file_info", op == "read_file", op == "read_file_range", op == "read_files":
		return "read"
	case op == "list_files", op == "search_code":
		return "search"
	case op == "snapshot_diagnostics", op == "verify_changes":
		return "verify"
	case strings.HasPrefix(op, "mcp_"):
		return "mcp"
	case strings.HasPrefix(op, "browser_"):
		return "browser"
	case strings.HasPrefix(op, "computer_"):
		return "computer"
	case strings.HasPrefix(op, "social_"):
		return "social"
	case strings.HasPrefix(op, "blog_"):
		return "blog"
	case op == "context_for_task", op == "project_info", op == "project_map", op == "read_instructions", op == "inspect_dependency", op == "read_dependency", op == "search_dependency", op == "semantic_info", op == "workspace_symbols", op == "document_symbols", op == "find_definition", op == "find_references", op == "find_implementations", op == "get_hover", op == "get_diagnostics", op == "get_callers", op == "get_callees", op == "get_import_graph":
		return "context"
	case op == "list_workspaces", op == "select_workspace", op == "workspace_info", op == "approval_mode", op == "memory_remember", op == "memory_recall", op == "learned_skill_list", op == "list_devices", op == "list_device_identities", op == "rename_device", op == "revoke_device", strings.HasPrefix(op, "approval_"), strings.HasPrefix(op, "sandbox_"):
		return "workspace"
	case strings.HasPrefix(op, "agent_") || strings.HasPrefix(op, "bounded_agent"):
		return "agent"
	default:
		return ""
	}
}

func (e *Engine) sanitizeBrowserBrainTelemetry(value any, depth int) any {
	if depth > 6 {
		return "[truncated]"
	}
	switch v := value.(type) {
	case nil, bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return v
	case string:
		text := e.sanitizeBrowserBrainText(v)
		if len(text) > 32768 {
			return text[:32768] + "…[truncated]"
		}
		return text
	case map[string]any:
		out := make(map[string]any, min(len(v), 100))
		count := 0
		for key, item := range v {
			if count >= 100 {
				out["_truncated"] = true
				break
			}
			if browserBrainSensitiveKey(key) {
				out[key] = "[redacted]"
			} else if text, ok := item.(string); ok && browserBrainSizeOnlyKey(key) {
				out[key] = fmt.Sprintf("[%d bytes]", len([]byte(text)))
			} else if text, ok := item.(string); ok && browserBrainCommandKey(key) {
				out[key] = e.sanitizeBrowserBrainText(security.RedactCommand(text))
			} else {
				out[key] = e.sanitizeBrowserBrainTelemetry(item, depth+1)
			}
			count++
		}
		return out
	case []any:
		limit := len(v)
		if limit > 100 {
			limit = 100
		}
		out := make([]any, 0, limit+1)
		for i := 0; i < limit; i++ {
			out = append(out, e.sanitizeBrowserBrainTelemetry(v[i], depth+1))
		}
		if len(v) > limit {
			out = append(out, "[truncated]")
		}
		return out
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return e.sanitizeBrowserBrainText(fmt.Sprint(v))
		}
		var decoded any
		if json.Unmarshal(raw, &decoded) == nil {
			return e.sanitizeBrowserBrainTelemetry(decoded, depth+1)
		}
		return e.sanitizeBrowserBrainText(fmt.Sprint(v))
	}
}

func (e *Engine) sanitizeBrowserBrainText(value string) string {
	e.mu.Lock()
	redact := append([]string(nil), e.runtimeRedact...)
	e.mu.Unlock()
	out := value
	for _, secret := range redact {
		if strings.TrimSpace(secret) != "" {
			out = strings.ReplaceAll(out, secret, "[redacted]")
		}
	}
	return out
}

func browserBrainSensitiveKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"), " ", "_"))
	for _, marker := range []string{"password", "passwd", "secret", "token", "authorization", "cookie", "credential", "private_key", "apikey", "api_key", "access_key", "refresh_token"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func browserBrainSizeOnlyKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "content", "patch", "input", "oldtext", "newtext":
		return true
	default:
		return false
	}
}

func browserBrainCommandKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "command", "query", "detail":
		return true
	default:
		return false
	}
}
