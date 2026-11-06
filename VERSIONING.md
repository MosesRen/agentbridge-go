# 版本管理

一个仓库、每个 Go module 独立发版。Claude 的 module 路径是 `github.com/MosesRen/agentbridge-go/claude`，发布标签使用 `claude/vX.Y.Z`，后续其他 agent 使用各自的模块前缀。

- patch：兼容性修复，不改变 Go API。
- minor：增加功能；在 v0 阶段，不兼容变更也升 minor，并写明迁移方式。
- v1 之后的不兼容变更：升级 major，并按 Go 规范变更 module/import 路径。

Go module 版本和上游 SDK/CLI 版本分开记录。上游兼容基准放在各模块的 `baseline.go` 和 `COMPATIBILITY.md`，不把上游版本直接当作本项目版本。

发布前执行模块测试、竞态检测、静态检查和所涉及的兼容测试。仅在明确发布时创建模块标签；普通提交不自动发版。
