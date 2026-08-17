import { createServer } from 'node:http';
import type { IncomingMessage, Server, ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';

export type RecordedRequest = {
  method: string;
  url: string;
  headers: Record<string, string | string[] | undefined>;
  body: string;
};

export type UpstreamReply = (
  request: RecordedRequest,
  response: ServerResponse,
) => void | Promise<void>;

export type FakeUpstream = {
  baseUrl: string;
  requests: RecordedRequest[];
  reply(handler: UpstreamReply): void;
  close(): Promise<void>;
};

function readBody(request: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    request.on('data', (chunk: Buffer) => chunks.push(chunk));
    request.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
    request.on('error', reject);
  });
}

function jsonReply(response: ServerResponse): void {
  response.writeHead(200, { 'content-type': 'application/json' });
  response.end(JSON.stringify({ type: 'message', content: [] }));
}

export async function startFakeUpstream(): Promise<FakeUpstream> {
  const requests: RecordedRequest[] = [];
  let handler: UpstreamReply = (_request, response) => jsonReply(response);

  const server: Server = createServer((request, response) => {
    void readBody(request).then((body) => {
      const recorded: RecordedRequest = {
        method: request.method ?? '',
        url: request.url ?? '',
        headers: request.headers,
        body,
      };
      requests.push(recorded);
      return handler(recorded, response);
    });
  });

  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address() as AddressInfo;

  return {
    baseUrl: `http://127.0.0.1:${address.port}`,
    requests,
    reply(next: UpstreamReply) {
      handler = next;
    },
    close() {
      return new Promise<void>((resolve, reject) => {
        server.close((error) => (error ? reject(error) : resolve()));
      });
    },
  };
}

/** Writes SSE events with a delay between each so buffering is observable. */
export async function writeDelayedEvents(
  response: ServerResponse,
  events: readonly string[],
  delayMs: number,
): Promise<void> {
  response.writeHead(200, {
    'content-type': 'text/event-stream',
    'cache-control': 'no-cache',
  });
  for (const event of events) {
    response.write(event);
    await new Promise((resolve) => setTimeout(resolve, delayMs));
  }
  response.end();
}
