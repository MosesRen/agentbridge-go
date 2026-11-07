# 版本管理

[English](VERSIONING.md) | 简体中文

同一仓库内各 Go module 独立发版。Claude 模块路径为 `github.com/MosesRen/agentbridge-go/claude`，标签为 **`claude/vX.Y.Z`**，后续模块采用各自目录前缀。

- **patch**：兼容性修复，不改变 Go API。
- **minor**：增加功能；v1 之前的不兼容变更也升 minor，并提供迁移说明。
- **major**：v1 之后的不兼容变更升 major，并按 Go 规范修改 module/import 路径后缀。

Go 模块版本与上游 SDK/CLI 版本分别管理。能力参考版本和协议测试基准记录在模块的 `baseline.go`、`COMPATIBILITY.md` 中；参考版本不表示承诺全部 API 等价。

开发安装可以使用 `@main`；应用应固定解析出的伪版本或发布标签。普通提交、推送不自动创建发布或标签。

发布模块版本前：

1. 运行模块测试、竞态检查、静态检查和相关兼容对照。
2. 原生协议行为变化时运行真实测试，并记录服务/平台要求。
3. 同步中英文文档和变更记录。
4. 核对模块内第三方声明与许可证。根目录 `LICENSE` 适用于全部模块；模块根目录没有独立 `LICENSE` 时，Go 会将仓库根目录的文件放入子模块归档。
5. 仅在明确要求发版时创建带模块前缀的标签。

子模块许可证规则见 [Go Modules Reference](https://go.dev/ref/mod#vcs-license)。
