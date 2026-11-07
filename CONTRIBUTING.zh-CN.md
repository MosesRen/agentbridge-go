# 贡献指南

[English](CONTRIBUTING.md) | 简体中文

欢迎使用中文或英文提交问题、改进文档和修复缺陷。请遵守[行为准则](CODE_OF_CONDUCT.md)。

## 范围

当前目标是可靠的 Claude 核心 SDK：查询、流式、多轮、配置、权限、Hooks、MCP、会话恢复和生命周期管理。新增 API 前先查看[能力清单](claude/COMPATIBILITY.zh-CN.md)。新增 Agent 模块或进阶子系统前，请先用 Issue 讨论范围。

保留各家 Agent 的原生 API 和协议，不引入统一 Agent 抽象，不自动安装/更新 CLI，不给 Go SDK 增加 Python/TypeScript 运行时依赖。

## 本地开发

使用 Go 1.25+。克隆后在仓库根目录工作，`go.work` 已包含 `./claude`。

```sh
git clone https://github.com/MosesRen/agentbridge-go.git
cd agentbridge-go
go test -race ./claude/...
go vet ./claude/...
```

普通测试使用模拟协议，不需要 Claude Code、认证、Node.js，也不消耗模型用量。修改 Go 文件后执行 `gofmt`。

## 可选集成验证

真实测试需要自行安装并配置认证的 Claude Code，以及网络/代理访问权限，会消耗模型用量：

```sh
AGENTBRIDGE_LIVE_TEST=1 AGENTBRIDGE_CLAUDE_PATH=/path/to/claude \
  go test -tags=integration -run '^TestLive' -v ./claude
```

使用临时工作目录和脱敏复现材料，不要提交凭据、私人会话或模型服务配置。

部分参数/初始化对照需要 Node.js 和自行解包的官方 TypeScript SDK 0.3.318，使用同一模拟 CLI，不调用模型：

```sh
AGENTBRIDGE_UPSTREAM_SDK=/path/to/package/sdk.mjs \
  go test -tags=integration -run '^TestUpstreamOptionsParity$' -v ./claude
```

官方 SDK 包和 CLI 二进制放在仓库之外；它们是开发参考，不是随项目分发的运行时依赖。

## Pull Request

说明具体问题、修改后的行为和验证结果。对有意义的行为变化增加回归测试；纯文档修改无需调用模型。明确尚未验证的行为和平台限制。

能力、示例、默认值或环境要求变化时，同步更新中英文文档。API 描述须与代码一致，不要仅为声称“完全对齐”而增加功能。

模块独立发版，见[版本策略](VERSIONING.zh-CN.md)。普通贡献不要创建标签或发布版本。

## 许可证

提交贡献即表示同意将自己的原创贡献按项目 [MIT License](LICENSE) 许可。引入第三方材料时保留所需版权/许可证声明，并在 PR 中说明。目前不要求另签 CLA。
