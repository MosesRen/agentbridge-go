# Claude capability and validation matrix

English | [简体中文](COMPATIBILITY.zh-CN.md)

This module covers core official-SDK workflows, not the complete API surface. The capability reference is Python SDK **0.2.178**. Native protocol validation uses Claude Code **2.1.318**; TypeScript SDK **0.3.318** is used for selected argument/initialization comparisons. Reviewed **2026-11-07**.

The Go runtime requires neither official language SDK. CLI installation, distribution, and upgrade mechanisms are outside the feature comparison.

## Included core features

| Capability | Go API / options | Validation |
| --- | --- | --- |
| Queries and streamed output | `Run`, `Query`, `Stream.Next`, `Stream.Result` | Simulated process and real CLI |
| Persistent conversations and streamed input | `Dial`, `Send`, `SendMessage`, `Receive`, `StreamInput` | Protocol tests and real multi-turn conversations |
| Runtime configuration | Prompts, models, directories, tools, budgets, JSON Schema, Thinking/Effort | Argument boundaries and selected official-TS comparisons |
| Common extensions | Agents, Skills, Plugins, Sandbox startup options | Argument/initialization tests; no runtime reload or special plugin-delivery implementation |
| Permissions and Hooks | `CanUseTool`, `Hooks`, cancellation, blocking-capability checks | Offline error/callback tests and real Hooks |
| External and in-process MCP | `MCPConfig`, `SDKServers`, `NewMCPServer`, connection controls | Official Go MCP schema/protocol tests and real tool calls |
| Native session resume | `Resume`, `Continue`, `SessionID` | Argument tests and real new-process resume; Claude Code owns history persistence |
| Execution controls | `Interrupt`, `SetModel`, `ResetModel`, `SetPermissionMode` | Control correlation/errors and real controls |
| Errors and cleanup | Contexts, typed errors, `Close`, `Wait`, `Stderr` | Lifecycle, race, and Unix process-group tests |
| Protocol access | `Message.Raw`, `Request`, `ExtraArgs`, `InitializeOptions`, `Transport` | JSON preservation and transport tests; no promise of complete advanced host behavior |

One-shot queries retain task/session-state tracking so the SDK does not close input before a native background agent's continuation. This is query correctness, not a task-management API.

## Outside the current scope

These advanced implementations are not part of the core module:

- SessionStore, transcript import, summary indexes, and temporary history reconstruction.
- Local session listing, history inspection/editing, rename, tags, deletion, forks, and subagent-history management.
- File checkpoint/rewind and file-read controls.
- Independent settings resolution, enterprise-policy merging, and settings-file writes.
- Startup/prewarm, spare claiming, and reinitialization after transport gaps.
- Dynamic MCP server replacement and runtime plugin/Skills/output-style reloads.
- Dedicated elicitation/user-dialog callbacks and hosted/browser connection adapters.
- Background-task management, asynchronous queue management, and dedicated usage-query controls.

Claude Code may use some of these capabilities itself through native configuration. That does not make their management APIs or required host callbacks supported by this Go SDK.

## Validation limits

Core tests cover normal results, failures, cancellation, and cleanup. Not every permission-approval scenario, Sandbox combination, or platform behavior has an end-to-end test.

Unix process-group cleanup has tests. Windows currently terminates only the direct child; local checks use cross-compilation, with runtime ordinary tests configured in CI. Some events/responses remain `json.RawMessage` instead of reproducing every official type.

Ordinary checks do not call a model:

```sh
go test -race ./claude/...
go vet ./claude/...
```

Real core tests consume model usage and require access to the configured service/proxy:

```sh
AGENTBRIDGE_LIVE_TEST=1 AGENTBRIDGE_CLAUDE_PATH=/path/to/claude-2.1.318 \
  go test -tags=integration -run '^TestLive' -v ./claude
```

An optional, model-free comparison uses a separately unpacked official npm package:

```sh
AGENTBRIDGE_UPSTREAM_SDK=/path/to/package/sdk.mjs \
  go test -tags=integration -run '^TestUpstreamOptionsParity$' -v ./claude
```

References: [Python SDK 0.2.178](https://github.com/anthropics/claude-agent-sdk-python/releases/tag/v0.2.178), [Python API](https://code.claude.com/docs/en/agent-sdk/python), [TypeScript SDK 0.3.318](https://github.com/anthropics/claude-agent-sdk-typescript/releases/tag/v0.3.318).
