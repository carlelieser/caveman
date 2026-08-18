import { toIR } from '../src/adapters/anthropic/to-ir.js';
import { fromIR } from '../src/adapters/anthropic/from-ir.js';
const bodies: Record<string, unknown>[] = [
  { model: 'm', max_tokens: 1, messages: [{ role: 123, content: [{ type: 'text', text: 'hi' }] }] },
  { model: 'm', max_tokens: 1, messages: [{ role: null, content: [{ type: 'text', text: 'hi' }] }] },
  { model: 'm', max_tokens: 1, messages: [{ content: [{ type: 'text', text: 'hi' }] }] },
];
for (const b of bodies) {
  const inBytes = JSON.stringify(b);
  const outBytes = JSON.stringify(fromIR(toIR(b)));
  console.log(inBytes === outBytes ? 'ROUNDTRIPS ' : 'CHANGES    ', inBytes, '->', outBytes);
}
