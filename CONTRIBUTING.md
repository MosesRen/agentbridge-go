# Contributing

English | [简体中文](CONTRIBUTING.zh-CN.md)

Bug reports, documentation improvements, and focused fixes are welcome in English or Chinese. Follow the [code of conduct](CODE_OF_CONDUCT.md).

## Scope

The current goal is a reliable core Claude SDK: querying, streaming, multi-turn conversations, configuration, permissions, Hooks, MCP, resume, and lifecycle management. Check the [capability matrix](claude/COMPATIBILITY.md) before proposing an API. Open an issue before starting a new agent module or an advanced subsystem so its scope can be agreed on.

Keep each agent's native API and protocol. Do not introduce a shared Agent abstraction, install/update a CLI automatically, or add a Python/TypeScript runtime dependency to the Go SDK.

## Local setup

Use Go 1.25+. Clone the repository and work from its root; `go.work` includes `./claude`.

```sh
git clone https://github.com/MosesRen/agentbridge-go.git
cd agentbridge-go
go test -race ./claude/...
go vet ./claude/...
```

Ordinary tests simulate the protocol and do not require Claude Code, authentication, Node.js, or model usage. Format changed Go files with `gofmt`.

## Optional integration checks

Real tests need a separately installed, authenticated Claude Code executable and network/proxy access. They consume model usage:

```sh
AGENTBRIDGE_LIVE_TEST=1 AGENTBRIDGE_CLAUDE_PATH=/path/to/claude \
  go test -tags=integration -run '^TestLive' -v ./claude
```

Use temporary working directories and sanitized reproducers. Do not commit credentials, private transcripts, or model-service configuration.

Selected argument/initialization comparisons use Node.js and a separately unpacked official TypeScript SDK 0.3.318, with a shared fake CLI and no model calls:

```sh
AGENTBRIDGE_UPSTREAM_SDK=/path/to/package/sdk.mjs \
  go test -tags=integration -run '^TestUpstreamOptionsParity$' -v ./claude
```

Keep official SDK packages and CLI binaries outside the repository. They are development references, not vendored runtime dependencies.

## Pull requests

Describe the concrete problem, resulting behavior, and validation. Add a regression test for a meaningful behavior change; documentation-only edits do not need model calls. State any unverified behavior and platform limits.

Update both language versions when capabilities, examples, defaults, or requirements change. API references should match the code. Avoid adding features solely to claim complete upstream parity.

Modules are versioned independently; see [VERSIONING](VERSIONING.md). Do not create tags or releases as part of an ordinary contribution.

## Licensing

By submitting a contribution, you agree to license your original contribution under this project's [MIT License](LICENSE). Include required attribution/license notices for any third-party material and identify it in the pull request. No separate CLA is currently required.
