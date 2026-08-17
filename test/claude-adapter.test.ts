import { readFileSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { claudeAdapter } from '../src/adapters/claude/adapter.js';
import { buildArgs, buildStdin } from '../src/adapters/claude/invoke.js';
import { flattenPrompt, flattenSystem } from '../src/adapters/claude/prompt.js';
import { createApp } from '../src/http/server.js';

const FAKE_CLI = fileURLToPath(new URL('./fixtures/fake-claude.mjs', import.meta.url));

type Invocation = { argv: string[]; stdin: string };

/**
 * A record file per test. A shared path lets one test read the invocation an
 * earlier one wrote, which is the kind of pass-in-isolation flake that is worse
 * than no assertion at all.
 */
let recordPath = '';

function readInvocation(): Invocation {
  return JSON.parse(readFileSync(recordPath, 'utf8')) as Invocation;
}

/** The recorded prompt, unwrapped from the stream-json line it arrives on. */
function recordedPrompt(): string {
  const line = readInvocation().stdin.trim();
  const parsed = JSON.parse(line) as {
    message: { content: { text: string }[] };
  };
  return parsed.message.content[0]?.text ?? '';
}

function flagValue(argv: string[], flag: string): string | undefined {
  const index = argv.indexOf(flag);
  return index === -1 ? undefined : argv[index + 1];
}

function post(body: unknown, headers: Record<string, string> = {}): Request {
  return new Request('http://caveman.test/claude/v1/messages', {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...headers },
    body: JSON.stringify(body),
  });
}

function request(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    model: 'claude-sonnet-4-5',
    max_tokens: 64,
    messages: [{ role: 'user', content: 'hello there' }],
    ...overrides,
  };
}

function app() {
  return createApp(undefined, [claudeAdapter]);
}

