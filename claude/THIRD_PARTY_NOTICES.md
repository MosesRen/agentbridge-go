# Claude third-party notices / Claude 第三方声明

Project code and documentation use the repository's [MIT License](../LICENSE). The following components retain their original terms. License copies are included in this module under [third_party/licenses](third_party/licenses). / 项目代码和文档采用仓库 MIT 许可证，下列组件保留原始条款，许可证副本位于模块内 `third_party/licenses`。

## Go dependencies / Go 依赖

Versions correspond to the current `go.mod`; this is an inventory of declared dependencies, not a claim that every package is linked into every consumer binary. / 版本对应当前 `go.mod`，下表列出声明依赖，不表示每个调用方二进制都会链接其中全部包。

| Module | Version | License notice |
| --- | --- | --- |
| `github.com/modelcontextprotocol/go-sdk` | v1.8.0 | [Mixed Apache-2.0/MIT code; upstream notice also describes its documentation license](third_party/licenses/github.com-modelcontextprotocol-go-sdk-LICENSE) |
| `github.com/google/jsonschema-go` | v0.4.3 | [MIT](third_party/licenses/github.com-google-jsonschema-go-LICENSE) |
| `github.com/segmentio/asm` | v1.1.3 | [MIT](third_party/licenses/github.com-segmentio-asm-LICENSE) |
| `github.com/segmentio/encoding` | v0.5.4 | [MIT](third_party/licenses/github.com-segmentio-encoding-LICENSE) |
| `github.com/yosida95/uritemplate/v3` | v3.0.2 | [BSD-3-Clause](third_party/licenses/github.com-yosida95-uritemplate-v3-LICENSE) |
| `golang.org/x/oauth2` | v0.35.0 | [BSD-3-Clause](third_party/licenses/golang.org-x-oauth2-LICENSE) |
| `golang.org/x/sync` | v0.20.0 | [BSD-3-Clause](third_party/licenses/golang.org-x-sync-LICENSE), [patent notice](third_party/licenses/golang.org-x-sync-PATENTS) |
| `golang.org/x/sys` | v0.41.0 | [BSD-3-Clause](third_party/licenses/golang.org-x-sys-LICENSE), [patent notice](third_party/licenses/golang.org-x-sys-PATENTS) |
| `golang.org/x/time` | v0.15.0 | [BSD-3-Clause](third_party/licenses/golang.org-x-time-LICENSE), [patent notice](third_party/licenses/golang.org-x-time-PATENTS) |

The MCP dependency contains contributions governed by different licenses during its licensing transition. This is not a blanket choice between MIT and Apache-2.0 for all of that dependency's code; preserve its full upstream notice. / MCP 依赖在许可证过渡期间包含不同许可的贡献，不能将其理解为全部代码可统一任选 MIT 或 Apache-2.0，应保留完整上游声明。

## Implementation references / 实现参考

Claude Agent SDK for Python **0.2.178**, copyright (c) 2025 Anthropic, PBC, is an implementation reference for native protocol behavior. Its [MIT copyright and permission notice](third_party/licenses/claude-agent-sdk-python-LICENSE) is retained here. It is not bundled as executable Python code and is not a runtime dependency. / 官方 Python SDK 是原生协议行为的实现参考，此处保留其 MIT 版权与许可声明，不分发可执行 Python 代码，也不作为运行时依赖。

The official TypeScript SDK **0.3.318** may be supplied separately by a developer for optional comparison tests. This repository does not contain or relicense that package or its CLI binaries. Its [upstream terms](https://github.com/anthropics/claude-agent-sdk-typescript/blob/main/LICENSE.md) remain applicable to that separately obtained software. / 官方 TypeScript SDK 可由开发者单独提供用于可选对照测试，本仓库不包含或重新许可该包及其 CLI 二进制，单独取得的软件仍适用上游条款。

## Redistribution / 再分发

Retain applicable third-party copyright, license, and patent notices when distributing software that includes those components. Update this inventory and the license copies when dependencies change. / 再分发包含这些组件的软件时，保留适用的第三方版权、许可证和专利声明；依赖变化时同步更新本清单及副本。

This inventory does not license Claude Code, third-party trademarks, or model-service access. / 本清单不授予 Claude Code、第三方商标或模型服务访问权。
