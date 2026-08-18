// Dumps compromise's runtime model to JSON so the Go tagger can be generated from it.
// The match rules are dumped *after* compromise has parsed them, so the Go side never
// needs a port of the match-syntax parser.
import nlp from 'compromise'
import { mkdirSync, writeFileSync, readFileSync } from 'node:fs'

const version = JSON.parse(
  readFileSync(new URL('../node_modules/compromise/package.json', import.meta.url), 'utf8'),
).version
if (version !== '14.16.0') {
  throw new Error(`expected compromise 14.16.0, found ${version}`)
}

const OUT = new URL('../internal/tagger/modeldata/', import.meta.url)
mkdirSync(OUT, { recursive: true })

const world = nlp.world()
const model = world.model

const setToArr = v => (v instanceof Set ? Array.from(v) : v)

// compromise stores parsed tokens with Set and RegExp values that JSON drops silently
const encodeReg = function (reg) {
  const out = {}
  for (const k of Object.keys(reg)) {
    const v = reg[k]
    if (v === undefined) continue
    if (k === 'regex') {
      out.regex = v.source
      out.regexFlags = v.flags
    } else if (k === 'fastOr') {
      out.fastOr = Array.from(v)
    } else if (k === 'choices') {
      out.choices = v.map(block => (Array.isArray(block) ? block.map(encodeReg) : block))
    } else {
      out[k] = setToArr(v)
    }
  }
  return out
}

// The order rules are applied in comes from buildNet's hook map, not from the rule
// list: getHooks walks Object.keys(hooks) and concatenates each bucket, so a rule
// hooked on an early key runs before a lower-indexed rule hooked on a later one.
// 'read back' depends on this - charge-back must land after look-what.
const net = world.methods.one.buildNet(model.two.matches, world)
const ruleIndex = new Map()
model.two.matches.forEach((m, i) => ruleIndex.set(m, i))
const hookOrder = Object.keys(net.hooks).map(key => ({
  key,
  rules: net.hooks[key].map(r => ruleIndex.get(r)).filter(i => i !== undefined),
}))
const alwaysRules = net.always.map(r => ruleIndex.get(r)).filter(i => i !== undefined)

const matches = model.two.matches.map(m => ({
  match: m.match,
  tag: m.tag,
  unTag: m.unTag,
  group: m.group === undefined ? null : String(m.group),
  reason: m.reason,
  ifNo: m.ifNo,
  regs: m.regs.map(encodeReg),
  notIf: m.notIf ? m.notIf.map(encodeReg) : null,
  needs: m.needs,
  wants: m.wants,
  minWant: m.minWant,
  minWords: m.minWords,
}))

const tagSet = {}
for (const [k, v] of Object.entries(model.one.tagSet)) {
  tagSet[k] = {
    is: v.is || '',
    not: v.not || [],
    parents: v.parents || [],
    children: v.children || [],
  }
}

const regexList = list => list.map(r => ({ regex: r[0].source, flags: r[0].flags, tag: r[1], reason: r[2] || '' }))

const endsWith = {}
for (const [k, v] of Object.entries(model.two.endsWith)) {
  endsWith[k] = regexList(v)
}

const write = (name, data) => {
  writeFileSync(new URL(name, OUT), JSON.stringify(data))
}

write('lexicon.json', model.one.lexicon)
write('tagset.json', tagSet)
write('matches.json', matches)
write('hooks.json', { hookOrder, always: alwaysRules })
write('misc.json', {
  version,
  suffixPatterns: model.two.suffixPatterns,
  prefixPatterns: model.two.prefixPatterns,
  endsWith,
  regexText: regexList(model.two.regexText),
  regexNormal: regexList(model.two.regexNormal),
  regexNumbers: regexList(model.two.regexNumbers),
  switches: model.two.switches,
  clues: model.two.clues,
  neighbours: model.two.neighbours,
  multiCache: model.one._multiCache,
  contractions: model.one.contractions,
  numberSuffixes: model.one.numberSuffixes,
  abbreviations: model.one.abbreviations,
  prefixes: model.one.prefixes,
  suffixes: model.one.suffixes,
  emoticons: model.one.emoticons,
  aliases: model.one.aliases,
  prePunctuation: model.one.prePunctuation,
  postPunctuation: model.one.postPunctuation,
  unicode: model.one.unicode,
  frozenLex: model.one.frozenLex,
  orgWords: model.two.orgWords,
  placeWords: model.two.placeWords,
  uncountable: model.two.uncountable,
  irregularPlurals: model.two.irregularPlurals,
  models: model.two.models,
})

console.log('wrote model data for compromise', version)