describe('claude adapter', () => {
  beforeEach(({ task }) => {
    recordPath = fileURLToPath(
      new URL(
        `./fixtures/.invocation-${task.id.replace(/\W/g, '_')}.json`,
        import.meta.url,
      ),
    );
    process.env['CAVEMAN_CLAUDE_BIN'] = FAKE_CLI;
    process.env['FAKE_CLAUDE_RECORD'] = recordPath;
  });

  afterEach(() => {
    delete process.env['CAVEMAN_CLAUDE_BIN'];
    delete process.env['FAKE_CLAUDE_RECORD'];
    delete process.env['FAKE_CLAUDE_MODE'];
    delete process.env['FAKE_CLAUDE_TEXT'];
    delete process.env['FAKE_CLAUDE_EXIT'];
    delete process.env['FAKE_CLAUDE_STDERR'];
    rmSync(recordPath, { force: true });
  });

  describe('non-streaming', () => {
    beforeEach(() => {
      process.env['FAKE_CLAUDE_MODE'] = 'nonstream';
    });

    it('assembles an Anthropic message response', async () => {
      const response = await app().fetch(post(request()));
      expect(response.status).toBe(200);
      expect(response.headers.get('content-type')).toContain('application/json');

      const body = (await response.json()) as Record<string, unknown>;
      expect(body['type']).toBe('message');
      expect(body['role']).toBe('assistant');
      expect(body['content']).toEqual([{ type: 'text', text: 'hello' }]);
      expect(body['stop_reason']).toBe('end_turn');
    });

    it('reports the usage the CLI billed rather than a local estimate', async () => {
      const response = await app().fetch(post(request()));
      const body = (await response.json()) as { usage: Record<string, number> };
      expect(body.usage['input_tokens']).toBe(11);
      expect(body.usage['output_tokens']).toBe(4);
    });

    it('sends the prompt on stdin, never as an argument', async () => {
      await app().fetch(post(request()));
      const invocation = readInvocation();
      expect(recordedPrompt()).toBe('hello there');
      expect(invocation.argv).not.toContain('hello there');
    });

    it('passes the requested model through to the CLI', async () => {
      await app().fetch(post(request({ model: 'haiku' })));
      expect(flagValue(readInvocation().argv, '--model')).toBe('haiku');
    });
  });

  describe('streaming', () => {
    it('re-frames CLI events as an Anthropic SSE stream', async () => {
      const response = await app().fetch(post(request({ stream: true })));
      expect(response.status).toBe(200);
      expect(response.headers.get('content-type')).toContain('text/event-stream');

      const text = await response.text();
      expect(text).toContain('event: message_start\n');
      expect(text).toContain('event: content_block_delta\n');
      expect(text).toContain('event: message_stop\n');
      expect(text.endsWith('\n\n')).toBe(true);
    });

    it('drops the CLI envelopes that are not Anthropic events', async () => {
      const response = await app().fetch(post(request({ stream: true })));
      const text = await response.text();
      expect(text).not.toContain('rate_limit_event');
      expect(text).not.toContain('"subtype":"init"');
      expect(text).not.toContain('session_id');
    });

    it('emits data lines that parse as Anthropic events', async () => {
      const response = await app().fetch(post(request({ stream: true })));
      const text = await response.text();
      const events = text
        .split('\n\n')
        .filter((block) => block !== '')
        .map((block) => {
          const line = block.split('\n').find((part) => part.startsWith('data: '));
          return JSON.parse(line?.slice('data: '.length) ?? '{}') as { type: string };
        });
      expect(events[0]?.type).toBe('message_start');
      expect(events.at(-1)?.type).toBe('message_stop');
    });

    it('adds the no-buffering headers the SSE passthrough applies', async () => {
      const response = await app().fetch(post(request({ stream: true })));
      expect(response.headers.get('cache-control')).toBe('no-cache, no-transform');
      expect(response.headers.get('x-accel-buffering')).toBe('no');
    });
  });

  describe('modes', () => {
    beforeEach(() => {
      process.env['FAKE_CLAUDE_MODE'] = 'nonstream';
    });

    it('replaces the agent prompt and denies tools by default', async () => {
      await app().fetch(post(request({ system: 'You are terse.' })));
      const { argv } = readInvocation();
      expect(flagValue(argv, '--system-prompt')).toBe('You are terse.');
      expect(argv).toContain('--disallowed-tools');
      expect(argv).not.toContain('--append-system-prompt');
    });

    it('appends the system prompt and keeps tools in agent mode', async () => {
      await app().fetch(
        post(request({ system: 'You are terse.' }), { 'X-Caveman-Claude-Mode': 'agent' }),
      );
      const { argv } = readInvocation();
      expect(flagValue(argv, '--append-system-prompt')).toBe('You are terse.');
      expect(argv).not.toContain('--system-prompt');
      expect(argv).not.toContain('--disallowed-tools');
    });

    it('rejects an unknown mode by naming the header', async () => {
      const response = await app().fetch(
        post(request(), { 'X-Caveman-Claude-Mode': 'sideways' }),
      );
      expect(response.status).toBe(400);
      const body = (await response.json()) as { error: { message: string } };
      expect(body.error.message).toContain('X-Caveman-Claude-Mode');
    });
  });

  describe('failures', () => {
    it('reports a missing binary in the Anthropic error envelope', async () => {
      process.env['CAVEMAN_CLAUDE_BIN'] = '/nonexistent/claude-binary';
      const response = await app().fetch(post(request()));
      expect(response.status).toBe(502);
      const body = (await response.json()) as {
        error: { type: string; message: string };
      };
      expect(body.error.type).toBe('invalid_request_error');
      expect(body.error.message).toContain('/nonexistent/claude-binary');
    });

    it('surfaces stderr when the CLI exits non-zero', async () => {
      process.env['FAKE_CLAUDE_MODE'] = 'silent';
      process.env['FAKE_CLAUDE_EXIT'] = '2';
      process.env['FAKE_CLAUDE_STDERR'] = 'not authenticated';
      const response = await app().fetch(post(request()));
      expect(response.status).toBe(502);
      const body = (await response.json()) as { error: { message: string } };
      expect(body.error.message).toContain('not authenticated');
    });

    it('reports an error the CLI declares in its result envelope', async () => {
      process.env['FAKE_CLAUDE_MODE'] = 'error';
      const response = await app().fetch(post(request()));
      expect(response.status).toBe(502);
      const body = (await response.json()) as { error: { message: string } };
      expect(body.error.message).toContain('the model refused');
    });
  });

  describe('compression', () => {
    beforeEach(() => {
      process.env['FAKE_CLAUDE_MODE'] = 'nonstream';
    });

    const VERBOSE =
      'Could you please go ahead and tell me what the weather is like in the city ' +
      'of San Francisco on this particular day, if that is something you can do?';

    it('compresses the prompt the CLI receives', async () => {
      await app().fetch(
        post(request({ messages: [{ role: 'user', content: VERBOSE }] }), {
          'X-Caveman-Compress': '0.4',
        }),
      );
      expect(recordedPrompt().length).toBeLessThan(VERBOSE.length);
    });

    it('reports the accounting headers on the response', async () => {
      const response = await app().fetch(
        post(request({ messages: [{ role: 'user', content: VERBOSE }] }), {
          'X-Caveman-Compress': '0.4',
        }),
      );
      expect(response.headers.get('X-Caveman-Ratio')).not.toBeNull();
      expect(Number(response.headers.get('X-Caveman-Tokens-After'))).toBeLessThan(
        Number(response.headers.get('X-Caveman-Tokens-Before')),
      );
    });

    it('forwards the prompt unchanged without a compress header', async () => {
      await app().fetch(
        post(request({ messages: [{ role: 'user', content: VERBOSE }] })),
      );
      expect(recordedPrompt()).toBe(VERBOSE);
    });
  });
});

