# agentbridge-go

Go SDKs for coding agents — native APIs, independent modules.

目前实现了 `claude` 模块，通过 Claude Code CLI 的双向 `stream-json` 协议运行 agent。以官方 TypeScript SDK **0.3.318** / CLI **2.1.318** 为兼容基准；进程内 MCP 使用官方 Go MCP SDK。

```text
agentbridge-go/
├── claude/       # 独立 Go module：github.com/MosesRen/agentbridge-go/claude
└── go.work       # 本地开发工作区
```

各模块独立发版，Claude 模块的发布标签使用 `claude/vX.Y.Z`。

## 开始使用

需要 Go 1.25+，以及已安装、已配置认证的 Claude Code CLI。

```sh
cd claude
go run ./examples/query 'Explain the purpose of this directory briefly.'
```

示例显式加载用户级 Claude 设置。`SettingSources: nil` 跟随上游省略该启动参数；空但非 nil 的切片显式禁用这些设置来源。

详见 [Claude 模块文档](claude/README.md)。

版本策略见 [VERSIONING.md](VERSIONING.md)，能力对应关系见 [claude/COMPATIBILITY.md](claude/COMPATIBILITY.md)。

## 本地验证

在仓库根目录执行：

```sh
go test -race ./claude/...
go vet ./claude/...
```

普通测试不访问模型服务，覆盖模拟 CLI、进程内 MCP 和临时会话文件。真实 CLI 测试需要显式开启，会产生模型用量：

```sh
AGENTBRIDGE_LIVE_TEST=1 go test -tags=integration -run '^TestLiveCLI$' -v ./claude
```

各家 SDK 将保留自己的协议、消息和生命周期语义。后续模块按需添加，不要求实现共同的 Agent 接口。
