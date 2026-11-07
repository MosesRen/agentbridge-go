# Changelog / 变更记录

Changes are grouped by module. Unreleased entries describe the working development scope; they do not imply a published release. / 按模块记录变更；Unreleased 描述当前开发范围，不表示已经发版。

## Unreleased — claude

### Included / 当前能力

- Queries, streaming, persistent multi-turn input, and native session resume. / 查询、流式、长驻多轮输入与原生会话恢复。
- Common runtime configuration, permissions, Hooks, external/in-process MCP, and lifecycle controls. / 常用配置、权限、Hooks、外部/进程内 MCP 和生命周期控制。
- Offline/race tests, selected official-SDK protocol comparisons, and opt-in real core tests. / 离线与竞态测试、部分官方协议对照和可选真实核心测试。
- English/Chinese documentation, community files, MIT licensing, and retained third-party notices. / 中英文文档、社区文件、MIT 许可和第三方声明。

### Scope changes / 范围调整

- Removed advanced storage/history APIs, prewarming/claiming, independent settings resolution, checkpoint/rewind, and dynamic server-management implementations from the current core module. / 当前核心模块移除高级存储/历史 API、预热认领、独立配置解析、checkpoint/rewind 和动态服务管理实现。
- Those removed development APIs are not part of the core support commitment. Consumers of earlier development snapshots must review the capability matrix before updating. / 这些开发阶段 API 不纳入核心支持范围；使用旧开发快照的调用方升级前应核对能力清单。
