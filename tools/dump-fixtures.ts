/**
 * Writes each request fixture out as the raw wire bytes the Go round-trip gate
 * compares against. `JSON.stringify` emits own keys in insertion order, which
 * is the property the gate depends on, so it is verified here rather than
 * assumed: each file is read back and its key order compared against the
 * fixture's.
 */
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { REQUEST_FIXTURES } from '../test/fixtures/requests.js';

const OUT_DIR = new URL('../testdata/golden/fixtures/', import.meta.url);

function slug(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
}

function keyOrders(value: unknown, path: string, into: string[]): void {
  if (Array.isArray(value)) {
    value.forEach((item, index) => keyOrders(item, `${path}[${index}]`, into));
    return;
  }
  if (typeof value !== 'object' || value === null) return;
  const keys = Object.keys(value as Record<string, unknown>);
  into.push(`${path}:${keys.join(',')}`);
  for (const key of keys) {
    keyOrders((value as Record<string, unknown>)[key], `${path}.${key}`, into);
  }
}

mkdirSync(OUT_DIR, { recursive: true });

const index: { name: string; file: string }[] = [];
const seen = new Set<string>();

for (const fixture of REQUEST_FIXTURES) {
  const file = `${slug(fixture.name)}.json`;
  if (seen.has(file)) throw new Error(`duplicate fixture file name: ${file}`);
  seen.add(file);

  const bytes = JSON.stringify(fixture.body);
  const path = new URL(file, OUT_DIR);
  writeFileSync(path, bytes);

  const reread = JSON.parse(readFileSync(path, 'utf8')) as unknown;
  if (JSON.stringify(reread) !== bytes) {
    throw new Error(`${file}: re-serialization differs from what was written`);
  }
  const written: string[] = [];
  const original: string[] = [];
  keyOrders(reread, '$', written);
  keyOrders(fixture.body, '$', original);
  if (written.join('\n') !== original.join('\n')) {
    throw new Error(`${file}: key order not preserved through the file`);
  }

  index.push({ name: fixture.name, file });
}

writeFileSync(new URL('index.json', OUT_DIR), JSON.stringify(index, null, 2) + '\n');
process.stdout.write(`fixtures=${index.length}\n`);
