package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type HookEvent string

const (
	HookPreToolUse          HookEvent = "PreToolUse"
	HookPostToolUse         HookEvent = "PostToolUse"
	HookPostToolUseFailure  HookEvent = "PostToolUseFailure"
	HookPostToolBatch       HookEvent = "PostToolBatch"
	HookNotification        HookEvent = "Notification"
	HookUserPromptSubmit    HookEvent = "UserPromptSubmit"
	HookUserPromptExpansion HookEvent = "UserPromptExpansion"
	HookSessionStart        HookEvent = "SessionStart"
	HookSessionEnd          HookEvent = "SessionEnd"
	HookStop                HookEvent = "Stop"
	HookStopFailure         HookEvent = "StopFailure"
	HookSubagentStart       HookEvent = "SubagentStart"
	HookSubagentStop        HookEvent = "SubagentStop"
	HookPreCompact          HookEvent = "PreCompact"
	HookPostCompact         HookEvent = "PostCompact"
	HookPreModelSwitch      HookEvent = "PreModelSwitch"
	HookPostModelSwitch     HookEvent = "PostModelSwitch"
	HookPermissionRequest   HookEvent = "PermissionRequest"
	HookPermissionDenied    HookEvent = "PermissionDenied"
	HookSetup               HookEvent = "Setup"
	HookTeammateIdle        HookEvent = "TeammateIdle"
	HookTaskCreated         HookEvent = "TaskCreated"
	HookTaskCompleted       HookEvent = "TaskCompleted"
	HookElicitation         HookEvent = "Elicitation"
	HookElicitationResult   HookEvent = "ElicitationResult"
	HookConfigChange        HookEvent = "ConfigChange"
	HookWorktreeCreate      HookEvent = "WorktreeCreate"
	HookWorktreeRemove      HookEvent = "WorktreeRemove"
	HookInstructionsLoaded  HookEvent = "InstructionsLoaded"
	HookCwdChanged          HookEvent = "CwdChanged"
	HookFileChanged         HookEvent = "FileChanged"
	HookDirectoryAdded      HookEvent = "DirectoryAdded"
	HookMessageDisplay      HookEvent = "MessageDisplay"
)

// HookInput keeps common fields typed and preserves every event-specific field.
type HookInput struct {
	HookEventName  HookEvent       `json:"hook_event_name"`
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	PermissionMode string          `json:"permission_mode"`
	ToolName       string          `json:"tool_name,omitempty"`
	ToolInput      json.RawMessage `json:"tool_input,omitempty"`
	Raw            json.RawMessage `json:"-"`
}

// HookOutput uses native camelCase output keys, including hookSpecificOutput.
// A nil result is an empty successful response. Async output is also supported.
type HookOutput map[string]any
type HookCallback func(context.Context, HookInput, string) (HookOutput, error)

type HookMatcher struct {
	Matcher   *string
	Hooks     []HookCallback
	Timeout   time.Duration
	OnFailure string // "continue", "block", or omitted. Block requires server support.
}

func validateHooks(hooks map[HookEvent][]HookMatcher) error {
	for event, matchers := range hooks {
		if event == "" {
			return fmt.Errorf("claude: hook event cannot be empty")
		}
		for _, matcher := range matchers {
			if matcher.Timeout < 0 {
				return fmt.Errorf("claude: negative hook timeout")
			}
			if matcher.OnFailure != "" && matcher.OnFailure != "continue" && matcher.OnFailure != "block" {
				return fmt.Errorf("claude: invalid hook failure policy")
			}
			for _, callback := range matcher.Hooks {
				if callback == nil {
					return fmt.Errorf("claude: hook callback cannot be nil")
				}
			}
		}
	}
	return nil
}

type hookRegistration struct {
	callback HookCallback
	timeout  time.Duration
}

