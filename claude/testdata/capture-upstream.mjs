// Dev-only differential runner. The upstream package is downloaded separately,
// pinned to the baseline version; it is not a Go SDK runtime dependency.
import { spawn } from 'node:child_process';
import { pathToFileURL } from 'node:url';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const { query } = await import(pathToFileURL(process.env.AGENTBRIDGE_UPSTREAM_SDK).href);
const fixture = resolve(dirname(fileURLToPath(import.meta.url)), 'capture-cli.mjs');
const options = JSON.parse(process.env.AGENTBRIDGE_UPSTREAM_OPTIONS);
options.pathToClaudeCodeExecutable = process.execPath;
options.canUseTool = async () => ({ behavior: 'deny', message: 'fixture' });
options.spawnClaudeCodeProcess = params => spawn(process.execPath, [fixture, ...params.args], {
  cwd: params.cwd, env: { ...params.env, AGENTBRIDGE_CAPTURE_FILE: process.env.AGENTBRIDGE_CAPTURE_FILE },
  stdio: ['pipe', 'pipe', 'pipe'], signal: params.signal,
});
for await (const message of query({ prompt: 'fixture prompt', options })) {
  if (message.type === 'result' && message.is_error) throw new Error('fixture failed');
}
