#!/usr/bin/env node
// A model-free protocol fixture shared by the Go and official TypeScript SDKs.
import { createInterface } from 'node:readline';
import { writeFileSync } from 'node:fs';

if (process.argv.includes('--version')) {
  console.log('2.1.318 (Claude Code)');
  process.exit(0);
}
const capture = { args: process.argv.slice(2) };
const save = () => writeFileSync(process.env.AGENTBRIDGE_CAPTURE_FILE, JSON.stringify(capture));
const emit = value => process.stdout.write(JSON.stringify(value) + '\n');
for await (const line of createInterface({ input: process.stdin })) {
  const frame = JSON.parse(line);
  if (frame.type === 'control_request') {
    if (frame.request.subtype === 'initialize') { capture.initialize = frame.request; save(); }
    emit({ type: 'control_response', response: { subtype: 'success', request_id: frame.request_id,
      response: { commands: [], models: [], agents: [], account: {}, hooks_applied: true, hook_on_failure_supported: true } } });
  } else if (frame.type === 'user') {
    capture.user = frame; save();
    emit({ type: 'result', subtype: 'success', is_error: false,
      session_id: '00000064-0000-4000-8000-000000000000', result: 'fixture', num_turns: 1,
      usage: { input_tokens: 1, output_tokens: 1 } });
  }
}
