package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func nativeMessage(t *testing.T, object map[string]any) Message {
	t.Helper()
	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	var message Message
	if err = json.Unmarshal(raw, &message); err != nil {
		t.Fatal(err)
	}
	message.Raw = raw
	return message
}
func trackerFinished(r *runTracker) bool { r.mu.Lock(); defer r.mu.Unlock(); return r.finished }

func TestRunTracksAgentContinuations(t *testing.T) {
	r := newRunTracker(Options{CanUseTool: func(context.Context, PermissionRequest) (PermissionDecision, error) { return PermissionDecision{}, nil }, RunEndTimeout: time.Second})
	defer r.close()
	r.sent("prompt")
	frames := []map[string]any{
		{"type": "system", "subtype": "session_state_changed", "state": "running", "sdk_host_only": true},
		{"type": "system", "subtype": "task_started", "task_id": "agent", "task_type": "local_agent"},
		{"type": "result", "subtype": "success", "user_message_uuid": "prompt"},
		{"type": "system", "subtype": "task_notification", "task_id": "agent", "status": "completed"},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": "continuation"}},
		{"type": "result", "subtype": "success"},
	}
	for _, frame := range frames {
		r.observe(nativeMessage(t, frame))
		if trackerFinished(r) {
			t.Fatalf("run ended before idle after %v", frame)
		}
	}
	visible := r.observe(nativeMessage(t, map[string]any{"type": "system", "subtype": "session_state_changed", "state": "idle", "sdk_host_only": true}))
	if visible || !trackerFinished(r) {
		t.Fatal("host-only idle should settle without being exposed")
	}
}

func TestRunDoesNotWaitOnPerpetualShell(t *testing.T) {
	r := newRunTracker(Options{})
	defer r.close()
	r.sent("prompt")
	r.observe(nativeMessage(t, map[string]any{"type": "system", "subtype": "task_started", "task_id": "server", "task_type": "local_bash"}))
	r.observe(nativeMessage(t, map[string]any{"type": "result", "subtype": "success"}))
	if !trackerFinished(r) {
		t.Fatal("perpetual shell prevents completion")
	}
}

func TestQueuedAndCoalescedPrompts(t *testing.T) {
	r := newRunTracker(Options{})
	defer r.close()
	r.sent("one")
	r.sent("two")
	r.sent("three")
	r.observe(nativeMessage(t, map[string]any{"type": "result", "user_message_uuid": "one"}))
	if trackerFinished(r) {
		t.Fatal("closed input before queued prompts")
	}
	r.observe(nativeMessage(t, map[string]any{"type": "result", "user_message_uuids": []string{"two", "three"}}))
	if !trackerFinished(r) {
		t.Fatal("coalesced result did not settle queued prompts")
	}
	r.sent("four")
	if trackerFinished(r) {
		t.Fatal("next turn did not reopen tracking")
	}
}

