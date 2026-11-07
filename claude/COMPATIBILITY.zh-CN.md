# Claude 核心能力范围

[English](COMPATIBILITY.md) | 简体中文

本模块提供官方 Agent SDK 的核心使用路径，不追求全量 API 复刻。功能范围参考 Python SDK **0.2.178**，协议验证使用 Claude Code **2.1.318**；TypeScript SDK **0.3.318** 用于部分参数/初始化差异测试。核对日期：**2026-11-07**。

Go 运行时不依赖 Python 或 TypeScript SDK。CLI 安装、分发和升级方式不计入功能差异。

## 本期保留

| 核心能力 | Go API / 配置 | 验证方式 |
| --- | --- | --- |
| 单次查询、流式输出 | `Run`、`Query`、`Stream.Next`、`Stream.Result` | 模拟进程、真实 CLI |
| 长驻多轮和流式输入 | `Dial`、`Send`、`SendMessage`、`Receive`、`StreamInput` | 协议测试、真实多轮 |
| 常用运行配置 | 提示词、模型、工作目录、工具范围、预算、JSON Schema、Thinking/Effort | 参数边界、官方 TS 部分差异测试 |
| 常用扩展配置 | Agents、Skills、Plugins、Sandbox 的启动配置 | 参数/初始化测试；不包含运行中重载和插件专用交付机制 |
| 权限与 Hooks | `CanUseTool`、`Hooks`、取消传播、阻断能力检查 | 离线回调与错误路径、真实 Hooks |
| 外部与进程内 MCP | `MCPConfig`、`SDKServers`、`NewMCPServer`、基本连接管理 | 真实 Go MCP SDK schema/协议测试、真实工具调用 |
| 原生会话恢复 | `Resume`、`Continue`、`SessionID` | 启动参数测试、真实新进程恢复；历史持久化由 Claude Code 负责 |
| 中断与运行控制 | `Interrupt`、`SetModel`、`ResetModel`、`SetPermissionMode` | 控制关联、错误路径和真实控制调用 |
| 超时、错误和进程清理 | context、类型化错误、`Close`、`Wait`、`Stderr` | 生命周期、竞态和 Unix 进程组测试 |
| 原生消息/控制扩展 | `Message.Raw`、`Request`、`ExtraArgs`、`InitializeOptions`、`Transport` | JSON 保留与 transport 测试；不承诺扩展场景的完整宿主逻辑 |

单次查询仍保留原生 task/session-state 跟踪，避免在后台 Agent 需要继续运行时提前关闭输入。这是查询正确性的一部分，并不提供后台任务管理 API。

## 本期不提供

以下进阶实现已移出当前模块：

- SessionStore、会话导入、摘要索引和临时历史重建。
- 本地会话列表、历史读取、重命名、标签、删除、分叉和子 Agent 历史编辑。
- 文件 checkpoint、rewind 和文件读取控制。
- 独立配置解析、企业策略合并和设置文件写入。
- Startup/Prewarm、spare 认领和断线后重新初始化。
- 动态 MCP server 替换、插件/Skills/output styles 运行中重载。
- 专用 elicitation/user-dialog 回调、托管服务和浏览器连接适配器。
- 后台任务管理、异步队列管理和使用量专用查询控制。

原生配置可能使 Claude Code 自行使用其中某些能力；这不构成 Go SDK 对相应管理 API 或宿主回调的支持承诺。

## 验证与限制

核心路径应覆盖正常结果、错误、取消和退出清理。已有接口和已完成真实验证需区分：权限 callback 的细分审批场景、所有 Sandbox 配置组合和各平台行为尚未完整端到端验证。

Unix 进程组清理已有测试；Windows 当前仅终止直接子进程，本机验证为交叉编译，运行检查由 CI 执行。部分消息/响应保留为 `json.RawMessage`，不复制官方全部类型。

```sh
go test -race ./claude/...
go vet ./claude/...
```

真实测试会产生模型用量：

```sh
AGENTBRIDGE_LIVE_TEST=1 AGENTBRIDGE_CLAUDE_PATH=/path/to/claude-2.1.318 \
  go test -tags=integration -run '^TestLive' -v ./claude
```

开发时将官方 npm 包解包到临时目录，可运行不调用模型的部分参数/初始化对照：

```sh
AGENTBRIDGE_UPSTREAM_SDK=/path/to/package/sdk.mjs \
  go test -tags=integration -run '^TestUpstreamOptionsParity$' -v ./claude
```

参考：[Python SDK 0.2.178](https://github.com/anthropics/claude-agent-sdk-python/releases/tag/v0.2.178)、[Python API](https://code.claude.com/docs/en/agent-sdk/python)、[TypeScript SDK 0.3.318](https://github.com/anthropics/claude-agent-sdk-typescript/releases/tag/v0.3.318)。
