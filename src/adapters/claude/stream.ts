import type { CliEnvelope } from './envelopes.js';
import { readEnvelopes, resultError, streamEvent } from './envelopes.js';

/**
 * The CLI's `stream_event` payloads are already Anthropic SSE events, so the
 * conversion is unwrapping and re-framing rather than translation. Everything
 * else the CLI emits describes the session and is dropped.
 */
function encodeEvent(event: Record<string, unknown>): string {
  const type = typeof event['type'] === 'string' ? event['type'] : 'message';
  return `event: ${type}\ndata: ${JSON.stringify(event)}\n\n`;
}

/** Reported in-band: the stream has already sent 200 by the time this is known. */
function encodeError(message: string): string {
  const payload = {
    type: 'error',
    error: { type: 'api_error', message },
  };
  return `event: error\ndata: ${JSON.stringify(payload)}\n\n`;
}

export type StreamSource = {
  envelopes: AsyncIterable<Uint8Array>;
  /** Resolves once the CLI has exited, with its message when the run failed. */
  completion: Promise<string | null>;
};

/**
 * Re-frames the CLI's output as an Anthropic SSE stream. Events are forwarded
 * as they arrive rather than collected, so the first token reaches the client
 * without waiting for the run to finish.
 */
export function toEventStream(source: StreamSource): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream<Uint8Array>({
    async start(controller) {
      let reported: string | null = null;
      try {
        for await (const envelope of readEnvelopes(source.envelopes)) {
          const failure = resultError(envelope satisfies CliEnvelope);
          if (failure !== null) reported = failure;
          const event = streamEvent(envelope);
          if (event !== null) controller.enqueue(encoder.encode(encodeEvent(event)));
        }
        const failure = await source.completion;
        const message = reported ?? failure;
        if (message !== null) controller.enqueue(encoder.encode(encodeError(message)));
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        controller.enqueue(encoder.encode(encodeError(message)));
      }
      controller.close();
    },
  });
}
