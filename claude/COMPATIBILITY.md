# Claude SDK 能力对应

基准：官方 TypeScript SDK **0.3.318**，随 SDK 发布的 CLI **2.1.318**，核对日期 **2026-11-06**。

Go 运行时不依赖 Python 或 TypeScript SDK。官方 SDK 只用于开发时的接口/协议对照。

| 官方能力 | Go 对应入口 | 验证 |
| --- | --- | --- |
| query / 流式和多轮输入 | `Query`、`Run`、`Dial`、`SendMessage`、`StreamInput` | 离线、真实 CLI |
| 系统提示词、模型、预算、工具、Skills、Agents | `Options`；提示词和 Agents 随 initialize 发送 | 官方 TS 参数/初始化差异测试 |
| 后台 continuation / session state | 自动跟踪 task 和 session state，读到最终退出 | 离线状态序列测试 |
| 权限回调和取消 | `CanUseTool`、原生控制请求取消 | 离线 |
| Hooks | `Hooks`、`HookMatcher`、`HookCallback` | 离线、真实 CLI |
| 进程内 MCP 工具、资源和 prompts | `SDKServers` + 官方 Go MCP SDK | schema/协议测试、真实工具调用 |
| 外部 MCP、重连、启停和动态配置 | `MCPConfig`、`GetMCPStatus`、`ReconnectMCPServer`、`ToggleMCPServer`、`SetMCPServers` | 动态 Go server 新增/替换/删除已实测，失败回滚有离线测试 |
| elicitation / user dialogs | `OnElicitation`、`OnUserDialog`、`SupportedDialogKinds` | 原生协议接口，尚未真实服务联调 |
| interrupt receipt / 取消队列消息 | `InterruptWithOptions`、`CancelAsyncMessage` | 基础 interrupt 已实测；队列 receipt 尚无全面实测 |
| 模型、权限、thinking 和 settings 控制 | `SetModel`、`ResetModel`、`SetPermissionMode`、`SetMaxThinkingTokens`、`ApplyFlagSettings`、`UpdateSettings` | 部分真实控制实测；文件写入方法不在实测中调用 |
| 支持的 commands/models/agents/account | `InitializationResult` 及对应访问方法、`Reinitialize` | 初始化协议 |
| context usage / 文件读取 / MCP resource / reload / 后台任务 | `GetContextUsage`、`ReadFile`、`ReadMCPResource`、`ReloadPlugins`、`ReloadSkills`、`ReloadOutputStyles`、`StopTask`、`BackgroundTasks` | 原生控制接口 |
| checkpoint / rewind | `EnableFileCheckpointing`、`RewindFiles` | 参数和控制接口，尚未真实文件回滚实测 |
| 会话列表、历史、标题、标签、删除、分叉、子 Agent 历史 | `sessions.go` 中的独立函数 | 离线文件/store 测试 |
| 外部 SessionStore / 恢复 | `SessionStore`、`InMemorySessionStore` | 离线、真实 CLI 镜像和恢复 |
| 自定义 transport | `Options.Transport` | 离线 transport 与生命周期测试 |
| 新增消息和控制字段 | `Message.Raw`、`Request`、`ExtraArgs`、`InitializeOptions` | 透传保留，不替代需要宿主执行的回调 |

## 已知差异

- 官方 SDK 随包携带 CLI；Go 模块使用本机 CLI 或显式 `CLIPath`，不自动安装/升级 CLI。
- JS/Bun 模块预热、浏览器端和官方托管服务的专用 transport 没有直接移植；Go 的 `Dial` 可预连接，其他 transport 由调用方提供。
- 消息保留完整 JSON；部分变化较快的控制响应和 event payload 使用 `json.RawMessage`，没有复制官方全部 TypeScript 类型定义。
- 会话文件逻辑并非官方源码逐行翻译，复杂分支/压缩/并行工具历史尚未完成全面差异测试；不能宣称所有 transcript 边界情况完全一致。
- SessionStore 采用逐帧写入，尚未提供官方的批量 flush/summary sidecar 优化 API。
- Unix 进程组清理已测试。Windows 只终止直接 CLI 进程；Windows 运行验证依赖 CI，当前本机仅能交叉编译。

## 复现验证

仓库根目录：

```sh
go test -race ./claude/...
go vet ./claude/...
```

使用匹配 CLI 和已配置的模型服务（会消耗模型用量）：

```sh
AGENTBRIDGE_LIVE_TEST=1 AGENTBRIDGE_CLAUDE_PATH=/path/to/claude-2.1.318 \
  go test -tags=integration -run '^TestLive' -v ./claude
```

将官方 npm 包 `@anthropic-ai/claude-agent-sdk@0.3.318` 解包到临时目录后，可运行不调用模型的差异测试：

```sh
AGENTBRIDGE_UPSTREAM_SDK=/path/to/package/sdk.mjs \
  go test -tags=integration -run '^TestUpstreamOptionsParity$' -v ./claude
```

参考：[官方发布记录](https://github.com/anthropics/claude-agent-sdk-typescript/releases/tag/v0.3.318)、[官方 SDK 参考](https://code.claude.com/docs/en/agent-sdk/typescript)。
