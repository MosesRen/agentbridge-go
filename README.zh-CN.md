# agentbridge-go

[![Go CI](https://github.com/MosesRen/agentbridge-go/actions/workflows/go.yml/badge.svg)](https://github.com/MosesRen/agentbridge-go/actions/workflows/go.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

[English](README.md) | 简体中文

为编程 Agent 提供 Go SDK，各模块保留原生 API，并独立管理版本。

首个模块 **`claude`** 聚焦 Claude Code 的核心调用流程：查询、流式输出、长驻多轮、权限、Hooks、MCP 工具、原生会话恢复和进程生命周期管理。它直接使用 Claude Code 原生 `stream-json` 协议，不自行实现 Agent 循环，也不直接调用模型 API。

这是**非官方、社区维护的项目**，与 Anthropic 无隶属关系。目标是覆盖核心能力，不承诺复刻官方 SDK 的全部 API。

## 模块

| 模块 | 导入路径 | 状态 |
| --- | --- | --- |
| Claude Code | `github.com/MosesRen/agentbridge-go/claude` | 核心实现，发布前阶段 |

各家 Agent 保留自己的协议、消息类型和生命周期，不强制实现统一的 Agent 接口。目前尚未实现其他 Agent 模块。

## 能力

| 能力 | 当前支持 |
| --- | --- |
| 单次查询与流式输出 | `Run`、`Query`、`Stream.Next`、`Stream.Result` |
| 长驻多轮与流式输入 | `Dial`、`Send`、`SendMessage`、`Receive`、`StreamInput` |
| 运行配置 | 模型、提示词、工具范围、工作目录、预算、JSON Schema 输出、Thinking/Effort |
| 权限与 Hooks | Go 回调、取消传播、阻断式 Hook 能力检查 |
| MCP | 外部服务和基于官方 Go MCP SDK 的进程内工具 |
| 原生会话恢复 | `Resume`、`Continue`、`SessionID` |
| 生命周期控制 | 中断、模型/权限切换、超时、类型化错误、进程清理 |

Agents、Skills、Plugins 和 Sandbox 保留常用启动配置。高级存储、历史编辑、checkpoint/rewind、预热认领和企业配置解析不在当前范围内，详见[能力与验证清单](claude/COMPATIBILITY.zh-CN.md)。

## 环境与安装

- Go **1.25+**。
- 自行安装并配置 Claude Code 认证；将 `claude` 加入 `PATH`，或指定 `Options.CLIPath`。
- 运行时不依赖 Python 或 TypeScript SDK。Node.js 只用于可选的上游对照测试。

在你的 Go 应用中添加模块：

```sh
go get github.com/MosesRen/agentbridge-go/claude@main
```

`@main` 获取开发快照。需要可复现构建时，固定解析得到的伪版本或已发布标签。发布前阶段的 API 可能调整。

## 快速开始

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
    if err := run(); err != nil {
        log.Fatal(err)
    }
}

func run() error {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    result, err := claude.Run(ctx, "简要说明这个仓库的用途。", claude.Options{
        Dir:            ".",
        SettingSources: []string{"user"},
        MaxTurns:       10,
    })
    if err != nil {
        return err
    }
    fmt.Println(result.Result)
    return nil
}
```

流式、MCP、权限和会话恢复用法见 [Claude 模块文档](claude/README.zh-CN.md)。完整示例见 [query](claude/examples/query/main.go) 和 [session](claude/examples/session/main.go)。

## 开发

在仓库根目录执行：

```sh
go test -race ./claude/...
go vet ./claude/...
```

这些检查不调用模型。真实测试需要显式开启并消耗模型用量，见[贡献指南](CONTRIBUTING.zh-CN.md)。CI 在 Linux、macOS、Windows 上执行普通检查。

各模块独立发布，Claude 模块使用 `claude/vX.Y.Z` 标签，上游 SDK/CLI 版本单独记录。详见[版本策略](VERSIONING.zh-CN.md)和[未发布变更](CHANGELOG.md)。

## 社区

- [反馈问题或提出需求](https://github.com/MosesRen/agentbridge-go/issues)。
- [参与贡献](CONTRIBUTING.zh-CN.md)，欢迎中文或英文。
- [报告安全问题](SECURITY.md)。
- 遵守[行为准则](CODE_OF_CONDUCT.md)。

## 许可证与第三方软件

本项目代码和文档采用 [MIT License](LICENSE)，允许使用、修改和再分发，包括商用，须遵守其声明保留要求。

依赖保留各自许可证，见[第三方声明](THIRD_PARTY_NOTICES.md)。Claude Code 和模型服务是独立产品，适用各自条款。本仓库不分发 CLI 或官方 TypeScript SDK。
