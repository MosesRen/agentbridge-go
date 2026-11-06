package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PermissionMode is a native Claude Code permission mode.
type PermissionMode string

const (
	PermissionDefault     PermissionMode = "default"
	PermissionAuto        PermissionMode = "auto"
	PermissionAcceptEdits PermissionMode = "acceptEdits"
	PermissionPlan        PermissionMode = "plan"
	PermissionDontAsk     PermissionMode = "dontAsk"
	PermissionBypass      PermissionMode = "bypassPermissions"
)

// Options configures a single Claude Code process. The zero value is usable.
// Options and referenced maps/slices must not be mutated after Dial.
type Options struct {
	CLIPath                 string    // Defaults to claude on PATH. Must be an executable, not a shell command.
	Transport               Transport // Optional externally managed protocol transport; CLI flags are not applied to it.
	Dir                     string
	Env                     map[string]string // Overrides the inherited environment. Never included in errors.
	Model                   string
	SystemPrompt            *string // nil uses an empty SDK prompt, matching upstream. See UseClaudeCodePrompt.
	AppendSystemPrompt      string
	Tools                   []string // nil keeps defaults; an empty, non-nil slice disables built-in tools.
	AllowedTools            []string
	DisallowedTools         []string
	PermissionMode          PermissionMode
	MaxTurns                int
	MaxBudgetUSD            float64
	Resume                  string
	SessionID               string
	Continue                bool
	ForkSession             bool
	NoSessionPersistence    bool
	IncludePartialMessages  bool
	SettingSources          []string // nil follows upstream omission; an explicit empty slice sends an empty source list.
	AddDirs                 []string
	MCPConfig               json.RawMessage // Native {"mcpServers": {...}} configuration for external servers.
	StrictMCPConfig         bool
	JSONSchema              json.RawMessage
	CanUseTool              PermissionHandler
	UseClaudeCodePrompt     bool // Opt into the Claude Code preset instead of the empty SDK prompt.
	SystemPromptFile        string
	SystemPromptBlocks      []string // Native prompt segments, including the dynamic boundary marker.
	SystemPromptSnapshot    *bool
	ExcludeDynamicSections  *bool
	FallbackModel           string
	Agent                   string
	Agents                  map[string]AgentDefinition
	Betas                   []string
	Settings                string // Inline JSON object or a settings file path.
	Sandbox                 *SandboxSettings
	Plugins                 []PluginConfig
	Skills                  []string // nil: unspecified; non-nil: explicit skill allowlist, including empty.
	AllSkills               bool
	Thinking                *ThinkingConfig
	Effort                  string
	TaskBudget              int
	BudgetLimit             *float64 // Explicit budget, including zero; overrides MaxBudgetUSD.
	ResumeSessionAt         string
	ResumeDropsTurn         *string
	EnableFileCheckpointing bool
	IncludeHookEvents       bool
	ForwardSubagentText     bool
	VerbatimPrompts         bool
	Bare                    bool
	Hooks                   map[HookEvent][]HookMatcher
	SDKServers              map[string]*MCPServer
	OnElicitation           ElicitationHandler
	OnUserDialog            UserDialogHandler
	OnControlRequest        ControlHandler     // Optional handler for newer server-initiated requests.
	ExtraArgs               map[string]*string // Native flag names without '--'; nil means a boolean flag.
	RunEndTimeout           time.Duration      // Default 10 minutes between background continuation turns; negative disables.
	SessionStore            SessionStore
	LoadTimeout             time.Duration // Default 60s for external store operations.
	SupportedDialogKinds    []string
	InitializeOptions       map[string]any // Additional native initialize fields; cannot override SDK-owned fields.
	mergedSettings          string
	mergedMCP               json.RawMessage
	StartupTimeout          time.Duration // Default 60s; bounds initialization, not model execution.
	ControlTimeout          time.Duration // Default 60s, bounded by the request context.
	CallbackTimeout         time.Duration // Zero adds no host deadline; native hook deadlines still apply.
	CloseTimeout            time.Duration // Default 2s before forcibly terminating the process tree.
	MaxMessageBytes         int           // Default 8 MiB per JSONL record.
	EventBuffer             int           // Default 0: unbounded. Positive values fail on overflow without dropping events.
	commandPrefix           []string      // Used by subprocess protocol tests.
}

