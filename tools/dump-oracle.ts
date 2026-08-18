import nlp from 'compromise';
import { mkdirSync, writeFileSync } from 'node:fs';
import { REQUEST_FIXTURES } from '../test/fixtures/requests.js';
import { compressText, proseLength } from '../src/compression/compress.js';
import { classifyWords } from '../src/compression/classify.js';
import { classifyRegions } from '../src/compression/regions.js';
import { LEVEL_NAMES } from '../src/compression/levels.js';
import type { CompressContext, CompressKind, CompressRole } from '../src/compression/compress.js';
import { ALL_SCOPES, collectTextNodes } from '../src/ir/walk.js';
import { toIR } from '../src/adapters/anthropic/to-ir.js';
import type { TextNode } from '../src/ir/walk.js';

const OUT_DIR = new URL('../testdata/golden/', import.meta.url);

type TermDump = {
  text: string;
  pre: string;
  post: string;
  tags: string[];
};

function dumpTerms(text: string): TermDump[] {
  const sentences = nlp(text).json({ terms: { tags: true } }) as {
    terms?: { text?: string; pre?: string; post?: string; tags?: string[] }[];
  }[];
  return sentences.flatMap((sentence) =>
    (sentence.terms ?? []).map((term) => ({
      text: term.text ?? '',
      pre: term.pre ?? '',
      post: term.post ?? '',
      tags: term.tags ?? [],
    })),
  );
}

function contextOf(node: TextNode): CompressContext {
  return {
    role: (node.role ?? 'user') as CompressRole,
    kind: (node.path.scope === 'tool_results' ? 'tool_result' : 'text') as CompressKind,
  };
}

const taggerOracle: unknown[] = [];
const compressionOracle: unknown[] = [];
const regionsOracle: unknown[] = [];

for (const fixture of REQUEST_FIXTURES) {
  const ir = toIR(fixture.body);
  const nodes = collectTextNodes(ir, ALL_SCOPES);
  nodes.forEach((node, nodeIndex) => {
    const id = `${fixture.name}#${nodeIndex}`;
    const regions = classifyRegions(node.text);

    taggerOracle.push({
      id,
      text: node.text,
      terms: dumpTerms(node.text),
      words: classifyWords(node.text, regions),
    });

    regionsOracle.push({
      id,
      text: node.text,
      regions,
      proseLength: proseLength(node.text),
    });

    for (const level of LEVEL_NAMES) {
      const result = compressText({
        text: node.text,
        level,
        context: contextOf(node),
      });
      compressionOracle.push({
        id,
        level,
        role: contextOf(node).role,
        kind: contextOf(node).kind,
        in: node.text,
        out: result.text,
        stats: result.stats,
      });
    }
  });
}

mkdirSync(OUT_DIR, { recursive: true });

function write(name: string, value: unknown): void {
  const path = new URL(name, OUT_DIR);
  writeFileSync(path, JSON.stringify(value, null, 2) + '\n');
  process.stdout.write(`${name}\n`);
}

write('tagger.json', taggerOracle);
write('compression.json', compressionOracle);
write('regions.json', regionsOracle);

process.stdout.write(
  `fixtures=${REQUEST_FIXTURES.length} nodes=${taggerOracle.length} cases=${compressionOracle.length}\n`,
);