func (c *Client) initializeRequest() map[string]any {
	request := map[string]any{"subtype": "initialize", "hooks": nil}
	if c.opts.SystemPromptFile == "" {
		if c.opts.SystemPrompt != nil {
			request["systemPrompt"] = []string{*c.opts.SystemPrompt}
		} else if !c.opts.UseClaudeCodePrompt {
			request["systemPrompt"] = []string{""}
		}
	}
	if c.opts.AppendSystemPrompt != "" {
		request["appendSystemPrompt"] = c.opts.AppendSystemPrompt
	}
	config := map[string]any{}
	events := make([]string, 0, len(c.opts.Hooks))
	for event := range c.opts.Hooks {
		events = append(events, string(event))
	}
	sort.Strings(events)
	c.hooks = map[string]hookRegistration{}
	for _, event := range events {
		var matchers []any
		for _, matcher := range c.opts.Hooks[HookEvent(event)] {
			ids := make([]string, 0, len(matcher.Hooks))
			for _, callback := range matcher.Hooks {
				id := fmt.Sprintf("hook_%d", len(c.hooks))
				c.hooks[id] = hookRegistration{callback, matcher.Timeout}
				ids = append(ids, id)
			}
			item := map[string]any{"matcher": matcher.Matcher, "hookCallbackIds": ids}
			if matcher.Timeout > 0 {
				item["timeout"] = matcher.Timeout.Seconds()
			}
			if matcher.OnFailure != "" {
				item["onFailure"] = matcher.OnFailure
			}
			matchers = append(matchers, item)
		}
		if len(matchers) > 0 {
			config[event] = matchers
		}
	}
	if len(config) > 0 {
		request["hooks"] = config
	}
	if c.opts.Agents != nil {
		request["agents"] = c.opts.Agents
	}
	if c.opts.Skills != nil {
		request["skills"] = c.opts.Skills
	}
	if c.opts.ForwardSubagentText {
		request["forwardSubagentText"] = true
	}
	if len(c.opts.SDKServers) > 0 {
		names := make([]string, 0, len(c.opts.SDKServers))
		for name := range c.opts.SDKServers {
			names = append(names, name)
		}
		sort.Strings(names)
		request["sdkMcpServers"] = names

	}
	for key, value := range c.opts.InitializeOptions {
		if _, owned := request[key]; !owned {
			request[key] = value
		}
	}
	return request
}

func (c *Client) checkInitialization(data json.RawMessage) error {
	blocks := false
	for _, matchers := range c.opts.Hooks {
		for _, matcher := range matchers {
			if matcher.OnFailure == "block" {
				blocks = true
			}
		}
	}
	if !blocks {
		return nil
	}
	var result struct {
		Supported bool  `json:"hook_on_failure_supported"`
		Applied   *bool `json:"hooks_applied"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	if !result.Supported || result.Applied != nil && !*result.Applied {
		return fmt.Errorf("claude: server cannot enforce blocking hooks; update CLI or start a new session")
	}
	return nil
}

func (c *Client) serverControl(ctx context.Context, raw json.RawMessage) (response any, err error) {
	defer func() {
		if recover() != nil {
			response = nil
			err = fmt.Errorf("claude: host control handler panicked")
		}
	}()
	var request struct {
		Subtype    string          `json:"subtype"`
		CallbackID string          `json:"callback_id"`
		Input      json.RawMessage `json:"input"`
		ToolUseID  string          `json:"tool_use_id"`
		ServerName string          `json:"server_name"`
		Message    json.RawMessage `json:"message"`
	}
	if err = json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	switch request.Subtype {
	case "can_use_tool":
		return c.permission(ctx, raw)
	case "hook_callback":
		hook, ok := c.hooks[request.CallbackID]
		if !ok {
			return nil, fmt.Errorf("claude: unknown hook callback")
		}
		if hook.timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, hook.timeout)
			defer cancel()
		}
		var input HookInput
		if err = json.Unmarshal(request.Input, &input); err != nil {
			return nil, err
		}
		input.Raw = request.Input
		output, err := hook.callback(ctx, input, request.ToolUseID)
		if output == nil {
			output = HookOutput{}
		}
		return output, err
	case "mcp_message":
		response, err := c.mcpMessage(ctx, request.ServerName, request.Message)
		if err != nil {
			return nil, err
		}
		return map[string]any{"mcp_response": response}, nil
	}
	return nil, fmt.Errorf("claude: unsupported server control %q", request.Subtype)
}
