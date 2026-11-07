# agentbridge-go

[![Go CI](https://github.com/MosesRen/agentbridge-go/actions/workflows/go.yml/badge.svg)](https://github.com/MosesRen/agentbridge-go/actions/workflows/go.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

English | [简体中文](README.zh-CN.md)

Go SDKs for coding agents, with native APIs and independently versioned modules.

The first module, **`claude`**, provides the core Claude Code workflow: queries, streaming, persistent conversations, permissions, Hooks, MCP tools, native session resume, and process lifecycle management. It speaks Claude Code's native `stream-json` protocol. It does not implement its own agent loop or call a model API directly.

This is an **unofficial, community-maintained project**, not affiliated with Anthropic. Core capability coverage is the goal; complete API parity with the official SDKs is not.

## Modules

| Module | Import path | Status |
| --- | --- | --- |
| Claude Code | `github.com/MosesRen/agentbridge-go/claude` | Core implementation; pre-release |

Each agent keeps its own protocol, message types, and lifecycle. No common Agent interface is required. Other agent modules are not implemented yet.

## Capabilities

| Capability | Included |
| --- | --- |
| Single queries and streamed output | `Run`, `Query`, `Stream.Next`, `Stream.Result` |
| Persistent conversations and streamed input | `Dial`, `Send`, `SendMessage`, `Receive`, `StreamInput` |
| Runtime configuration | Models, prompts, tools, working directories, budgets, JSON Schema output, Thinking/Effort |
| Permissions and Hooks | Go callbacks, cancellation propagation, blocking-hook capability checks |
| MCP | External servers and in-process tools using the official Go MCP SDK |
| Native session resume | `Resume`, `Continue`, `SessionID` |
| Lifecycle controls | Interrupt, model/permission changes, timeouts, typed errors, process cleanup |

Agents, Skills, Plugins, and Sandbox have common startup configuration support. Advanced storage, transcript editing, checkpoint/rewind, prewarming, and enterprise settings resolution are outside the current scope. See the [capability and validation matrix](claude/COMPATIBILITY.md).

## Requirements and installation

- Go **1.25+**.
- Claude Code installed and authenticated separately; `claude` must be on `PATH`, or set `Options.CLIPath`.
- No Python or TypeScript SDK is required at runtime. Node.js is only used by optional upstream comparison tests.

In your Go application, add the module:

```sh
go get github.com/MosesRen/agentbridge-go/claude@main
```

`@main` selects a development snapshot. Pin the resulting pseudo-version or an available release tag for reproducible builds. Pre-release APIs may change.

## Quick start

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

    result, err := claude.Run(ctx, "Explain this repository briefly.", claude.Options{
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

For streaming, MCP tools, permissions, and session resume, see the [Claude module guide](claude/README.md). Runnable examples are in [query](claude/examples/query/main.go) and [session](claude/examples/session/main.go).

## Development

From the repository root:

```sh
go test -race ./claude/...
go vet ./claude/...
```

These checks do not call a model. Live tests are opt-in and consume model usage; see [CONTRIBUTING](CONTRIBUTING.md). CI runs the ordinary checks on Linux, macOS, and Windows.

Each module is released separately. Claude tags use `claude/vX.Y.Z`; upstream SDK/CLI versions are recorded independently. See [versioning](VERSIONING.md) and [unreleased changes](CHANGELOG.md).

## Community

- [Report a bug or request a feature](https://github.com/MosesRen/agentbridge-go/issues).
- [Contribute](CONTRIBUTING.md); English and Chinese are welcome.
- [Report a security issue](SECURITY.md).
- Follow the [code of conduct](CODE_OF_CONDUCT.md).

## License and third-party software

Project code and documentation are licensed under the [MIT License](LICENSE). You may use, modify, and redistribute them, including commercially, subject to its notice requirements.

Dependencies retain their own licenses; see [third-party notices](THIRD_PARTY_NOTICES.md). Claude Code and the model services you use are separate products governed by their own terms. This repository does not distribute the CLI or the official TypeScript SDK.
