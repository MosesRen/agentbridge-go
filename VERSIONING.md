# Versioning

English | [简体中文](VERSIONING.zh-CN.md)

Each Go module is versioned independently in this repository. The Claude module is `github.com/MosesRen/agentbridge-go/claude`, with release tags named **`claude/vX.Y.Z`**. Future modules use their own directory prefixes.

- **Patch:** compatible fixes, without Go API changes.
- **Minor:** added functionality. Before v1, breaking changes also require a minor increment and migration notes.
- **Major:** after v1, breaking changes require a new major version and the Go module/import-path suffix required by Go.

Go module versions are independent of upstream SDK and CLI versions. Capability references and protocol-test baselines are recorded in each module's `baseline.go` and `COMPATIBILITY.md`. A reference version does not promise complete API parity.

Development installs may use `@main`; applications should pin the resolved pseudo-version or a release tag. A commit/push does not automatically create a release or tag.

Before publishing a module release:

1. Run the relevant module tests, race checks, static checks, and compatibility comparisons.
2. Run live tests when native protocol behavior changed; record their service/platform requirements.
3. Update both language versions of the affected documentation and the changelog.
4. Check the module's third-party notices and license files. The root `LICENSE` applies to all modules; Go includes it in nested-module archives when no module-local `LICENSE` exists.
5. Create a module-prefixed tag only when a release is explicitly requested.

See the [Go module reference](https://go.dev/ref/mod#vcs-license) for nested-module license handling.
