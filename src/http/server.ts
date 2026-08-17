import '../config/env.js';
import { serve } from '@hono/node-server';
import { Hono } from 'hono';
import { pathToFileURL } from 'node:url';
import type { ProviderAdapter } from '../adapters/provider.js';
import type { CompressionStage } from './messages.js';
import { anthropicAdapter } from '../adapters/anthropic/adapter.js';
import { claudeAdapter } from '../adapters/claude/adapter.js';
import { createMessagesHandler } from './messages.js';
import { createCompressionStage } from './compression-stage.js';

const DEFAULT_PORT = 8787;

/** Providers Caveman serves. Adding one is adding an entry here. */
export const REGISTERED_ADAPTERS: readonly ProviderAdapter[] = [
  anthropicAdapter,
  claudeAdapter,
];

export function createApp(
  stage: CompressionStage = createCompressionStage(),
  adapters: readonly ProviderAdapter[] = REGISTERED_ADAPTERS,
): Hono {
  const app = new Hono();
  for (const adapter of adapters) {
    app.post(adapter.path, createMessagesHandler(adapter, stage));
  }
  return app;
}

export const app = createApp();

export function listenPort(): number {
  const configured = process.env['PORT'];
  if (configured === undefined || configured.trim() === '') return DEFAULT_PORT;
  const parsed = Number(configured);
  if (!Number.isInteger(parsed) || parsed < 0 || parsed > 65535) {
    throw new Error(`reading PORT failed: "${configured}" is not a valid port`);
  }
  return parsed;
}

function isRunAsScript(): boolean {
  const entry = process.argv[1];
  if (entry === undefined) return false;
  return import.meta.url === pathToFileURL(entry).href;
}

if (isRunAsScript()) {
  const port = listenPort();
  serve({ fetch: app.fetch, port });
  process.stdout.write(`caveman listening on http://localhost:${port}\n`);
}
