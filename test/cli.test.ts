import { execFile } from 'node:child_process';
import { createServer } from 'node:http';
import type { Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

const ROOT = fileURLToPath(new URL('..', import.meta.url));
const CAVEMAN = join(ROOT, 'bin', 'caveman');

type Run = { code: number; stdout: string; stderr: string };

let runDir: string;

/** Runs the CLI against a throwaway run directory. Never rejects: the exit code
 *  is the assertion target. */
function run(args: string[], env: Record<string, string> = {}): Promise<Run> {
  return new Promise((resolve) => {
    execFile(
      CAVEMAN,
      args,
      { env: { ...process.env, CAVEMAN_RUN_DIR: runDir, ...env }, timeout: 40_000 },
      (error, stdout, stderr) => {
        const code = error === null ? 0 : typeof error.code === 'number' ? error.code : 1;
        resolve({ code, stdout, stderr });
      },
    );
  });
}

async function freePort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve) => server.listen(0, resolve));
  const { port } = server.address() as AddressInfo;
  await new Promise<void>((resolve) => server.close(() => resolve()));
  return port;
}

function startServerOn(port: number, body: string): Promise<Server> {
  const server = createServer((_request, response) => {
    response.writeHead(200, { 'content-type': 'application/json' });
    response.end(body);
  });
  return new Promise((resolve) => server.listen(port, () => resolve(server)));
}

beforeEach(async () => {
  runDir = await mkdtemp(join(tmpdir(), 'caveman-cli-'));
});

afterEach(async () => {
  await rm(runDir, { recursive: true, force: true });
});

describe('usage', () => {
  it('lists the clients it found on disk', async () => {
    const result = await run(['help']);
    expect(result.code).toBe(0);
    expect(result.stdout).toContain('claude');
  });

  it('exits 2 with no command, since nothing was done', async () => {
    const result = await run([]);
    expect(result.code).toBe(2);
  });

  it('names an unknown command', async () => {
    const result = await run(['nope']);
    expect(result.code).toBe(2);
    expect(result.stderr).toContain('nope');
  });
});

describe('port resolution', () => {
  it('prefers PORT from the environment', async () => {
    const result = await run(['status'], { PORT: '9393' });
    expect(result.stdout).toContain('9393');
  });

  it('reports the server’s own message for an out-of-range port', async () => {
    const result = await run(['status'], { PORT: '99999' });
    expect(result.code).toBe(1);
    expect(result.stderr).toContain('reading PORT failed');
    expect(result.stderr).toContain('99999');
  });

  it('reports the server’s own message for a non-numeric port', async () => {
    const result = await run(['status'], { PORT: 'abc' });
    expect(result.code).toBe(1);
    expect(result.stderr).toContain('reading PORT failed');
  });
});

describe('lifecycle', () => {
  it('starts, reports status, and stops', async () => {
    const port = String(await freePort());
    const started = await run(['up'], { PORT: port });
    expect(started.code).toBe(0);
    expect(started.stdout).toContain(port);
    expect(existsSync(join(runDir, 'caveman.pid'))).toBe(true);

    const status = await run(['status'], { PORT: port });
    expect(status.code).toBe(0);
    expect(status.stdout).toContain('running');

    const stopped = await run(['down'], { PORT: port });
    expect(stopped.code).toBe(0);
    expect(existsSync(join(runDir, 'caveman.pid'))).toBe(false);
  });

  it('reports an already-running server rather than starting a second', async () => {
    const port = String(await freePort());
    await run(['up'], { PORT: port });
    const pidFile = join(runDir, 'caveman.pid');
    const first = await readFile(pidFile, 'utf8');

    const again = await run(['up'], { PORT: port });
    expect(again.code).toBe(0);
    expect(again.stdout).toContain('already running');
    expect(await readFile(pidFile, 'utf8')).toBe(first);

    await run(['down'], { PORT: port });
  });

  it('exits 0 when stopping something that is not running', async () => {
    const result = await run(['down'], { PORT: String(await freePort()) });
    expect(result.code).toBe(0);
    expect(result.stdout).toContain('not running');
  });

  it('clears a stale pid file instead of reporting it as running', async () => {
    const port = String(await freePort());
    await writeFile(join(runDir, 'caveman.pid'), '999999');

    const status = await run(['status'], { PORT: port });
    expect(status.stdout).toContain('not running');

    const stopped = await run(['down'], { PORT: port });
    expect(stopped.code).toBe(0);
    expect(existsSync(join(runDir, 'caveman.pid'))).toBe(false);
  });
});

