# Claude SDK for Go

[English](README.md) | 简体中文

为 Go 应用提供 Claude Code 的核心调用能力：单次查询、流式输出、多轮对话、常用启动配置、权限与 Hooks、MCP 工具和原生会话恢复。

这是非官方实现，通过 Claude 原生双向 `stream-json` 协议交互，运行时不依赖 Python 或 TypeScript SDK。需要 Go 1.25+，以及本机已安装、已配置认证的 Claude Code。`Options.CLIPath` 可以指定可执行文件路径，不接受 shell 命令字符串。

核心能力范围参考官方 Python SDK 0.2.178；协议验证基准为 Claude Code 2.1.318，部分参数和初始化协议另与 TypeScript SDK 0.3.318 对照。具体范围见 [能力清单](COMPATIBILITY.zh-CN.md)，不承诺复刻官方 SDK 的全部接口。

```sh
go get github.com/MosesRen/agentbridge-go/claude@main
```

`@main` 是开发快照；需要可复现构建时固定其伪版本或发布标签。完整可运行的入门代码见[项目首页](../README.zh-CN.md#快速开始)。下列代码块是函数体片段，使用调用方的 `ctx` 和 `options`，按需导入 `context`、`encoding/json`、`errors`、`fmt`、`io` 和 SDK 包。

## 单次查询

```go
result, err := claude.Run(ctx, "Explain this repository.", claude.Options{
    Dir: ".",
    SettingSources: []string{"user"},
    MaxTurns: 10,
})
if err != nil { return err }
fmt.Println(result.Result)
```

`Run` 消费整个单次运行并返回最终结果。原生后台 Agent 引发后续轮次时，SDK 会继续接收消息，直到运行结束。失败结果同时返回非 nil 的 `*Result` 和 `*ResultError`。

## 流式输出

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

设置 `IncludePartialMessages: true` 可接收增量 `stream_event`；通过 `Message.Raw` 处理原生 delta，`Message.Text()` 提取 user/assistant 消息中的文本。应继续读取到 EOF，单个 `result` 不一定意味着整个运行已经结束。`stream.Client()` 提供同一进程的控制接口。

## 多轮对话

```go
client, err := claude.Dial(ctx, options)
if err != nil { return err }
defer client.Close()

if err := client.Send(ctx, "Remember the code word ORCHID."); err != nil { return err }
for {
    message, err := client.Receive(ctx)
    if err != nil { return err }
    if message.Type == "result" {
        if _, err := message.Result(); err != nil { return err }
        break
    }
}
if err := client.Send(ctx, "What code word did I give you?"); err != nil { return err }
// 继续调用 Receive 消费下一轮消息。
```

`SendMessage` 接受原生字符串或内容块，`StreamInput` 从 channel 发送输入。使用单一逻辑消息消费者，控制请求和权限回调无需应用持续调用 `Receive` 才能处理。

## 常用配置与恢复

| 选项 | 作用 |
| --- | --- |
| `Model`、`FallbackModel` | 模型与降级模型 |
| `SystemPrompt`、`AppendSystemPrompt`、`SystemPromptFile`、`UseClaudeCodePrompt` | 自定义提示词或 Claude Code preset |
| `Dir`、`AddDirs`、`Env` | 工作目录、额外目录和环境变量 |
| `Tools`、`AllowedTools`、`DisallowedTools` | 内置工具范围和权限规则 |
| `PermissionMode` | 原生权限模式 |
| `MaxTurns`、`MaxBudgetUSD`、`BudgetLimit` | 轮次和预算上限；`BudgetLimit` 可显式设零 |
| `JSONSchema` | 结构化输出 |
| `Thinking`、`Effort` | 思考与推理配置 |
| `Agent`、`Agents`、`Skills`、`Plugins`、`Sandbox` | 常用启动配置 |
| `Settings`、`SettingSources` | 设置内容/文件和来源选择 |
| `Resume`、`Continue`、`SessionID` | 原生会话恢复、继续或指定新会话 ID；三者互斥 |
| `NoSessionPersistence` | 关闭原生会话持久化 |

`SystemPrompt: nil` 使用空 SDK 提示词；`UseClaudeCodePrompt: true` 使用 Claude Code preset。`Tools: nil` 保留默认工具，空但非 nil 的切片禁用全部内置工具。`SettingSources: nil` 省略启动参数，空但非 nil 的切片显式禁用这些设置来源。

恢复已保存的会话时，取上一次 `Result.SessionID`，在新客户端的 `Options.Resume` 中传入，并使用原工作目录。持久化和加载由 Claude Code 完成；关闭了持久化的会话不能用于后续进程恢复。

## 权限、Hooks 与 MCP

```go
options.CanUseTool = func(ctx context.Context, request claude.PermissionRequest) (claude.PermissionDecision, error) {
    if request.ToolName == "Read" {
        return claude.PermissionDecision{Behavior: "allow"}, nil
    }
    return claude.PermissionDecision{Behavior: "deny", Message: "Only reads are allowed."}, nil
}
```

回调处理 CLI 转交宿主的审批请求；已被原生规则允许或拒绝的操作可能不进入回调。回调报错、panic、超时或返回非法结果不会批准操作。原生取消请求会取消回调 context，回调必须响应 context。请求 ID 通过 `PermissionRequest.RequestID` 或 `ControlRequestID(ctx)` 获取。

`Options.Hooks` 注册 Go 回调。输入 `HookInput.Raw` 和输出 `HookOutput` 使用原生协议。设置 `OnFailure: "block"` 会检查原生支持情况；无法确认时初始化失败。

外部 MCP 使用 `MCPConfig`，可搭配 `StrictMCPConfig`。进程内工具使用 `NewMCPServer`，通过官方 `github.com/modelcontextprotocol/go-sdk/mcp` 的 `AddTool` 注册后填入 `Options.SDKServers`；工具参数校验由该 MCP SDK 完成。`GetMCPStatus`、`ReconnectMCPServer`、`ToggleMCPServer` 支持基本连接管理。

进程内工具示例需导入 `github.com/modelcontextprotocol/go-sdk/mcp`：

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

原生连接限制仍适用，包括对 SDK 内嵌服务的 reconnect 限制。`Message.Raw`、`Request`、`ExtraArgs`、`InitializeOptions` 和 `Transport` 提供协议层访问，不表示支持所有进阶宿主流程。

## 取消、错误和生命周期

| API | 行为 |
| --- | --- |
| `Dial(ctx, ...)` 的 context | 控制整个进程生命周期 |
| `Receive(ctx)` / `Next(ctx)` 的 context | 只取消当前消息等待，可以重试读取 |
| `Request(ctx, ...)` 的 context | 取消等待并发送原生控制取消请求，不能撤销已完成操作 |
| `Interrupt` | 中断当前执行，保持客户端存活 |
| `SetModel` / `ResetModel` / `SetPermissionMode` | 修改运行中的模型或权限模式 |
| `Close` | 幂等，终止并回收进程 |
| `Wait` | 等待进程回收，返回退出或生命周期错误 |
| `Stderr` | 显式读取最近 32 KiB 的诊断输出 |

初始化和普通控制请求默认超时 60 秒；回调默认不增加宿主超时，仍遵循原生取消和 Hook 自身超时。Unix 按独立进程组清理，其他平台目前只终止直接子进程；默认关闭宽限时间为两秒。始终关闭客户端或流，父 Go 进程被强杀时无法保证自动清理。

默认单条消息上限 8 MiB，事件队列不设数量上限。正值 `EventBuffer` 可限制数量，溢出时报错并关闭，不静默丢弃消息。格式化错误不包含 stderr，诊断内容需要显式读取。

## 开发验证

```sh
go test -race ./...
go vet ./...
```

普通测试不调用模型。真实核心测试需要显式开启，会产生模型用量：

```sh
AGENTBRIDGE_LIVE_TEST=1 AGENTBRIDGE_CLAUDE_PATH=/path/to/claude \
  go test -tags=integration -run '^TestLive' -v .
```

完整示例见 [query](examples/query/main.go) 和 [session](examples/session/main.go)，开发流程见[贡献指南](../CONTRIBUTING.zh-CN.md)。

项目代码与文档采用 [MIT License](../LICENSE)，依赖和实现参考保留[第三方声明](THIRD_PARTY_NOTICES.md)。Claude Code 和模型服务另行适用各自条款。