describe('prompt flattening', () => {
  it('sends a lone user message bare, without turn labels', () => {
    const prompt = flattenPrompt({ messages: [{ role: 'user', content: 'hello' }] });
    expect(prompt).toBe('hello');
  });

  it('labels turns once the conversation has history', () => {
    const prompt = flattenPrompt({
      messages: [
        { role: 'user', content: 'My favorite color is teal.' },
        { role: 'assistant', content: 'Got it.' },
        { role: 'user', content: 'What color?' },
      ],
    });
    expect(prompt).toBe(
      'Human: My favorite color is teal.\n\nAssistant: Got it.\n\nHuman: What color?',
    );
  });

  it('names a tool call rather than dropping it silently', () => {
    const prompt = flattenPrompt({
      messages: [
        { role: 'user', content: 'weather?' },
        {
          role: 'assistant',
          content: [{ type: 'tool_use', id: 't1', name: 'get_weather', input: {} }],
        },
        {
          role: 'user',
          content: [{ type: 'tool_result', tool_use_id: 't1', content: 'sunny' }],
        },
      ],
    });
    expect(prompt).toContain('[tool_use: get_weather]');
    expect(prompt).toContain('[tool_result]');
    expect(prompt).toContain('sunny');
  });

  it('keeps images visible as a placeholder', () => {
    const prompt = flattenPrompt({
      messages: [
        {
          role: 'user',
          content: [
            { type: 'image', source: { type: 'base64', data: 'x' } },
            { type: 'text', text: 'what is this?' },
          ],
        },
      ],
    });
    expect(prompt).toBe('[image]\n\nwhat is this?');
  });

  it('omits thinking blocks, which cannot be replayed', () => {
    const prompt = flattenPrompt({
      messages: [
        {
          role: 'assistant',
          content: [
            { type: 'thinking', thinking: 'hmm', signature: 'sig' },
            { type: 'text', text: 'answer' },
          ],
        },
      ],
    });
    expect(prompt).toBe('answer');
  });

  it('flattens a block-array system prompt to a string', () => {
    const system = flattenSystem({
      system: [
        { type: 'text', text: 'You are terse.' },
        { type: 'text', text: 'Answer in one word.' },
      ],
    });
    expect(system).toBe('You are terse.\n\nAnswer in one word.');
  });
});

describe('cli arguments', () => {
  it('asks for partial messages only when streaming', () => {
    const body = { model: 'x', messages: [] };
    const streaming = buildArgs({ body, mode: 'proxy', stream: true });
    const buffered = buildArgs({ body, mode: 'proxy', stream: false });
    expect(streaming).toContain('--include-partial-messages');
    expect(buffered).not.toContain('--include-partial-messages');
  });

  it('omits the model flag when the request names no model', () => {
    const args = buildArgs({ body: { messages: [] }, mode: 'proxy', stream: false });
    expect(args).not.toContain('--model');
  });

  it('caps the run at a single turn', () => {
    const args = buildArgs({ body: { messages: [] }, mode: 'proxy', stream: false });
    expect(flagValue(args, '--max-turns')).toBe('1');
  });

  it('writes one newline-terminated stream-json line', () => {
    const stdin = buildStdin({ messages: [{ role: 'user', content: 'hi' }] });
    expect(stdin.endsWith('\n')).toBe(true);
    expect(stdin.trimEnd().split('\n')).toHaveLength(1);
    expect(JSON.parse(stdin)).toEqual({
      type: 'user',
      message: { role: 'user', content: [{ type: 'text', text: 'hi' }] },
    });
  });
});