describe('a port held by another process', () => {
  let foreign: Server | null = null;

  afterEach(async () => {
    if (foreign !== null) {
      await new Promise<void>((resolve) => foreign!.close(() => resolve()));
      foreign = null;
    }
  });

  it('refuses to start when the answer is not caveman', async () => {
    const port = await freePort();
    foreign = await startServerOn(port, '{"status":"ok"}');

    const result = await run(['up'], { PORT: String(port) });
    expect(result.code).toBe(3);
    expect(result.stderr).toContain(String(port));
  });

  it('leaves that process alive', async () => {
    const port = await freePort();
    foreign = await startServerOn(port, '{"status":"ok"}');

    await run(['up'], { PORT: String(port) });
    expect(foreign.listening).toBe(true);
  });
});

describe('client dispatch', () => {
  let clientDir: string;

  beforeEach(async () => {
    clientDir = await mkdtemp(join(tmpdir(), 'caveman-clients-'));
  });

  afterEach(async () => {
    await rm(clientDir, { recursive: true, force: true });
  });

  it('passes the base url and every argument to the client', async () => {
    const port = String(await freePort());
    await writeFile(
      join(clientDir, 'echoer.sh'),
      'client_launch() { printf "url=%s args=%s\\n" "$CAVEMAN_BASE_URL" "$*"; }\n',
    );

    await run(['up'], { PORT: port });
    const result = await run(['echoer', '--resume', '-p', 'hi there'], {
      PORT: port,
      CAVEMAN_CLIENT_DIR: clientDir,
    });
    await run(['down'], { PORT: port });

    expect(result.stdout).toContain(`url=http://localhost:${port}`);
    expect(result.stdout).toContain('args=--resume -p hi there');
  });

  it('lists a client dropped into the directory, with no registry to edit', async () => {
    await writeFile(join(clientDir, 'codex.sh'), 'client_launch() { :; }\n');
    const result = await run(['help'], { CAVEMAN_CLIENT_DIR: clientDir });
    expect(result.stdout).toContain('codex');
  });
});

describe('compression level', () => {
  let clientDir: string;
  let port: string;

  /** Reports the level it would send and the arguments it was left with. */
  async function writeReporter(): Promise<void> {
    await writeFile(
      join(clientDir, 'show.sh'),
      'client_launch() { printf "level=%s args=%s\\n" "$CAVEMAN_LEVEL" "$*"; }\n',
    );
  }

  function show(args: string[]): Promise<Run> {
    return run(['show', ...args], { PORT: port, CAVEMAN_CLIENT_DIR: clientDir });
  }

  beforeEach(async () => {
    clientDir = await mkdtemp(join(tmpdir(), 'caveman-level-'));
    port = String(await freePort());
    await writeReporter();
  });

  afterEach(async () => {
    await run(['down'], { PORT: port });
    await rm(clientDir, { recursive: true, force: true });
  });

  it('rejects a level that is not one of the four', async () => {
    const result = await run(['up', '--level', 'bogus'], { PORT: port });
    expect(result.code).toBe(2);
    expect(result.stderr).toContain('bogus');
  });

  it('rejects the flag with no level after it', async () => {
    const result = await run(['up', '--level'], { PORT: port });
    expect(result.code).toBe(2);
  });

  it('compresses when no level was given, since asking for the CLI is the ask', async () => {
    await run(['up'], { PORT: port });
    expect((await show([])).stdout).toContain('level=caveman');
  });

  it('still allows off explicitly, for a baseline through the same proxy', async () => {
    await run(['up', '-l', 'off'], { PORT: port });
    expect((await show([])).stdout).toContain('level=off');
  });

  it('inherits the level given to up', async () => {
    await run(['up', '-l', 'moderate'], { PORT: port });
    expect((await show([])).stdout).toContain('level=moderate');
  });

  it('reports the stored level in status', async () => {
    await run(['up', '-l', 'light'], { PORT: port });
    const result = await run(['status'], { PORT: port });
    expect(result.stdout).toContain('level: light');
  });

  it('lets a client override the stored level for that launch only', async () => {
    await run(['up', '-l', 'moderate'], { PORT: port });
    expect((await show(['-l', 'caveman'])).stdout).toContain('level=caveman');
    expect((await show([])).stdout).toContain('level=moderate');
  });

  it('accepts --level=value as well as a separate argument', async () => {
    await run(['up', '--level=caveman'], { PORT: port });
    expect((await show([])).stdout).toContain('level=caveman');
  });

  it('keeps the level flag out of the arguments the client receives', async () => {
    await run(['up'], { PORT: port });
    const result = await show(['-l', 'light', '--resume', '-p', 'hi there']);
    expect(result.stdout).toContain('args=--resume -p hi there');
  });

  it('leaves a client’s own -l alone after --', async () => {
    await run(['up', '-l', 'light'], { PORT: port });
    const result = await show(['--', '-l', 'something']);
    expect(result.stdout).toContain('level=light');
    expect(result.stdout).toContain('args=-l something');
  });
});
