package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type AgentDefinition struct {
	Description     string            `json:"description"`
	Prompt          string            `json:"prompt"`
	Tools           []string          `json:"tools,omitempty"`
	DisallowedTools []string          `json:"disallowedTools,omitempty"`
	Model           string            `json:"model,omitempty"`
	Skills          []string          `json:"skills,omitempty"`
	Memory          string            `json:"memory,omitempty"`
	MCPServers      []json.RawMessage `json:"mcpServers,omitempty"`
	Background      *bool             `json:"background,omitempty"`
	Effort          any               `json:"effort,omitempty"`
}

type PluginConfig struct {
	Type             string `json:"type"` // Empty and "local" mean a local plugin.
	Path             string `json:"path"`
	SkipMCPDiscovery bool   `json:"skipMcpDiscovery,omitempty"`
}

type ThinkingConfig struct {
	Type         string `json:"type"` // adaptive, enabled, disabled
	BudgetTokens int    `json:"budgetTokens,omitempty"`
	Display      string `json:"display,omitempty"`
}

// SandboxSettings uses native CLI JSON field names; pointer booleans preserve
// the distinction between omitted and explicitly disabled policy.
type SandboxSettings struct {
	Enabled                   *bool               `json:"enabled,omitempty"`
	AutoAllowBashIfSandboxed  *bool               `json:"autoAllowBashIfSandboxed,omitempty"`
	ExcludedCommands          []string            `json:"excludedCommands,omitempty"`
	AllowUnsandboxedCommands  *bool               `json:"allowUnsandboxedCommands,omitempty"`
	Network                   map[string]any      `json:"network,omitempty"`
	IgnoreViolations          map[string][]string `json:"ignoreViolations,omitempty"`
	EnableWeakerNestedSandbox *bool               `json:"enableWeakerNestedSandbox,omitempty"`
}

var skillNamePattern = regexp.MustCompile(`[(),\x00-\x1f\x7f-\x9f\x{feff}]`)
var flagNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)

func (o Options) extendedDefaults() (Options, error) {
	if o.LoadTimeout == 0 {
		o.LoadTimeout = 60 * time.Second
	}
	if o.LoadTimeout < 0 {
		return o, fmt.Errorf("claude: negative store timeout")
	}
	if o.SessionStore != nil && o.NoSessionPersistence {
		return o, fmt.Errorf("claude: session store requires session persistence and transcript mirroring")
	}
	if o.RunEndTimeout == 0 {
		o.RunEndTimeout = 10 * time.Minute
	}
	if o.BudgetLimit != nil && (*o.BudgetLimit < 0 || math.IsNaN(*o.BudgetLimit) || math.IsInf(*o.BudgetLimit, 0)) {
		return o, fmt.Errorf("claude: invalid budget limit")
	}
	if o.TaskBudget < 0 {
		return o, fmt.Errorf("claude: task budget cannot be negative")
	}
	if o.SystemPromptFile != "" && (o.SystemPrompt != nil || o.UseClaudeCodePrompt) || o.SystemPrompt != nil && o.UseClaudeCodePrompt {
		return o, fmt.Errorf("claude: prompt text, file and preset are mutually exclusive")
	}
	if o.SystemPromptBlocks != nil && (o.SystemPromptFile != "" || o.SystemPrompt != nil || o.UseClaudeCodePrompt) {
		return o, fmt.Errorf("claude: prompt blocks conflict with prompt text, file or preset")
	}
	if o.AllSkills && o.Skills != nil {
		return o, fmt.Errorf("claude: Skills and AllSkills are mutually exclusive")
	}
	if o.Skills != nil || o.AllSkills {
		o.AllowedTools = append([]string{}, o.AllowedTools...)
		if o.AllSkills {
			o.AllowedTools = appendUnique(o.AllowedTools, "Skill")
		}
		for _, name := range o.Skills {
			if name == "" || strings.TrimSpace(name) != name || skillNamePattern.MatchString(name) || name == "*" || strings.HasSuffix(name, ":*") || strings.HasSuffix(name, " *") || strings.HasPrefix(name, "/") || strings.Contains(name, `\\`) {
				return o, fmt.Errorf("claude: invalid skill name %q", name)
			}
			o.AllowedTools = appendUnique(o.AllowedTools, "Skill("+name+")")
		}
		if o.SettingSources == nil {
			o.SettingSources = []string{"user", "project"}
		}
	}
	if o.Effort != "" {
		switch o.Effort {
		case "low", "medium", "high", "xhigh", "max":
		default:
			return o, fmt.Errorf("claude: invalid effort %q", o.Effort)
		}
	}
	if o.Thinking != nil {
		switch o.Thinking.Type {
		case "adaptive", "disabled":
		case "enabled":
			if o.Thinking.BudgetTokens <= 0 {
				return o, fmt.Errorf("claude: enabled thinking requires a positive token budget")
			}
		default:
			return o, fmt.Errorf("claude: invalid thinking type")
		}
		if o.Thinking.Display != "" && o.Thinking.Display != "summarized" && o.Thinking.Display != "omitted" && o.Thinking.Display != "highlights" {
			return o, fmt.Errorf("claude: invalid thinking display")
		}
	}
	for _, p := range o.Plugins {
		if (p.Type != "" && p.Type != "local") || p.Path == "" {
			return o, fmt.Errorf("claude: plugin must be local and have a path")
		}
	}
	for flag := range o.ExtraArgs {
		if !flagNamePattern.MatchString(flag) {
			return o, fmt.Errorf("claude: invalid extra flag name %q", flag)
		}
		switch flag {
		case "print", "input-format", "output-format", "permission-prompt-tool":
			return o, fmt.Errorf("claude: %s is owned by the SDK transport", flag)
		}
	}
	if err := validateHooks(o.Hooks); err != nil {
		return o, err
	}
	if o.Settings != "" || o.Sandbox != nil {
		settings := map[string]json.RawMessage{}
		if o.Settings != "" {
			data := []byte(o.Settings)
			if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
				var err error
				data, err = os.ReadFile(o.Settings)
				if err != nil {
					return o, fmt.Errorf("claude: read settings file: %w", err)
				}
			}
			if err := json.Unmarshal(data, &settings); err != nil || settings == nil {
				return o, fmt.Errorf("claude: settings must be a JSON object")
			}
		}
		if o.Sandbox != nil {
			data, err := json.Marshal(o.Sandbox)
			if err != nil {
				return o, fmt.Errorf("claude: invalid sandbox settings: %w", err)
			}
			settings["sandbox"] = data
		}
		data, err := json.Marshal(settings)
		if err != nil {
			return o, err
		}
		o.mergedSettings = string(data)
	}
	if len(o.SDKServers) > 0 {
		var config map[string]json.RawMessage
		if len(o.MCPConfig) > 0 {
			if err := json.Unmarshal(o.MCPConfig, &config); err != nil {
				return o, err
			}
		}
		if config == nil {
			config = map[string]json.RawMessage{}
		}
		servers := map[string]json.RawMessage{}
		if len(config["mcpServers"]) > 0 {
			if err := json.Unmarshal(config["mcpServers"], &servers); err != nil {
				return o, fmt.Errorf("claude: invalid mcpServers object")
			}
		}
		if servers == nil {
			return o, fmt.Errorf("claude: mcpServers must be an object")
		}
		for name, server := range o.SDKServers {
			if name == "" || server == nil {
				return o, fmt.Errorf("claude: SDK MCP server must have a name and implementation")
			}
			if _, exists := servers[name]; exists {
				return o, fmt.Errorf("claude: duplicate MCP server %q", name)
			}
			if err := server.validate(); err != nil {
				return o, err
			}
			servers[name], _ = json.Marshal(map[string]any{"type": "sdk", "name": name})
		}
		config["mcpServers"], _ = json.Marshal(servers)
		o.mergedMCP, _ = json.Marshal(config)
	}
	return o, nil
}