func TestHookRegistrationAndCapabilityGate(t *testing.T) {
	var calls atomic.Int32
	callback := func(ctx context.Context, input HookInput, toolID string) (HookOutput, error) {
		if input.ToolName != "Read" || toolID != "tool" || !json.Valid(input.Raw) {
			return nil, errors.New("bad hook input")
		}
		calls.Add(1)
		return HookOutput{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": "blocked"}}, nil
	}
	client := &Client{opts: Options{Hooks: map[HookEvent][]HookMatcher{HookPreToolUse: {{Hooks: []HookCallback{callback}, OnFailure: "block"}}}}}
	request := client.initializeRequest()
	raw, _ := json.Marshal(request)
	if !strings.Contains(string(raw), `"hookCallbackIds":["hook_0"]`) {
		t.Fatalf("bad native registration %s", raw)
	}
	if err := client.checkInitialization(json.RawMessage(`{"hook_on_failure_supported":false}`)); err == nil {
		t.Fatal("accepted a server unable to enforce blocking hooks")
	}
	if err := client.checkInitialization(json.RawMessage(`{"hook_on_failure_supported":true,"hooks_applied":true}`)); err != nil {
		t.Fatal(err)
	}
	response, err := client.serverControl(context.Background(), json.RawMessage(`{"subtype":"hook_callback","callback_id":"hook_0","tool_use_id":"tool","input":{"hook_event_name":"PreToolUse","tool_name":"Read"}}`))
	if err != nil || calls.Load() != 1 {
		t.Fatalf("response=%v err=%v", response, err)
	}
	if _, err = client.serverControl(context.Background(), json.RawMessage(`{"subtype":"hook_callback","callback_id":"missing","input":{}}`)); err == nil {
		t.Fatal("unknown callback accepted")
	}
}

func TestSDKMCPUsesRealServerAndSchemaValidation(t *testing.T) {
	server := NewMCPServer("test", "1.0.0")
	type args struct {
		Name string `json:"name"`
	}
	var calls atomic.Int32
	mcp.AddTool(server.Server, &mcp.Tool{Name: "greet", Description: "Greet by name"}, func(ctx context.Context, _ *mcp.CallToolRequest, input args) (*mcp.CallToolResult, any, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Hello " + input.Name}}}, nil, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bridge, err := newMCPBridge(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.close()
	call := func(raw string) json.RawMessage {
		t.Helper()
		result, err := bridge.handle(ctx, json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	initialized := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`)
	if !strings.Contains(string(initialized), `"serverInfo"`) {
		t.Fatalf("bad initialize %s", initialized)
	}
	call(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	listed := call(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if !strings.Contains(string(listed), `"greet"`) {
		t.Fatalf("missing tool %s", listed)
	}
	result := call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"greet","arguments":{"name":"Go"}}}`)
	if !strings.Contains(string(result), "Hello Go") || calls.Load() != 1 {
		t.Fatalf("bad tool result %s", result)
	}
	call(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"greet","arguments":{"name":42}}}`)
	if calls.Load() != 1 {
		t.Fatal("invalid input reached typed tool handler")
	}
	unknown := call(`{"jsonrpc":"2.0","id":5,"method":"does/not/exist","params":{}}`)
	if !strings.Contains(string(unknown), `"error"`) {
		t.Fatalf("unknown method succeeded %s", unknown)
	}
}

func TestLatestOptionDefaultsAndSkillIsolation(t *testing.T) {
	options, err := (Options{}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(options.args(), "\n") + "\n"
	client := &Client{opts: options}
	init, _ := json.Marshal(client.initializeRequest())
	if !strings.Contains(string(init), `"systemPrompt":[""]`) || options.StartupTimeout != 60*time.Second || options.ControlTimeout != 60*time.Second {
		t.Fatal("upstream defaults differ")
	}
	options, err = (Options{UseClaudeCodePrompt: true, Skills: []string{"review"}, Thinking: &ThinkingConfig{Type: "enabled", BudgetTokens: 2048}, Effort: "high", Plugins: []PluginConfig{{Path: "/tmp/plugin"}}}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(options.args(), "\n")
	for _, want := range []string{"--setting-sources=user,project", "--allowedTools=Skill(review)", "--max-thinking-tokens=2048", "--effort=high", "--plugin-dir=/tmp/plugin"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %s", want, joined)
		}
	}
	if strings.Contains(joined, "--system-prompt=") {
		t.Fatal("preset replaced by empty prompt")
	}
	if _, err = (Options{Skills: []string{"review),Bash(*)"}}).normalized(); err == nil {
		t.Fatal("skill allowed rule injection")
	}
}

func fixtureUUID(n int) string { return fmt.Sprintf("%08x-0000-4000-8000-000000000000", n) }
func fixtureEntry(n, parent int, kind, content string) json.RawMessage {
	entry := map[string]any{"type": kind, "uuid": fixtureUUID(n), "sessionId": fixtureUUID(100), "timestamp": "2026-11-06T10:00:00Z", "message": map[string]any{"role": kind, "content": content}}
	if parent > 0 {
		entry["parentUuid"] = fixtureUUID(parent)
	}
	raw, _ := json.Marshal(entry)
	return raw
}

func TestSessionStoreAndMutations(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()
	options := SessionOptions{Directory: t.TempDir(), Store: store}
	id := fixtureUUID(100)
	key, err := sessionKey(id, options)
	if err != nil {
		t.Fatal(err)
	}
	entries := []json.RawMessage{fixtureEntry(1, 0, "user", "question"), fixtureEntry(2, 1, "assistant", "answer")}
	if err = store.Append(ctx, key, entries); err != nil {
		t.Fatal(err)
	}
	entries[0][0] = 'x'
	loaded, _ := store.Load(ctx, key)
	if !json.Valid(loaded[0]) {
		t.Fatal("append aliases caller memory")
	}
	loaded[0][0] = 'x'
	loaded, _ = store.Load(ctx, key)
	if !json.Valid(loaded[0]) {
		t.Fatal("load aliases stored memory")
	}
	if err = RenameSession(ctx, id, "New title", options); err != nil {
		t.Fatal(err)
	}
	tag := "test"
	if err = TagSession(ctx, id, &tag, options); err != nil {
		t.Fatal(err)
	}
	info, err := GetSessionInfo(ctx, id, options)
	if err != nil || info.CustomTitle != "New title" || info.Tag != "test" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	messages, err := GetSessionMessages(ctx, id, options)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%v err=%v", messages, err)
	}
	fork, err := ForkSession(ctx, id, ForkSessionOptions{SessionOptions: options, UpToMessageID: fixtureUUID(1)})
	if err != nil {
		t.Fatal(err)
	}
	forkMessages, err := GetSessionMessages(ctx, fork.SessionID, options)
	if err != nil || len(forkMessages) != 1 || forkMessages[0].UUID == fixtureUUID(1) {
		t.Fatalf("bad fork: %+v %v", forkMessages, err)
	}
	list, err := ListSessions(ctx, options)
	if err != nil || len(list) != 2 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if err = DeleteSession(ctx, id, options); err != nil {
		t.Fatal(err)
	}
	info, err = GetSessionInfo(ctx, id, options)
	if err != nil || info != nil {
		t.Fatalf("deleted session still present: %+v %v", info, err)
	}
}

func TestDiskSessionsAndTraversal(t *testing.T) {
	ctx := context.Background()
	options := SessionOptions{ConfigDir: t.TempDir(), Directory: t.TempDir()}
	project, err := ProjectKeyForDirectory(options.Directory)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(options.ConfigDir, "projects", project)
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id := fixtureUUID(100)
	data := append(fixtureEntry(1, 0, "user", "hello"), '\n')
	data = append(data, fixtureEntry(2, 1, "assistant", "world")...)
	data = append(data, '\n')
	if err = os.WriteFile(filepath.Join(dir, id+".jsonl"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = RenameSession(ctx, id, "Disk title", options); err != nil {
		t.Fatal(err)
	}
	info, err := GetSessionInfo(ctx, id, options)
	if err != nil || info.CustomTitle != "Disk title" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	if _, err = ForkSession(ctx, id, ForkSessionOptions{SessionOptions: options}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../escape", "/tmp/escape", "a/b", strings.Repeat("x", 36)} {
		if err = DeleteSession(ctx, bad, options); err == nil {
			t.Errorf("accepted invalid UUID %q", bad)
		}
	}
	if _, ok := FilePathToSessionKey(filepath.Join(options.ConfigDir, "escape.jsonl"), filepath.Join(options.ConfigDir, "projects")); ok {
		t.Fatal("accepted outside mirror root")
	}
}

func TestResumeMaterializationRedactsAndCleansAuth(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", source)
	if err := os.WriteFile(filepath.Join(source, ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"fixture-access","refreshToken":"fixture-refresh"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "settings.json"), []byte(`{"enabledPlugins":{"x":true},"env":{"CLAUDE_CONFIG_DIR":"bad","KEEP":"yes"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewInMemorySessionStore()
	dir := t.TempDir()
	key, _ := sessionKey(fixtureUUID(100), SessionOptions{Directory: dir})
	if err := store.Append(ctx, key, []json.RawMessage{fixtureEntry(1, 0, "user", "remember")}); err != nil {
		t.Fatal(err)
	}
	options, err := (Options{Dir: dir, SessionStore: store, Resume: key.SessionID}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	materialized, cleanup, err := materializeStoreResume(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	root := materialized.Env["CLAUDE_CONFIG_DIR"]
	data, err := os.ReadFile(filepath.Join(root, ".credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "fixture-refresh") || !strings.Contains(string(data), "fixture-access") {
		t.Fatal("incorrect credential redaction")
	}
	settings, err := os.ReadFile(filepath.Join(root, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(settings), "enabledPlugins") || strings.Contains(string(settings), "CLAUDE_CONFIG_DIR") {
		t.Fatal("unsafe resume settings copied")
	}
	cleanup()
	if _, err = os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("private config not cleaned")
	}
}
