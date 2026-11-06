# agentbridge-go

Go SDKs for coding agents — native APIs, independent modules.

目前实现了 `claude` 模块，通过 Claude Code CLI 的双向 `stream-json` 协议运行 agent。标准库实现，无第三方 Go 依赖。

```text
agentbridge-go/
├── claude/       # 独立 Go module：agentbridge-go/claude
└── go.work       # 本地开发工作区
```

仓库尚未发布。`agentbridge-go/claude` 是当前的本地 module 路径；确定 Git 托管地址后，再替换 module 和示例 import 路径。

## 开始使用

需要 Go 1.23+，以及已安装、已配置认证的 Claude Code CLI。

```sh
cd claude
go run ./examples/query 'Explain the purpose of this directory briefly.'
```

示例加载用户级 Claude 设置以使用本机认证配置。SDK 默认不加载用户、项目和本地设置；需要时显式设置 `SettingSources`。

详见 [Claude 模块文档](claude/README.md)。

## 本地验证

在仓库根目录执行：

```sh
go test -race ./claude/...
go vet ./claude/...
```

普通测试仅启动模拟 CLI 子进程，不访问模型服务。真实 CLI 测试需要显式开启，会产生模型用量：

```sh
AGENTBRIDGE_LIVE_TEST=1 go test -tags=integration -run '^TestLiveCLI$' -v ./claude
```

各家 SDK 将保留自己的协议、消息和生命周期语义。后续模块按需添加，不要求实现共同的 Agent 接口。
