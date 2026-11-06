# Claude SDK for Go

通过本机 Claude Code CLI，提供 Go 风格的单次查询、流式响应和长驻多轮客户端。使用 Claude 原生 SDK 控制协议，不依赖 Python 或 TypeScript SDK。

此模块是非官方实现。兼容基准为 TypeScript SDK 0.3.318 / CLI 2.1.318，需要 Go 1.25+ 和可执行的 `claude`。通过 `Options.CLIPath` 指定 CLI 可执行文件或带 shebang 的脚本；该字段不接受 shell 命令字符串或 Windows `.cmd` 文件。SDK 不自动下载 CLI，不管理登录流程。

## 单次查询

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/MosesRen/agentbridge-go/claude"
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

`Run` 消费整个单次运行，包括后台 agent 引发的后续轮次，最后返回最终 `result`。失败结果同时返回非 nil 的 `*Result` 和 `*ResultError`。运行结束判定跟随 CLI session state 和 agent task 生命周期；长驻 shell/monitor 不阻止运行结束。需要自行安排多轮交互时使用 `Dial`。

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

`Options.Resume`、`Continue`、`SessionID` 和 `ForkSession` 配置新进程的会话启动方式；前三者互斥，`ForkSession` 搭配恢复或继续。独立的 `ListSessions`、`GetSessionInfo`、`GetSessionMessages`、`RenameSession`、`TagSession`、`DeleteSession`、`ForkSession` 以及子 Agent 查询函数用于管理已保存的会话。

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
| `SettingSources` | nil 省略参数；空但非 nil 切片显式传空；可启用 `user`、`project`、`local` |
| `Tools` | nil 保留 CLI 默认工具，空但非 nil 切片关闭全部内置工具 |
| `Env` | 在继承环境上覆盖；默认移除嵌套运行标记 `CLAUDECODE` |
| `MCPConfig` | 外部 MCP server 的原生 JSON 配置；可搭配 `StrictMCPConfig` |

Unix 上每个客户端建立独立进程组：关闭时先发 SIGTERM，默认等待 2 秒，再用 SIGKILL 清理进程组。其他平台目前只终止直接子进程，尚未实现 Windows Job Object。调用方必须 `Close()`；父 Go 进程被强杀时，没有退出钩子保证自动清理。

默认每条消息最多 8 MiB，事件队列不设数量上限。可显式设置正值 `EventBuffer`，超出容量时返回错误并关闭，不静默丢消息。使用单一逻辑消息消费者；控制请求与审批处理不依赖应用层消息消费。初始化和普通控制请求默认超时为 60 秒；回调默认仅受原生取消请求和 Hook 自身超时控制。

stderr 仅保留最后 32 KiB，可显式读取 `Client.Stderr()` 或 `ProcessError.Stderr`；格式化错误不会自动输出 stderr，避免意外泄露诊断中的敏感信息。

## Hooks、MCP 和 SessionStore

`Options.Hooks` 注册 Go 回调，输入的 `Raw` 和输出的 `HookOutput` 保留原生 Hook 协议。`OnFailure: "block"` 会检查 CLI 是否支持并已应用阻断语义；不支持时初始化报错。

通过 `NewMCPServer` 创建服务，在其 `Server` 上使用官方 `github.com/modelcontextprotocol/go-sdk/mcp` 的 `AddTool`、资源和 prompt API 注册 Go 函数，然后填入 `Options.SDKServers`。普通 MCP server 仍使用 `MCPConfig`。工具参数的 JSON Schema 验证由官方 MCP SDK 完成。

运行中可使用 `client.SetMCPServers(ctx, externalConfigs, sdkServers)` 替换动态配置。第三个参数是可选的 Go server map；省略则移除先前动态 SDK server。SDK 实例替换按 CLI 的移除/重新注册流程处理。原生 `ReadMCPResource` 控制只支持外部服务的 `ui://` 资源；SDK 内嵌服务由宿主直接访问，这是官方 CLI 的限制。

`Options.SessionStore` 支持外部 transcript 存储；提供了 `NewInMemorySessionStore()` 供开发测试。接入的 adapter 实现 `Append` 和 `Load`，可选实现列表、删除和子路径枚举接口。镜像写入失败会产生 `system/mirror_error`。从 store 恢复时，会在私有临时目录还原 transcript，并在进程结束后删除；复用本机认证时移除复制凭据中的 refresh token，避免子进程消费原账户的刷新令牌。

其他已暴露的上游配置包括 Agents、Skills、Plugins、Sandbox、Thinking/Effort、fallback model、系统提示词 preset/segments/snapshot 和文件 checkpointing。查看 [能力清单](COMPATIBILITY.md) 了解 API 对应关系、测试范围和差异。

## 验证记录

本机 macOS arm64、Go 1.27.3、Claude Code 2.1.318 下，已验证查询、partial streaming、多轮记忆、权限模式切换、中断、Go Hooks、进程内 MCP 工具调用和外部 SessionStore 恢复。另有官方 TypeScript SDK 与 Go SDK 共用模拟 CLI 的差异测试。审批和错误路径、后台轮次跟踪、并发控制、会话文件变换及进程回收由离线测试覆盖。

```sh
go test -race ./...
go vet ./...
```

## 参考

- [官方 SDK 概览](https://code.claude.com/docs/en/agent-sdk/overview)
- [官方流式输入说明](https://code.claude.com/docs/en/agent-sdk/streaming-vs-single-mode)
- [官方 TypeScript SDK 0.3.318](https://github.com/anthropics/claude-agent-sdk-typescript/releases/tag/v0.3.318)
