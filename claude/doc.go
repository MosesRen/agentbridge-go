// Package claude drives the Claude Code CLI using its native, bidirectional
// stream-json protocol. It does not implement an agent loop or call the model API
// directly. Install Claude Code and configure authentication before use.
//
// Use Query for a stream ending at the first result, Run for its final result,
// or Dial for a persistent client with multiple turns and control requests.
// Always close streams and clients. A Dial context owns the process lifetime;
// contexts passed to Receive or Request only cancel that individual operation.
package claude
