# Claude SDK for Go

通过本机 Claude Code CLI，提供 Go 风格的单次查询、流式响应和长驻多轮客户端。使用 Claude 原生 SDK 控制协议，不依赖 Python 或 TypeScript SDK。

此模块是非官方实现。需要 Go 1.23+ 和可执行的 `claude`；也可通过 `Options.CLIPath` 指定 CLI 可执行文件或带 shebang 的启动脚本。该字段不接受 shell 命令字符串，也不直接执行 Windows `.cmd` 文件。SDK 不下载 CLI，不管理登录和凭据。

## 单次查询

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "agentbridge-go/claude"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    result, err := claude.Run(ctx, "Explain the repository architecture.", claude.Options{
        Dir: ".",
        SettingSources: []string{"user"},
        MaxTurns: 10,
    })
    if err != nil { log.Fatal(err) }
    fmt.Println(result.Result)
}
```

`Run` 消费消息直到第一个前台 `result`，随后关闭进程。失败结果会同时返回非 nil 的 `*Result` 和 `*ResultError`。如果任务会启动后台 agent，并需要继续接收其后续事件，应使用 `Dial`，自行决定结束时间。

## 流式查询

```go
stream, err := claude.Query(ctx, "Review the current changes.", options)
if err != nil { return err }
defer stream.Close()

for {
    message, err := stream.Next(ctx)
    if errors.Is(err, io.EOF) { break }
    if err != nil { return err }

    if message.Type == "assistant" {
        fmt.Print(message.Text())
    }
    if message.Type == "result" { break }
}
result, err := stream.Result(ctx)
```

`Message.Text()` 读取完整的 user/assistant 文本块，排除 thinking 和 tool_result。逐 token 输出请设置 `IncludePartialMessages: true`，消费 `stream_event` 的 `Message.Event`；不要将完整 assistant 消息与 token delta 重复拼接。

所有消息都保留原生 `Type`、`Subtype`、`SessionID` 等字段；`Raw` 包含完整 JSON，未知事件类型也会透传。结构化结果位于 `Result.StructuredOutput`，可搭配 `Options.JSONSchema` 使用。图像等输入可通过 `SendMessage` 传入原生 content blocks。

## 长驻多轮会话

```go
client, err := claude.Dial(ctx, options)
if err != nil { return err }
defer client.Close()

if err := client.Send(ctx, "Find the likely cause of the failure."); err != nil {
    return err
}
for {
    message, err := client.Receive(ctx)
    if err != nil { return err }
    if message.Type == "result" {
        if _, err := message.Result(); err != nil { return err }
        break
    }
}
// 同一个进程里发送下一轮，继续消费至对应的 result。
err = client.Send(ctx, "Now suggest a fix.")
```

控制方法包括 `Interrupt`、`SetModel`、`SetPermissionMode`。`Request` 可以发送包含 `subtype` 的原生控制对象；响应按 request ID 关联，允许多个并发控制请求。`Initialization()` 返回握手时的原始能力信息。

`Options.Resume`、`Continue`、`SessionID` 和 `ForkSession` 用于新进程的会话启动方式；`Resume`、`Continue`、`SessionID` 三者互斥，`ForkSession` 必须搭配恢复或继续。SDK 不自行操作磁盘上的会话文件。

## 权限回调

```go
options.CanUseTool = func(ctx context.Context, req claude.PermissionRequest) (claude.PermissionDecision, error) {
    if req.ToolName == "Read" {
        return claude.PermissionDecision{Behavior: "allow"}, nil
    }
    return claude.PermissionDecision{
        Behavior: "deny",
        Message: "This application only approves reads.",
    }, nil
}
```

回调仅处理 CLI 转交宿主的审批请求；CLI 已允许或拒绝的操作可能不进入回调。需要限制工具集合时，同时配置 `Tools`、`AllowedTools`、`DisallowedTools` 和原生权限模式。

没有回调时，宿主收到的审批请求默认拒绝。`allow` 未指定 `UpdatedInput` 时保留原输入。回调报错、panic、超时或返回未知行为时，不会批准操作。回调可能并发执行，必须响应其 context；服务端撤销请求会取消对应的回调 context。

## 配置和进程生命周期

| 配置/API | 行为 |
| --- | --- |
| `Dial(ctx, ...)` 的 ctx | 控制整个子进程的生命周期；取消后终止并回收进程 |
| `Receive(ctx)` 的 ctx | 只取消这一次消息等待，可继续调用 |
| `Request(ctx, ...)` 的 ctx | 只取消等待，不撤销已经发送的控制操作 |
| `Interrupt(ctx)` | 请求中断当前执行，进程继续存活 |
| `Close()` | 幂等；终止并回收 SDK 创建的进程 |
| `Wait(ctx)` | 等待进程回收，返回进程错误或生命周期取消原因 |
| `SettingSources` | 默认空；可显式启用 `user`、`project`、`local` |
| `Tools` | nil 保留 CLI 默认工具，空但非 nil 切片关闭全部内置工具 |
| `Env` | 在继承环境上覆盖；默认移除嵌套运行标记 `CLAUDECODE` |
| `MCPConfig` | 外部 MCP server 的原生 JSON 配置；可搭配 `StrictMCPConfig` |

Unix 上每个客户端建立独立进程组：关闭时先发 SIGTERM，默认等待 2 秒，再用 SIGKILL 清理进程组。其他平台目前只终止直接子进程，尚未实现 Windows Job Object。调用方必须 `Close()`；父 Go 进程被强杀时，没有退出钩子保证自动清理。

默认每条消息最多 8 MiB，事件队列容量为 256。超出大小或队列容量时客户端返回错误并关闭，不静默丢消息。增加 `MaxMessageBytes` / `EventBuffer` 可调整限制。使用单一逻辑消息消费者；控制请求与审批处理不依赖应用层消息消费。

stderr 仅保留最后 32 KiB，可显式读取 `Client.Stderr()` 或 `ProcessError.Stderr`；格式化错误不会自动输出 stderr，避免意外泄露诊断中的敏感信息。

## 第一版范围

已实现：查询和多轮会话、原生流式事件、审批回调、恢复/分叉参数、结构化与多模态消息输入、外部 MCP 配置、模型/权限控制、中断、进程回收。

暂未实现：Go hooks 注册、进程内 MCP 工具注册、SDK 会话文件管理、后台任务自动完成判定、CLI 自动安装、Windows 进程树回收。原生控制协议不是稳定的公共 HTTP API，需要跟随 Claude CLI 版本维护。

## 验证记录

本机 macOS arm64、Go 1.27.3、Claude Code 2.1.158 下，已使用现有用户配置验证单次查询、partial streaming、多轮上下文记忆、权限模式切换及中断控制。审批回调、错误路径、超时、并发控制请求和 Unix 进程树清理由离线子进程测试覆盖。

```sh
go test -race ./...
go vet ./...
```

## 参考

- [官方 SDK 概览](https://code.claude.com/docs/en/agent-sdk/overview)
- [官方流式输入说明](https://code.claude.com/docs/en/agent-sdk/streaming-vs-single-mode)
- [官方 Python 子进程 transport 实现](https://github.com/anthropics/claude-agent-sdk-python/blob/main/src/claude_agent_sdk/_internal/transport/subprocess_cli.py)
- [官方 Python 控制协议实现](https://github.com/anthropics/claude-agent-sdk-python/blob/main/src/claude_agent_sdk/_internal/query.py)
