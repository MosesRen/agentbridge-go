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
	CLIPath                string // Defaults to claude on PATH. Must be an executable, not a shell command.
	Dir                    string
	Env                    map[string]string // Overrides the inherited environment. Never included in errors.
	Model                  string
	SystemPrompt           *string // nil keeps the CLI default; a pointer to "" replaces it with empty text.
	AppendSystemPrompt     string
	Tools                  []string // nil keeps defaults; an empty, non-nil slice disables built-in tools.
	AllowedTools           []string
	DisallowedTools        []string
	PermissionMode         PermissionMode
	MaxTurns               int
	MaxBudgetUSD           float64
	Resume                 string
	SessionID              string
	Continue               bool
	ForkSession            bool
	NoSessionPersistence   bool
	IncludePartialMessages bool
	SettingSources         []string // nil/empty loads no setting sources, matching SDK isolation.
	AddDirs                []string
	MCPConfig              json.RawMessage // Native {"mcpServers": {...}} configuration for external servers.
	StrictMCPConfig        bool
	JSONSchema             json.RawMessage
	CanUseTool             PermissionHandler
	StartupTimeout         time.Duration // Default 30s; bounds initialization, not model execution.
	ControlTimeout         time.Duration // Default 30s; used when the request context has no earlier deadline.
	CallbackTimeout        time.Duration // Default 30s; permission handlers must honor their context.
	CloseTimeout           time.Duration // Default 2s before forcibly terminating the process tree.
	MaxMessageBytes        int           // Default 8 MiB per JSONL record.
	EventBuffer            int           // Default 256; overflow terminates the client rather than dropping events.
	commandPrefix          []string      // Used by subprocess protocol tests.
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
		o.StartupTimeout = 30 * time.Second
	}
	if o.ControlTimeout == 0 {
		o.ControlTimeout = 30 * time.Second
	}
	if o.CallbackTimeout == 0 {
		o.CallbackTimeout = 30 * time.Second
	}
	if o.CloseTimeout == 0 {
		o.CloseTimeout = 2 * time.Second
	}
	if o.MaxMessageBytes == 0 {
		o.MaxMessageBytes = 8 << 20
	}
	if o.EventBuffer == 0 {
		o.EventBuffer = 256
	}
	if o.StartupTimeout < 0 || o.ControlTimeout < 0 || o.CallbackTimeout < 0 || o.CloseTimeout < 0 || o.MaxMessageBytes < 1 || o.EventBuffer < 1 {
		return o, fmt.Errorf("claude: timeouts and buffer sizes must be positive")
	}
	for key, value := range o.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, '\x00') {
			return o, fmt.Errorf("claude: invalid environment entry")
		}
	}
	return o, nil
}

func (o Options) args() []string {
	args := append([]string{}, o.commandPrefix...)
	args = append(args, "--print", "--verbose", "--input-format=stream-json", "--output-format=stream-json", "--permission-prompt-tool=stdio", "--setting-sources="+strings.Join(o.SettingSources, ","))
	add := func(flag, value string) { args = append(args, flag+"="+value) }
	if o.Model != "" {
		add("--model", o.Model)
	}
	if o.SystemPrompt != nil {
		add("--system-prompt", *o.SystemPrompt)
	}
	if o.AppendSystemPrompt != "" {
		add("--append-system-prompt", o.AppendSystemPrompt)
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
	if o.MaxBudgetUSD > 0 {
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
	if len(o.MCPConfig) > 0 {
		add("--mcp-config", string(o.MCPConfig))
	}
	if len(o.JSONSchema) > 0 {
		add("--json-schema", string(o.JSONSchema))
	}
	for _, dir := range o.AddDirs {
		add("--add-dir", dir)
	}
	return args
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
