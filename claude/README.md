# Claude SDK for Go

English | [简体中文](README.zh-CN.md)

An unofficial Go SDK for Claude Code's core workflows. It uses the native bidirectional `stream-json` protocol and does not require the Python or TypeScript SDK at runtime.

Requires **Go 1.25+** and a separately installed, authenticated Claude Code executable. Set `Options.CLIPath` when it is not on `PATH`; this option accepts an executable path, not a shell command.

```sh
go get github.com/MosesRen/agentbridge-go/claude@main
```

`@main` is a development snapshot. Pin its resolved pseudo-version or an available release tag for reproducible builds. See the [complete quick start](../README.md#quick-start), [capability matrix](COMPATIBILITY.md), and [versioning policy](../VERSIONING.md).

## Query and stream

The following snippets are function bodies using a caller-owned `ctx` and `options`. Import `context`, `encoding/json`, `errors`, `fmt`, `io`, and the relevant SDK packages as needed.

```go
result, err := claude.Run(ctx, "Explain this repository.", claude.Options{
    Dir:            ".",
    SettingSources: []string{"user"},
    MaxTurns:       10,
})
if err != nil { return err }
fmt.Println(result.Result)
```

`Run` consumes the complete one-shot run, including native background-agent continuation turns. Agent failures return both a non-nil `*Result` and a `*ResultError`.

```go
stream, err := claude.Query(ctx, "Review the current changes.", options)
if err != nil { return err }
defer stream.Close()

for {
    message, err := stream.Next(ctx)
    if errors.Is(err, io.EOF) { break }
    if err != nil { return err }
    fmt.Print(message.Text())
}
```

`IncludePartialMessages: true` includes incremental `stream_event` frames. Inspect `Message.Raw` to consume their native delta payloads; `Message.Text()` extracts text from user/assistant messages. Continue reading through EOF: an individual `result` does not necessarily end the run. `stream.Client()` provides controls for the same process.

## Persistent conversations

```go
client, err := claude.Dial(ctx, options)
if err != nil { return err }
defer client.Close()

for _, prompt := range []string{
    "Remember the code word ORCHID. Reply only READY.",
    "What code word did I give you?",
} {
    if err := client.Send(ctx, prompt); err != nil { return err }
    for {
        message, err := client.Receive(ctx)
        if err != nil { return err }
        if message.Type == "result" {
            result, err := message.Result()
            if err != nil { return err }
            fmt.Println(result.Result)
            break
        }
    }
}
```

Use `SendMessage` for native strings/content blocks and `StreamInput` for channel-based input. Keep one logical message consumer. Control responses and permission callbacks are processed independently of application calls to `Receive`.

## Configuration and resume

| Options | Purpose |
| --- | --- |
| `Model`, `FallbackModel` | Primary and fallback model |
| `SystemPrompt`, `AppendSystemPrompt`, `SystemPromptFile`, `UseClaudeCodePrompt` | Custom prompts or the Claude Code preset |
| `Dir`, `AddDirs`, `Env` | Working directory, additional directories, environment overrides |
| `Tools`, `AllowedTools`, `DisallowedTools`, `PermissionMode` | Tool availability and native permission controls |
| `MaxTurns`, `MaxBudgetUSD`, `BudgetLimit` | Turn/budget limits; `BudgetLimit` can explicitly be zero |
| `JSONSchema` | Structured output |
| `Thinking`, `Effort` | Reasoning configuration |
| `Agent`, `Agents`, `Skills`, `Plugins`, `Sandbox` | Common startup configuration |
| `Settings`, `SettingSources` | Settings JSON/file and enabled filesystem sources |
| `Resume`, `Continue`, `SessionID` | Resume, continue, or select a new session ID; mutually exclusive |
| `NoSessionPersistence` | Disable native transcript persistence |

Defaults are significant:

- `SystemPrompt: nil` uses an empty SDK prompt. Set `UseClaudeCodePrompt: true` for the Claude Code preset.
- `Tools: nil` preserves default tools; an explicitly empty slice disables built-in tools.
- `SettingSources: nil` omits the flag; an explicitly empty slice disables those filesystem sources.
- `Env` overlays the inherited environment; the nested-session marker `CLAUDECODE` is removed by default.

To resume in a new process, pass a previous `Result.SessionID` in `Options.Resume` and use the original working directory. Claude Code owns persistence and loading; a session created without persistence cannot later be resumed from that run.

## Permissions and Hooks

```go
options.CanUseTool = func(ctx context.Context, request claude.PermissionRequest) (claude.PermissionDecision, error) {
    if request.ToolName == "Read" {
        return claude.PermissionDecision{Behavior: "allow"}, nil
    }
    return claude.PermissionDecision{Behavior: "deny", Message: "Only reads are allowed."}, nil
}
```

Callbacks only receive asks forwarded to the host; native allow/deny rules may resolve a call before the callback runs. Errors, panics, invalid decisions, or expired callbacks never approve an operation. Handlers may run concurrently and must honor context cancellation. The envelope ID is available as `PermissionRequest.RequestID` or `ControlRequestID(ctx)`.

Register Go Hooks in `Options.Hooks`. `HookInput.Raw` and `HookOutput` preserve native protocol fields. `OnFailure: "block"` requires native capability confirmation; initialization fails if it cannot be confirmed.

## MCP tools

External servers use `MCPConfig` and optionally `StrictMCPConfig`. For in-process tools, import `github.com/modelcontextprotocol/go-sdk/mcp`:

```go
server := claude.NewMCPServer("greeting", "1.0.0")
type input struct {
    Name string `json:"name"`
}
mcp.AddTool(server.Server, &mcp.Tool{
    Name: "greet", Description: "Greet someone by name.",
}, func(ctx context.Context, request *mcp.CallToolRequest, args input) (*mcp.CallToolResult, any, error) {
    return &mcp.CallToolResult{Content: []mcp.Content{
        &mcp.TextContent{Text: "Hello " + args.Name},
    }}, nil, nil
})
options.SDKServers = map[string]*claude.MCPServer{"greeting": server}
```

The official Go MCP SDK validates tool input. `GetMCPStatus`, `ReconnectMCPServer`, and `ToggleMCPServer` expose basic connection management; native restrictions, including SDK-server reconnect limitations, still apply.

## Errors, cancellation, and ownership

| API | Behavior |
| --- | --- |
| Context passed to `Dial` | Owns the process lifetime |
| Context passed to `Receive` / `Next` | Cancels that wait; reading can be retried |
| Context passed to `Request` | Cancels the wait and sends native control cancellation; completed work cannot be undone |
| `Interrupt` | Interrupts execution while keeping the client alive |
| `SetModel`, `ResetModel`, `SetPermissionMode` | Change the running model/permission mode |
| `Close` | Idempotently terminates and reaps the owned process |
| `Wait` | Waits for cleanup and reports process/lifetime errors |
| `Stderr` | Explicitly reads the latest 32 KiB of diagnostics |

Initialization/control requests default to 60 seconds. Callbacks have no additional host deadline by default, but native cancellation and Hook deadlines apply. Unix cleanup targets the process group; other platforms currently terminate only the direct child. The default close grace is two seconds. Always close clients/streams; a forcibly terminated Go parent cannot guarantee child cleanup.

Messages default to an 8 MiB size limit. The event queue is unbounded unless `EventBuffer` is positive; overflow returns an error and closes the client instead of dropping messages. Formatted SDK errors omit stderr; inspect it explicitly when needed.

`Message.Raw`, `Request`, `ExtraArgs`, `InitializeOptions`, and `Transport` provide protocol-level access. Their existence does not imply support for every advanced host workflow.

## Development and scope

See [CONTRIBUTING](../CONTRIBUTING.md) for ordinary, live, and upstream comparison tests. Runnable examples: [query](examples/query/main.go), [session](examples/session/main.go).

The [capability matrix](COMPATIBILITY.md) separates implemented core features, validation coverage, and intentionally excluded advanced APIs. This module is not affiliated with Anthropic and does not promise full official-SDK parity.

Project code/documentation use the [MIT License](../LICENSE); [third-party notices](THIRD_PARTY_NOTICES.md) preserve dependency and reference notices. Claude Code and model-service terms remain separate.
