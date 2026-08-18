import { mkdirSync, writeFileSync } from 'node:fs';
import { compressText, proseLength } from '../src/compression/compress.js';
import { classifyWords } from '../src/compression/classify.js';
import { classifyRegions } from '../src/compression/regions.js';
import { LEVEL_NAMES } from '../src/compression/levels.js';

/**
 * The recorded corpus is ASCII apart from one em-dash, so the grapheme-boundary
 * filter and the pictograph exclusion in `LEADING_PUNCTUATION_PATTERN` are
 * unexercised by it. Those two are backed by `Intl.Segmenter` and
 * `\p{Extended_Pictographic}`, whose Go replacements are substitutions rather
 * than ports, so they get their own cases.
 */
const CASES: readonly [string, string][] = [
  ['zwj-family', 'The family 👨‍👩‍👧‍👦 was very happy about the result.'],
  ['zwj-family-adjacent', 'the 👨‍👩‍👧‍👦, and a dog'],
  ['emoji-after-dropped-word', 'it is a 🎉 party'],
  ['emoji-only', '🎉🎊✨'],
  ['skin-tone', 'the developer 👩🏽‍💻 quickly fixed the bug'],
  ['flag-sequence', 'the flag 🇯🇵 is red and white'],
  ['keycap', 'press the 1️⃣ key on the keyboard'],
  ['variation-selector', 'the ❤️ was in the message'],
  ['combining-accent', 'the café was quite busy'],
  ['combining-decomposed', 'the café was quite busy'],
  ['cjk-han', 'the 日本語 text is in the file'],
  ['cjk-mixed', '这是 a test of the mixed script handling'],
  ['hiragana', 'the ひらがな characters were parsed'],
  ['hangul', 'the 한국어 string was empty'],
  ['thai', 'the ภาษาไทย value is set'],
  ['rtl-arabic', 'the العربية text was reversed'],
  ['nbsp', 'the value is a number'],
  ['emdash-unspaced', 'the value—a number—was wrong'],
  ['zero-width-space', 'the​value is set'],
  ['emoji-punct-boundary', 'is the build green? 🎉 yes it is'],
  ['math-symbols', 'the ∑ of the values is ∞'],
  ['mixed-emoji-code', 'run `npm test` 🎉 and the suite is green'],
];

type Out = {
  id: string;
  text: string;
  regions: unknown;
  proseLength: number;
  words: unknown;
  levels: { level: string; out: string; stats: unknown }[];
};

const dump: Out[] = CASES.map(([id, text]) => {
  const regions = classifyRegions(text);
  return {
    id,
    text,
    regions,
    proseLength: proseLength(text),
    words: classifyWords(text, regions),
    levels: LEVEL_NAMES.map((level) => {
      const result = compressText({
        text,
        level,
        context: { role: 'user', kind: 'text' },
      });
      return { level, out: result.text, stats: result.stats };
    }),
  };
});

const OUT_DIR = new URL('../testdata/golden/', import.meta.url);
mkdirSync(OUT_DIR, { recursive: true });
writeFileSync(new URL('unicode.json', OUT_DIR), JSON.stringify(dump, null, 2) + '\n');
process.stdout.write(`unicode.json cases=${dump.length}\n`);