func (o Options) normalized() (Options, error) {
	if o.CLIPath == "" {
		o.CLIPath = "claude"
	}
	if o.MaxTurns < 0 || o.MaxBudgetUSD < 0 || math.IsNaN(o.MaxBudgetUSD) || math.IsInf(o.MaxBudgetUSD, 0) {
		return o, fmt.Errorf("claude: turn and budget limits must be finite and non-negative")
	}
	if o.Resume != "" && (o.Continue || o.SessionID != "") || o.Continue && o.SessionID != "" {
		return o, fmt.Errorf("claude: resume, continue, and session ID are mutually exclusive")
	}
	if o.ForkSession && o.Resume == "" && !o.Continue {
		return o, fmt.Errorf("claude: fork requires resume or continue")
	}
	for name, value := range map[string]json.RawMessage{"MCPConfig": o.MCPConfig, "JSONSchema": o.JSONSchema} {
		present := len(value) > 0
		value = bytes.TrimSpace(value)
		if present && (len(value) == 0 || !json.Valid(value) || value[0] != '{') {
			return o, fmt.Errorf("claude: %s must be a JSON object", name)
		}
	}
	switch o.PermissionMode {
	case "", PermissionDefault, PermissionAuto, PermissionAcceptEdits, PermissionPlan, PermissionDontAsk, PermissionBypass:
	default:
		return o, fmt.Errorf("claude: unsupported permission mode %q", o.PermissionMode)
	}
	if o.StartupTimeout == 0 {
		o.StartupTimeout = 60 * time.Second
	}
	if o.ControlTimeout == 0 {
		o.ControlTimeout = 60 * time.Second
	}
	if o.CloseTimeout == 0 {
		o.CloseTimeout = 2 * time.Second
	}
	if o.MaxMessageBytes == 0 {
		o.MaxMessageBytes = 8 << 20
	}
	if o.StartupTimeout < 0 || o.ControlTimeout < 0 || o.CallbackTimeout < 0 || o.CloseTimeout < 0 || o.MaxMessageBytes < 1 || o.EventBuffer < 0 {
		return o, fmt.Errorf("claude: timeouts and buffer sizes must be positive")
	}
	for key, value := range o.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, '\x00') {
			return o, fmt.Errorf("claude: invalid environment entry")
		}
	}
	return o.extendedDefaults()
}

func (o Options) args() []string {
	args := append([]string{}, o.commandPrefix...)
	args = append(args, "--print", "--verbose", "--input-format=stream-json", "--output-format=stream-json")
	if o.CanUseTool != nil {
		args = append(args, "--permission-prompt-tool=stdio")
	}
	add := func(flag, value string) { args = append(args, flag+"="+value) }
	if o.SettingSources != nil {
		add("--setting-sources", strings.Join(o.SettingSources, ","))
	}
	if o.Model != "" {
		add("--model", o.Model)
	}
	if o.SystemPromptFile != "" {
		add("--system-prompt-file", o.SystemPromptFile)
	}
	if o.Tools != nil {
		add("--tools", strings.Join(o.Tools, ","))
	}
	if len(o.AllowedTools) > 0 {
		add("--allowedTools", strings.Join(o.AllowedTools, ","))
	}
	if len(o.DisallowedTools) > 0 {
		add("--disallowedTools", strings.Join(o.DisallowedTools, ","))
	}
	if o.PermissionMode != "" {
		add("--permission-mode", string(o.PermissionMode))
	}
	if o.MaxTurns > 0 {
		add("--max-turns", strconv.Itoa(o.MaxTurns))
	}
	if o.BudgetLimit != nil {
		add("--max-budget-usd", strconv.FormatFloat(*o.BudgetLimit, 'f', -1, 64))
	} else if o.MaxBudgetUSD > 0 {
		add("--max-budget-usd", strconv.FormatFloat(o.MaxBudgetUSD, 'f', -1, 64))
	}
	if o.Resume != "" {
		add("--resume", o.Resume)
	}
	if o.SessionID != "" {
		add("--session-id", o.SessionID)
	}
	if o.Continue {
		args = append(args, "--continue")
	}
	if o.ForkSession {
		args = append(args, "--fork-session")
	}
	if o.NoSessionPersistence {
		args = append(args, "--no-session-persistence")
	}
	if o.IncludePartialMessages {
		args = append(args, "--include-partial-messages")
	}
	if o.StrictMCPConfig {
		args = append(args, "--strict-mcp-config")
	}
	if len(o.mergedMCP) > 0 {
		add("--mcp-config", string(o.mergedMCP))
	} else if len(o.MCPConfig) > 0 {
		add("--mcp-config", string(o.MCPConfig))
	}
	if len(o.JSONSchema) > 0 {
		add("--json-schema", string(o.JSONSchema))
	}
	for _, dir := range o.AddDirs {
		add("--add-dir", dir)
	}
	return append(args, o.extendedArgs()...)
}

func (o Options) environment() []string {
	values := make(map[string]string)
	keyName := func(key string) string {
		if runtime.GOOS == "windows" {
			return strings.ToUpper(key)
		}
		return key
	}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok && keyName(key) != "CLAUDECODE" {
			values[keyName(key)] = value
		}
	}
	values["CLAUDE_CODE_ENTRYPOINT"] = "sdk-go"
	values["CLAUDE_CODE_SDK_READS_SESSION_STATE"] = "1"
	if o.EnableFileCheckpointing {
		values["CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING"] = "true"
	}
	for key, value := range o.Env {
		values[keyName(key)] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}