func appendUnique(values []string, value string) []string {
	for _, old := range values {
		if old == value {
			return values
		}
	}
	return append(values, value)
}

func (o Options) extendedArgs() []string {
	var args []string
	add := func(flag, value string) { args = append(args, "--"+flag+"="+value) }
	for _, pair := range [][2]string{{"fallback-model", o.FallbackModel}, {"agent", o.Agent}, {"effort", o.Effort}, {"resume-session-at", o.ResumeSessionAt}} {
		if pair[1] != "" {
			add(pair[0], pair[1])
		}
	}
	if len(o.Betas) > 0 {
		add("betas", strings.Join(o.Betas, ","))
	}
	if o.mergedSettings != "" {
		add("settings", o.mergedSettings)
	}
	if o.TaskBudget > 0 {
		add("task-budget", strconv.Itoa(o.TaskBudget))
	}
	if o.ResumeDropsTurn != nil {
		add("resume-drops-turn", *o.ResumeDropsTurn)
	}
	if o.Thinking != nil {
		if o.Thinking.Type == "enabled" {
			add("max-thinking-tokens", strconv.Itoa(o.Thinking.BudgetTokens))
		} else {
			add("thinking", o.Thinking.Type)
		}
		if o.Thinking.Display != "" && o.Thinking.Type != "disabled" {
			add("thinking-display", o.Thinking.Display)
		}
	}
	if o.IncludeHookEvents {
		args = append(args, "--include-hook-events")
	}
	if o.EnableFileCheckpointing {
		args = append(args, "--replay-user-messages")
	}
	if o.Bare {
		args = append(args, "--bare")
	}
	if o.SessionStore != nil {
		args = append(args, "--session-mirror")
	}
	for _, plugin := range o.Plugins {
		flag := "plugin-dir"
		if plugin.SkipMCPDiscovery {
			flag = "plugin-dir-no-mcp"
		}
		add(flag, plugin.Path)
	}
	keys := make([]string, 0, len(o.ExtraArgs))
	for key := range o.ExtraArgs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value := o.ExtraArgs[key]; value == nil {
			args = append(args, "--"+key)
		} else {
			add(key, *value)
		}
	}
	return args
}
