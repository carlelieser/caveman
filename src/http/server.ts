import '../config/env.js';
import { serve } from '@hono/node-server';
import { Hono } from 'hono';
import { pathToFileURL } from 'node:url';
import type { ProviderAdapter } from '../adapters/provider.js';
import type { CompressionStage } from './messages.js';
import type { SavingsReporter } from '../telemetry/savings-log.js';
import type { LogSink } from '../telemetry/logging.js';
import { anthropicAdapter } from '../adapters/anthropic/adapter.js';
import { createMessagesHandler } from './messages.js';
import { createCompressionStage } from './compression-stage.js';
import { createLogSink } from '../telemetry/logging.js';
import { createSavingsReporter } from '../telemetry/savings-log.js';

const DEFAULT_PORT = 8787;

/** Providers Caveman serves. Adding one is adding an entry here. */
export const REGISTERED_ADAPTERS: readonly ProviderAdapter[] = [anthropicAdapter];

/** An app paired with the reporter tallying its session. */
export type ServedApp = {
  app: Hono;
  reporter: SavingsReporter;
};

export function createServedApp(
  reporter: SavingsReporter = createSavingsReporter(createLogSink()),
  stage: CompressionStage = createCompressionStage(),
  adapters: readonly ProviderAdapter[] = REGISTERED_ADAPTERS,
): ServedApp {
  const app = new Hono();
  for (const adapter of adapters) {
    app.post(adapter.path, createMessagesHandler(adapter, stage, reporter));
  }
  return { app, reporter };
}

export function createApp(
  stage: CompressionStage = createCompressionStage(),
  adapters: readonly ProviderAdapter[] = REGISTERED_ADAPTERS,
): Hono {
  return createServedApp(undefined, stage, adapters).app;
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

const SHUTDOWN_SIGNALS: readonly NodeJS.Signals[] = ['SIGINT', 'SIGTERM'];

/** Writes the session total on the way out. A hard kill skips it. */
function reportOnShutdown(reporter: SavingsReporter, sink: LogSink): void {
  for (const signal of SHUTDOWN_SIGNALS) {
    process.once(signal, () => {
      const summary = reporter.summary();
      if (summary !== null) sink(summary);
      process.exit(0);
    });
  }
}

if (isRunAsScript()) {
  const port = listenPort();
  const served = createServedApp();
  serve({ fetch: served.app.fetch, port });
  process.stdout.write(`caveman listening on http://localhost:${port}\n`);
  reportOnShutdown(served.reporter, createLogSink());
}
