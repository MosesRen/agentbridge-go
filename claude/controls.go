package claude

import (
	"context"
	"encoding/json"
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
func (c *Client) ResetModel(ctx context.Context) error {
	_, err := c.Request(ctx, map[string]any{"subtype": "set_model", "model": nil})
	return err
}

func (c *Client) control(ctx context.Context, subtype string, fields map[string]any) (json.RawMessage, error) {
	request := map[string]any{"subtype": subtype}
	for k, v := range fields {
		request[k] = v
	}
	return c.Request(ctx, request)
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
func (c *Client) ReconnectMCPServer(ctx context.Context, name string) error {
	_, err := c.control(ctx, "mcp_reconnect", map[string]any{"serverName": name})
	return err
}
func (c *Client) ToggleMCPServer(ctx context.Context, name string, enabled bool) error {
	_, err := c.control(ctx, "mcp_toggle", map[string]any{"serverName": name, "enabled": enabled})
	return err
}

// StreamInput sends messages through a channel, closing input after the final run settles.
func (c *Client) StreamInput(ctx context.Context, input <-chan UserMessage) error {
	for {
		select {
		case <-c.ctx.Done():
			return c.closedError()
		case <-ctx.Done():
			return ctx.Err()
		case m, ok := <-input:
			if !ok {
				if c.run.hasBidirectional() {
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
