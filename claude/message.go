package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Message is a native Claude stream envelope. Raw preserves the complete record,
// including unrecognized message types and fields added by future CLI versions.
type Message struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype,omitempty"`
	SessionID       string          `json:"session_id,omitempty"`
	UUID            string          `json:"uuid,omitempty"`
	ParentToolUseID *string         `json:"parent_tool_use_id,omitempty"`
	Message         *APIMessage     `json:"message,omitempty"`
	Event           json.RawMessage `json:"event,omitempty"` // Native partial streaming event.
	Raw             json.RawMessage `json:"-"`
}

type APIMessage struct {
	ID         string          `json:"id,omitempty"`
	Role       string          `json:"role,omitempty"`
	Model      string          `json:"model,omitempty"`
	Content    json.RawMessage `json:"content"` // Text string or an array of ContentBlock.
	Usage      json.RawMessage `json:"usage,omitempty"`
	StopReason *string         `json:"stop_reason,omitempty"`
}

type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// Text returns text content from a user or assistant message; it excludes thinking
// blocks and tool results. Partial token deltas are available in Message.Event.
func (m Message) Text() string {
	if m.Message == nil {
		return ""
	}
	var text string
	if json.Unmarshal(m.Message.Content, &text) == nil {
		return text
	}
	var blocks []ContentBlock
	if json.Unmarshal(m.Message.Content, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

type Usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// Result describes one foreground turn, not completion of background agent work.
type Result struct {
	CredentialExpiresAt string          `json:"credential_expires_at,omitempty"`
	UserMessageUUID     string          `json:"user_message_uuid,omitempty"`
	UserMessageUUIDs    []string        `json:"user_message_uuids,omitempty"`
	Subtype             string          `json:"subtype"`
	SessionID           string          `json:"session_id"`
	IsError             bool            `json:"is_error"`
	Result              string          `json:"result"`
	Errors              []string        `json:"errors,omitempty"`
	NumTurns            int             `json:"num_turns"`
	DurationMS          int64           `json:"duration_ms"`
	DurationAPIMS       int64           `json:"duration_api_ms"`
	TotalCostUSD        float64         `json:"total_cost_usd"`
	Usage               Usage           `json:"usage"`
	ModelUsage          json.RawMessage `json:"modelUsage,omitempty"`
	StructuredOutput    json.RawMessage `json:"structured_output,omitempty"`
	PermissionDenials   json.RawMessage `json:"permission_denials,omitempty"`
	Raw                 json.RawMessage `json:"-"`
}

// Result decodes a result envelope. The result is also returned on agent failure.
func (m Message) Result() (*Result, error) {
	if m.Type != "result" {
		return nil, fmt.Errorf("claude: %q is not a result message", m.Type)
	}
	var r Result
	if err := json.Unmarshal(m.Raw, &r); err != nil {
		return nil, fmt.Errorf("claude: decode result: %w", err)
	}
	r.Raw = bytes.Clone(m.Raw)
	if r.IsError || r.Subtype != "success" {
		return &r, &ResultError{Result: &r}
	}
	return &r, nil
}

// UserMessage accepts either a JSON string or an array of native content blocks
// (including image and tool_result blocks). Use Send for a plain text prompt.
type UserMessage struct {
	Priority        string
	ShouldQuery     *bool
	AgentID         string
	SubagentType    string
	TaskDescription string
	Extra           map[string]json.RawMessage // Future native user-envelope fields, excluding SDK-owned fields.
	Content         json.RawMessage
	SessionID       string
	UUID            string
	ParentToolUseID *string
}

type PermissionRequest struct {
	RequestID               string          `json:"-"` // SDK control envelope ID, including replayed requests.
	ToolName                string          `json:"tool_name"`
	Input                   json.RawMessage `json:"input"`
	ToolUseID               string          `json:"tool_use_id"`
	PermissionSuggestions   json.RawMessage `json:"permission_suggestions,omitempty"`
	BlockedPath             string          `json:"blocked_path,omitempty"`
	DecisionReason          string          `json:"decision_reason,omitempty"`
	AgentID                 string          `json:"agent_id,omitempty"`
	Title                   string          `json:"title,omitempty"`
	DisplayName             string          `json:"display_name,omitempty"`
	Description             string          `json:"description,omitempty"`
	MCPServer               json.RawMessage `json:"mcp_server,omitempty"`
	DecisionReasonType      string          `json:"decision_reason_type,omitempty"`
	ClassifierApprovable    *bool           `json:"classifier_approvable,omitempty"`
	SuppressAlwaysAllowRule bool            `json:"suppress_always_allow_rule,omitempty"`
	DefaultToNo             bool            `json:"default_to_no,omitempty"`
	MatchedAskRule          json.RawMessage `json:"matched_ask_rule,omitempty"`
	RequiresUserInteraction bool            `json:"requires_user_interaction,omitempty"`
	Raw                     json.RawMessage `json:"-"`
}

type PermissionDecision struct {
	Behavior           string          `json:"behavior"` // "allow" or "deny"; all other values are rejected.
	UpdatedInput       json.RawMessage `json:"updatedInput,omitempty"`
	UpdatedPermissions json.RawMessage `json:"updatedPermissions,omitempty"`
	Message            string          `json:"message,omitempty"`
	Interrupt          bool            `json:"interrupt,omitempty"`
}
