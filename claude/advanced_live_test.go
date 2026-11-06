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

func TestLiveDynamicSDKMCP(t *testing.T) {
	options := liveOptions(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, err := Dial(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := NewMCPServer("dynamic", "1")
	server.Server.AddResource(&mcp.Resource{URI: "ui://fixture", Name: "fixture"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "ui://fixture", MIMEType: "text/html;profile=mcp-app", Text: "dynamic resource"}}}, nil
	})
	if _, err = client.SetMCPServers(ctx, nil, map[string]*MCPServer{"dynamic": server}); err != nil {
		t.Fatal(err)
	}
	waitConnected := func() {
		t.Helper()
		for {
			status, e := client.GetMCPStatus(ctx)
			if e != nil {
				t.Fatal(e)
			}
			connected := false
			for _, item := range status.MCPServers {
				if item.Name == "dynamic" {
					if item.Status == "failed" {
						t.Fatalf("dynamic MCP failed: %s", item.Error)
					}
					connected = item.Status == "connected"
				}
			}
			if connected {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	waitConnected()
	result, err := client.mcpMessage(ctx, "dynamic", json.RawMessage(`{"jsonrpc":"2.0","id":"fixture-resource","method":"resources/read","params":{"uri":"ui://fixture"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), "dynamic resource") {
		t.Fatalf("bad dynamic resource %s", result)
	}
	replacement := NewMCPServer("dynamic", "2")
	replacement.Server.AddResource(&mcp.Resource{URI: "ui://fixture", Name: "fixture"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "ui://fixture", MIMEType: "text/html;profile=mcp-app", Text: "replacement resource"}}}, nil
	})
	if _, err = client.SetMCPServers(ctx, nil, map[string]*MCPServer{"dynamic": replacement}); err != nil {
		t.Fatal(err)
	}
	waitConnected()
	result, err = client.mcpMessage(ctx, "dynamic", json.RawMessage(`{"jsonrpc":"2.0","id":"fixture-resource-v2","method":"resources/read","params":{"uri":"ui://fixture"}}`))
	if err != nil || !strings.Contains(string(result), "replacement resource") {
		t.Fatalf("replacement result=%s err=%v", result, err)
	}
	if _, err = client.SetMCPServers(ctx, nil); err != nil {
		t.Fatal(err)
	}
	status, err := client.GetMCPStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range status.MCPServers {
		if server.Name == "dynamic" {
			t.Fatal("removed SDK server remains registered")
		}
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
