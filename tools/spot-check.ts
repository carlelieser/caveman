import { compressText } from '../src/compression/compress.js';
import type { Level } from '../src/compression/levels.js';
const cases: [string, Level, string][] = [
  ['The man went to the store.', 'light', 'man went to store.'],
  ['an abandoned building was sold', 'caveman', 'building sold'],
  ['The very large dog quickly ate the food.', 'caveman', 'dog ate food.'],
];
for (const [input, level, want] of cases) {
  const got = compressText({ text: input, level, context: { role: 'user', kind: 'text' } }).text;
  console.log(`${got === want ? 'PASS' : 'FAIL'} [${level}] ${JSON.stringify(input)} -> ${JSON.stringify(got)}${got === want ? '' : ` want ${JSON.stringify(want)}`}`);
}
const survive = ['Do not proceed if the tests fail.', 'Stop unless the build is green.', 'It failed because the disk was full.'];
for (const s of survive) {
  const got = compressText({ text: s, level: 'caveman', context: { role: 'user', kind: 'text' } }).text;
  const keeps = ['not','if','unless','because'].filter(w => new RegExp(`\\b${w}\\b`,'i').test(s));
  const kept = keeps.every(w => new RegExp(`\\b${w}\\b`,'i').test(got));
  console.log(`${kept ? 'PASS' : 'FAIL'} [caveman] ${JSON.stringify(s)} -> ${JSON.stringify(got)} (keeps ${keeps.join(',')})`);
}
