//go:build integration

package claude

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func liveOptions(t *testing.T) Options {
	t.Helper()
	if os.Getenv("AGENTBRIDGE_LIVE_TEST") != "1" {
		t.Skip("opt-in real CLI test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	config := t.TempDir()
	if err := copyResumeAuth(ctx, config, nil); err != nil {
		t.Fatal(err)
	}
	return Options{CLIPath: os.Getenv("AGENTBRIDGE_CLAUDE_PATH"), Dir: t.TempDir(), Env: map[string]string{"CLAUDE_CONFIG_DIR": config}, SettingSources: []string{"user"}, Tools: []string{}, MCPConfig: json.RawMessage(`{"mcpServers":{}}`), StrictMCPConfig: true, MaxTurns: 5, MaxBudgetUSD: 0.25}
}

func TestLiveHooksAndSDKMCP(t *testing.T) {
	options := liveOptions(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	server := NewMCPServer("bridge", "1.0.0")
	type input struct {
		Key string `json:"key"`
	}
	var toolCalls, hooks atomic.Int32
	mcp.AddTool(server.Server, &mcp.Tool{Name: "lookup", Description: "Look up the verification code for the given key."}, func(ctx context.Context, _ *mcp.CallToolRequest, args input) (*mcp.CallToolResult, any, error) {
		toolCalls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "BRIDGE_TOOL_618"}}}, nil, nil
	})
	options.SDKServers = map[string]*MCPServer{"bridge": server}
	options.AllowedTools = []string{"mcp__bridge__lookup"}
	options.Hooks = map[HookEvent][]HookMatcher{HookPreToolUse: {{Hooks: []HookCallback{func(ctx context.Context, input HookInput, id string) (HookOutput, error) {
		hooks.Add(1)
		return HookOutput{}, nil
	}}, OnFailure: "block"}}}
	result, err := Run(ctx, "Call the bridge lookup tool with key=verification, then reply with exactly the code returned by the tool. Do not guess the code.", options)
	if err != nil {
		t.Fatal(err)
	}
	if toolCalls.Load() == 0 || hooks.Load() == 0 || strings.TrimSpace(result.Result) != "BRIDGE_TOOL_618" {
		t.Fatalf("tool calls=%d hooks=%d reply=%q", toolCalls.Load(), hooks.Load(), result.Result)
	}
}

func TestLiveExternalStoreResume(t *testing.T) {
	options := liveOptions(t)
	options.MaxTurns = 1
	store := NewInMemorySessionStore()
	options.SessionStore = store
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	first, err := Run(ctx, "Remember the code word IVORY. Reply only READY.", options)
	if err != nil {
		t.Fatal(err)
	}
	key, err := sessionKey(first.SessionID, SessionOptions{Directory: options.Dir})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := store.Load(ctx, key)
	if err != nil || len(entries) == 0 {
		t.Fatalf("missing mirrored transcript: %v", err)
	}
	options.Resume = first.SessionID
	second, err := Run(ctx, "What code word did I ask you to remember? Reply only that word.", options)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(second.Result) != "IVORY" || second.SessionID != first.SessionID {
		t.Fatalf("resume reply=%q session=%s", second.Result, second.SessionID)
	}
}
