import assert from 'node:assert/strict'
import { decisionSummary } from '../src/plan-decision.mjs'

const input = { analysis: { data_date: '2026-10-09', complete: true, source: 'live' }, structure: { state: 'confirmed', plan: { entry_low: 10, entry_high: 11, invalidation: 9 } }, quote: { current: '10.5', quote_time: '2026-10-09 10:00:00' }, session: 'trading', today: '2026-10-09', now: Date.parse('2026-10-09T10:01:00+08:00') }
assert.equal(decisionSummary(input).state, 'in_zone')
assert.equal(decisionSummary({ ...input, historical: true }).state, 'history')
assert.equal(decisionSummary({ ...input, historyError: 'offline' }).state, 'stale')
assert.equal(decisionSummary({ ...input, analysis: { ...input.analysis, source: '腾讯缓存' } }).blocksMonitoring, true)
assert.equal(decisionSummary({ ...input, quote: { ...input.quote, current: '8' } }).state, 'invalidated')
assert.equal(decisionSummary({ ...input, now: input.now + 600000 }).state, 'stale')
assert.equal(decisionSummary({ ...input, structure: { ...input.structure, state: 'watching' }, analysis: { ...input.analysis, complete: false } }).label, '等待完整日K确认')
console.log('PASS: decision freshness, historical isolation, invalidation and confirmation gates')
