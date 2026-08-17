#!/usr/bin/env node
// A stand-in for the `claude` binary. It records how it was invoked and replays
// recorded CLI output, so the adapter is exercised against a real subprocess
// without reaching the network. Behaviour is driven by env vars:
//
//   FAKE_CLAUDE_RECORD    file to write {argv, stdin} to
//   FAKE_CLAUDE_MODE      'stream' (default), 'nonstream', 'error', 'silent'
//   FAKE_CLAUDE_TEXT      assistant text to emit
//   FAKE_CLAUDE_EXIT      exit code
//   FAKE_CLAUDE_STDERR    text to write to stderr

import { writeFileSync } from 'node:fs';

const argv = process.argv.slice(2);
const mode = process.env.FAKE_CLAUDE_MODE ?? 'stream';
const text = process.env.FAKE_CLAUDE_TEXT ?? 'hello';
const exitCode = Number(process.env.FAKE_CLAUDE_EXIT ?? '0');
const record = process.env.FAKE_CLAUDE_RECORD;

function emit(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

async function readStdin() {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  return Buffer.concat(chunks).toString('utf8');
}

const SESSION = 'session-fake';

function streamEvent(event) {
  emit({ type: 'stream_event', event, session_id: SESSION });
}

const stdin = await readStdin();
if (record !== undefined) {
  writeFileSync(record, JSON.stringify({ argv, stdin }, null, 2));
}

if (process.env.FAKE_CLAUDE_STDERR !== undefined) {
  process.stderr.write(process.env.FAKE_CLAUDE_STDERR);
}

const usage = { input_tokens: 11, output_tokens: 4 };
const message = {
  id: 'msg_fake_1',
  type: 'message',
  role: 'assistant',
  model: 'claude-fake-1',
  content: [{ type: 'text', text }],
  stop_reason: null,
  usage,
};

if (mode !== 'silent') {
  // Envelopes the adapter must ignore, interleaved as the real CLI does.
  emit({ type: 'rate_limit_event', rate_limit_info: { status: 'allowed' } });
  emit({ type: 'system', subtype: 'init', session_id: SESSION, tools: [] });
}

if (mode === 'stream') {
  streamEvent({ type: 'message_start', message: { ...message, content: [] } });
  streamEvent({
    type: 'content_block_start',
    index: 0,
    content_block: { type: 'text', text: '' },
  });
  for (const piece of text.split(' ')) {
    streamEvent({
      type: 'content_block_delta',
      index: 0,
      delta: { type: 'text_delta', text: piece },
    });
  }
  streamEvent({ type: 'content_block_stop', index: 0 });
  streamEvent({ type: 'message_delta', delta: { stop_reason: 'end_turn' }, usage });
  streamEvent({ type: 'message_stop' });
}

if (mode === 'stream' || mode === 'nonstream') {
  emit({ type: 'assistant', message, session_id: SESSION });
  emit({
    type: 'result',
    subtype: 'success',
    is_error: false,
    stop_reason: 'end_turn',
    result: text,
    usage,
    session_id: SESSION,
  });
}

if (mode === 'error') {
  emit({
    type: 'result',
    subtype: 'error',
    is_error: true,
    result: 'the model refused',
    usage,
    session_id: SESSION,
  });
}

process.exit(exitCode);
