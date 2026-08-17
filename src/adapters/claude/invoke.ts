import { spawn } from 'node:child_process';
import type { ChildProcessWithoutNullStreams } from 'node:child_process';
import type { ClaudeMode } from '../../policy/headers.js';
import type { ProviderRequestBody } from '../provider.js';
import { flattenPrompt, flattenSystem } from './prompt.js';

const BIN_VARIABLE = 'CAVEMAN_CLAUDE_BIN';
const DEFAULT_BIN = 'claude';

/**
 * Tool names denied in proxy mode. The CLI has no flag for "no tools at all",
 * so the built-ins that reach outside the conversation are named explicitly.
 */
const PROXY_DENIED_TOOLS = [
  'Bash',
  'Edit',
  'Write',
  'Read',
  'Glob',
  'Grep',
  'WebFetch',
  'WebSearch',
  'Task',
  'NotebookEdit',
];

export function claudeBinary(): string {
  const configured = process.env[BIN_VARIABLE];
  if (configured === undefined || configured.trim() === '') return DEFAULT_BIN;
  return configured.trim();
}

export type InvokeOptions = {
  body: ProviderRequestBody;
  mode: ClaudeMode;
  stream: boolean;
};

/**
 * Builds the CLI arguments. The prompt never appears here: variadic flags such
 * as `--disallowed-tools` swallow a trailing positional, so it goes on stdin.
 */
export function buildArgs(options: InvokeOptions): string[] {
  const args = [
    '--print',
    '--input-format',
    'stream-json',
    '--output-format',
    'stream-json',
    '--verbose',
    '--max-turns',
    '1',
  ];
  if (options.stream) args.push('--include-partial-messages');

  const model = options.body['model'];
  if (typeof model === 'string' && model.trim() !== '') {
    args.push('--model', model.trim());
  }

  const system = flattenSystem(options.body);
  if (options.mode === 'proxy') {
    // Replaces the CLI's own agent prompt, so the run behaves as closely to a
    // plain model call as a CLI session can.
    args.push('--system-prompt', system);
    args.push('--disallowed-tools', ...PROXY_DENIED_TOOLS);
  } else if (system !== '') {
    args.push('--append-system-prompt', system);
  }
  return args;
}

/** One stdin line, in the shape `--input-format stream-json` expects. */
export function buildStdin(body: ProviderRequestBody): string {
  const message = {
    type: 'user',
    message: {
      role: 'user',
      content: [{ type: 'text', text: flattenPrompt(body) }],
    },
  };
  return `${JSON.stringify(message)}\n`;
}

export class ClaudeCliError extends Error {
  constructor(message: string, cause?: unknown) {
    super(message, cause === undefined ? undefined : { cause });
    this.name = 'ClaudeCliError';
  }
}

export type ClaudeRun = {
  stdout: AsyncIterable<Uint8Array>;
  /**
   * Rejects with a `ClaudeCliError` when the process could not start at all.
   * Separate from `completion` so a missing binary can be reported as an error
   * response, before any status has been committed.
   */
  spawned: Promise<void>;
  /** Resolves with the failure message when the run failed, else null. */
  completion: Promise<string | null>;
};

function collectStderr(child: ChildProcessWithoutNullStreams): { read(): string } {
  let text = '';
  child.stderr.setEncoding('utf8');
  child.stderr.on('data', (chunk: string) => {
    text += chunk;
  });
  return {
    read: () => text.trim(),
  };
}

function spawnFailure(error: NodeJS.ErrnoException): ClaudeCliError {
  if (error.code === 'ENOENT') {
    return new ClaudeCliError(
      `spawning the claude CLI failed: "${claudeBinary()}" was not found`,
      error,
    );
  }
  return new ClaudeCliError('spawning the claude CLI failed', error);
}

function exitFailure(
  code: number | null,
  signal: NodeJS.Signals | null,
  detail: string,
): string {
  const reason = signal !== null ? `was killed by ${signal}` : `exited with code ${code}`;
  return detail === '' ? `claude CLI ${reason}` : `claude CLI ${reason}: ${detail}`;
}

/**
 * Spawns the CLI and hands back its stdout. The child is killed if the caller
 * aborts, so a client that hangs up does not leave a process running.
 */
export function invokeClaude(options: InvokeOptions, signal: AbortSignal): ClaudeRun {
  const child = spawn(claudeBinary(), buildArgs(options), {
    stdio: ['pipe', 'pipe', 'pipe'],
  });
  const stderr = collectStderr(child);

  let settleSpawned: (error?: ClaudeCliError) => void = () => undefined;
  const spawned = new Promise<void>((resolve, reject) => {
    settleSpawned = (error) => (error === undefined ? resolve() : reject(error));
  });

  const completion = new Promise<string | null>((resolve) => {
    child.on('error', (error: NodeJS.ErrnoException) => {
      settleSpawned(spawnFailure(error));
      resolve(null);
    });
    child.on('spawn', () => settleSpawned());
    child.on('close', (code, signalName) => {
      settleSpawned();
      resolve(code === 0 ? null : exitFailure(code, signalName, stderr.read()));
    });
  });

  const abort = (): void => {
    child.kill('SIGTERM');
  };
  if (signal.aborted) abort();
  else signal.addEventListener('abort', abort, { once: true });
  void completion.finally(() => signal.removeEventListener('abort', abort));

  // A CLI that exits before reading stdin makes the write fail; the exit code
  // is the meaningful error, so the broken pipe is ignored.
  child.stdin.on('error', () => undefined);
  child.stdin.end(buildStdin(options.body));

  return { stdout: child.stdout, spawned, completion };
}
