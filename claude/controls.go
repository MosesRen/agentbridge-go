package claude

import (
	"context"
	"encoding/json"
	"fmt"
)

type SlashCommand struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ArgumentHint string   `json:"argumentHint"`
	Aliases      []string `json:"aliases,omitempty"`
	Builtin      bool     `json:"builtin,omitempty"`
}
type ModelInfo struct {
	Value                    string   `json:"value"`
	ResolvedModel            string   `json:"resolvedModel,omitempty"`
	DisplayName              string   `json:"displayName"`
	Description              string   `json:"description"`
	SupportsEffort           bool     `json:"supportsEffort,omitempty"`
	SupportedEffortLevels    []string `json:"supportedEffortLevels,omitempty"`
	SupportsAdaptiveThinking bool     `json:"supportsAdaptiveThinking,omitempty"`
	SupportsFastMode         bool     `json:"supportsFastMode,omitempty"`
	SupportsAutoMode         bool     `json:"supportsAutoMode,omitempty"`
}
type AgentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Model       string `json:"model,omitempty"`
}
type AccountInfo struct {
	Email            string `json:"email,omitempty"`
	Organization     string `json:"organization,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
	TokenSource      string `json:"tokenSource,omitempty"`
	APIKeySource     string `json:"apiKeySource,omitempty"`
	APIProvider      string `json:"apiProvider,omitempty"`
}
type InitializationResult struct {
	Commands               []SlashCommand  `json:"commands"`
	Models                 []ModelInfo     `json:"models"`
	Agents                 []AgentInfo     `json:"agents"`
	Account                AccountInfo     `json:"account"`
	OutputStyle            string          `json:"output_style"`
	AvailableOutputStyles  []string        `json:"available_output_styles"`
	ClaudeCodeVersion      string          `json:"claude_code_version,omitempty"`
	HooksApplied           *bool           `json:"hooks_applied,omitempty"`
	HookOnFailureSupported bool            `json:"hook_on_failure_supported,omitempty"`
	Raw                    json.RawMessage `json:"-"`
}

func (c *Client) InitializationResult() (*InitializationResult, error) {
	data := c.Initialization()
	var r InitializationResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	r.Raw = data
	return &r, nil
}
func (c *Client) SupportedCommands() ([]SlashCommand, error) {
	r, e := c.InitializationResult()
	if e != nil {
		return nil, e
	}
	return r.Commands, nil
}
func (c *Client) SupportedModels() ([]ModelInfo, error) {
	r, e := c.InitializationResult()
	if e != nil {
		return nil, e
	}
	return r.Models, nil
}
func (c *Client) SupportedAgents() ([]AgentInfo, error) {
	r, e := c.InitializationResult()
	if e != nil {
		return nil, e
	}
	return r.Agents, nil
}
func (c *Client) AccountInfo() (AccountInfo, error) {
	r, e := c.InitializationResult()
	if e != nil {
		return AccountInfo{}, e
	}
	return r.Account, nil
}
func (c *Client) Reinitialize(ctx context.Context) (*InitializationResult, error) {
	data, err := c.request(ctx, c.initRequest, c.opts.StartupTimeout)
	if err != nil {
		return nil, err
	}
	if err = c.checkInitialization(data); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.initialization = data
	c.mu.Unlock()
	return c.InitializationResult()
}

type InterruptOptions struct{ CancelQueued bool }
type InterruptReceipt struct {
	StillQueued []string        `json:"still_queued"`
	Cancelled   []string        `json:"cancelled,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

func (c *Client) InterruptWithOptions(ctx context.Context, o InterruptOptions) (*InterruptReceipt, error) {
	data, err := c.Request(ctx, map[string]any{"subtype": "interrupt", "cancel_queued": o.CancelQueued})
	if err != nil {
		return nil, err
	}
	r := &InterruptReceipt{Raw: data}
	if len(data) > 0 {
		err = json.Unmarshal(data, r)
	}
	return r, err
}
func (c *Client) ResetModel(ctx context.Context) error {
	_, err := c.Request(ctx, map[string]any{"subtype": "set_model", "model": nil})
	return err
}

// SetMCPPermissionModeOverride mirrors the upstream tighten-only server override.
// nil clears it; the CLI enforces that the override cannot widen privileges.
func (c *Client) SetMCPPermissionModeOverride(ctx context.Context, name string, mode *string) (json.RawMessage, error) {
	if mode != nil && *mode != "default" && *mode != "auto" {
		return nil, fmt.Errorf("claude: invalid MCP permission override")
	}
	return c.control(ctx, "set_mcp_permission_mode_override", map[string]any{"serverName": name, "mode": mode})
}
func (c *Client) control(ctx context.Context, subtype string, fields map[string]any) (json.RawMessage, error) {
	request := map[string]any{"subtype": subtype}
	for k, v := range fields {
		request[k] = v
	}
	return c.Request(ctx, request)
}
func (c *Client) SetMaxThinkingTokens(ctx context.Context, tokens *int, display *string) error {
	fields := map[string]any{"max_thinking_tokens": tokens}
	if display != nil {
		fields["thinking_display"] = *display
	}
	_, err := c.control(ctx, "set_max_thinking_tokens", fields)
	return err
}
func (c *Client) ApplyFlagSettings(ctx context.Context, settings map[string]any) error {
	_, err := c.control(ctx, "apply_flag_settings", map[string]any{"settings": settings})
	return err
}

// UpdateSettings explicitly writes a CLI settings file. ApplyFlagSettings only modifies the session.
func (c *Client) UpdateSettings(ctx context.Context, source string, settings map[string]any) error {
	if source != "localSettings" && source != "userSettings" {
		return fmt.Errorf("claude: invalid settings source")
	}
	_, err := c.control(ctx, "update_settings", map[string]any{"source": source, "settings": settings})
	return err
}

type MCPStatus struct {
	MCPServers []MCPServerStatus `json:"mcpServers"`
	Raw        json.RawMessage   `json:"-"`
}
type MCPServerStatus struct {
	Name       string          `json:"name"`
	Status     string          `json:"status"`
	ServerInfo json.RawMessage `json:"serverInfo,omitempty"`
	Error      string          `json:"error,omitempty"`
	Config     json.RawMessage `json:"config,omitempty"`
	Scope      string          `json:"scope,omitempty"`
	Source     string          `json:"source,omitempty"`
	Tools      json.RawMessage `json:"tools,omitempty"`
}

func (c *Client) GetMCPStatus(ctx context.Context) (*MCPStatus, error) {
	data, err := c.control(ctx, "mcp_status", nil)
	if err != nil {
		return nil, err
	}
	r := &MCPStatus{Raw: data}
	err = json.Unmarshal(data, r)
	return r, err
}
func (c *Client) GetContextUsage(ctx context.Context, detail string) (json.RawMessage, error) {
	f := map[string]any{}
	if detail != "" {
		if detail != "full" && detail != "summary" {
			return nil, fmt.Errorf("claude: invalid usage detail")
		}
		f["detail"] = detail
	}
	return c.control(ctx, "get_context_usage", f)
}
func (c *Client) ReconnectMCPServer(ctx context.Context, name string) error {
	_, err := c.control(ctx, "mcp_reconnect", map[string]any{"serverName": name})
	return err
}
func (c *Client) ToggleMCPServer(ctx context.Context, name string, enabled bool) error {
	_, err := c.control(ctx, "mcp_toggle", map[string]any{"serverName": name, "enabled": enabled})
	return err
}
func (c *Client) ReadMCPResource(ctx context.Context, name, uri string) (json.RawMessage, error) {
	return c.control(ctx, "mcp_read_resource", map[string]any{"serverName": name, "uri": uri})
}
func (c *Client) SetMCPServers(ctx context.Context, servers map[string]json.RawMessage) (json.RawMessage, error) {
	return c.control(ctx, "mcp_set_servers", map[string]any{"servers": servers})
}
func (c *Client) StopTask(ctx context.Context, id string) error {
	_, err := c.control(ctx, "stop_task", map[string]any{"task_id": id})
	return err
}
func (c *Client) BackgroundTasks(ctx context.Context, id string) (json.RawMessage, error) {
	f := map[string]any{}
	if id != "" {
		f["tool_use_id"] = id
	}
	return c.control(ctx, "background_tasks", f)
}
func (c *Client) CancelAsyncMessage(ctx context.Context, uuid string) (json.RawMessage, error) {
	return c.control(ctx, "cancel_async_message", map[string]any{"message_uuid": uuid})
}
func (c *Client) RewindFiles(ctx context.Context, id string, dryRun bool) (json.RawMessage, error) {
	return c.control(ctx, "rewind_files", map[string]any{"user_message_id": id, "dry_run": dryRun})
}
func (c *Client) SeedReadState(ctx context.Context, path string, mtime float64) error {
	_, err := c.control(ctx, "seed_read_state", map[string]any{"path": path, "mtime": mtime})
	return err
}

type ReadFileOptions struct {
	MaxBytes int
	Encoding string
}
type ReadFileResult struct {
	Contents  string `json:"contents"`
	AbsPath   string `json:"absPath"`
	Truncated bool   `json:"truncated,omitempty"`
	Encoding  string `json:"encoding,omitempty"`
	Lossy     *bool  `json:"lossy,omitempty"`
}

func (c *Client) ReadFile(ctx context.Context, path string, o ReadFileOptions) (*ReadFileResult, error) {
	f := map[string]any{"path": path}
	if o.MaxBytes > 0 {
		f["max_bytes"] = o.MaxBytes
	}
	if o.Encoding != "" {
		f["encoding"] = o.Encoding
	}
	data, err := c.control(ctx, "read_file", f)
	if err != nil {
		return nil, err
	}
	var r *ReadFileResult
	err = json.Unmarshal(data, &r)
	return r, err
}

type ReloadPluginsOptions struct {
	HoldOnCacheImpact bool
	AddPlugins        []PluginConfig
}

func (c *Client) ReloadPlugins(ctx context.Context, o ReloadPluginsOptions) (json.RawMessage, error) {
	f := map[string]any{"hold_on_cache_impact": o.HoldOnCacheImpact}
	if o.AddPlugins != nil {
		f["add_plugins"] = o.AddPlugins
	}
	return c.control(ctx, "reload_plugins", f)
}
func (c *Client) ReloadSkills(ctx context.Context) (json.RawMessage, error) {
	return c.control(ctx, "reload_skills", nil)
}
func (c *Client) ReloadOutputStyles(ctx context.Context) (json.RawMessage, error) {
	return c.control(ctx, "reload_output_styles", nil)
}
func (c *Client) GetUsage(ctx context.Context, skip bool) (json.RawMessage, error) {
	return c.control(ctx, "get_usage", map[string]any{"skip_behaviors": skip})
}
func (c *Client) GetSessionCost(ctx context.Context) (json.RawMessage, error) {
	return c.control(ctx, "get_session_cost", nil)
}
func (c *Client) GetSettings(ctx context.Context) (json.RawMessage, error) {
	return c.control(ctx, "get_settings", nil)
}
func (c *Client) GetHooksListing(ctx context.Context) (json.RawMessage, error) {
	return c.control(ctx, "get_hooks_listing", nil)
}
func (c *Client) GetTaskOutput(ctx context.Context, id string) (json.RawMessage, error) {
	return c.control(ctx, "get_task_output", map[string]any{"task_id": id})
}
func (c *Client) ListModels(ctx context.Context) (json.RawMessage, error) {
	return c.control(ctx, "list_models", nil)
}
func (c *Client) ListPermissionRules(ctx context.Context) (json.RawMessage, error) {
	return c.control(ctx, "list_permission_rules", nil)
}
func (c *Client) GetBinaryVersion(ctx context.Context) (json.RawMessage, error) {
	return c.control(ctx, "get_binary_version", nil)
}
func (c *Client) RenameCurrentSession(ctx context.Context, title string) error {
	_, err := c.control(ctx, "rename_session", map[string]any{"title": title, "source": "host"})
	return err
}

// StreamInput sends messages through a channel, closing input after the final run settles.
func (c *Client) StreamInput(ctx context.Context, input <-chan UserMessage) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m, ok := <-input:
			if !ok {
				if c.run.bidirectional {
					if err := c.run.wait(ctx); err != nil {
						return err
					}
				}
				return c.EndInput(ctx)
			}
			if err := c.SendMessage(ctx, m); err != nil {
				return err
			}
		}
	}
}
