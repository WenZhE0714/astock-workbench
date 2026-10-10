import assert from 'node:assert/strict'
import { matchingTimeframes, timeframeAlignmentLabel, timeframeTrendLabel, timeframeRangeLabel } from '../src/chart-timeframes.mjs'

const analysis = {symbol:'sh600519',data_date:'2026-10-09',fingerprint:'valid'}
const report = {version:'timeframes-v1',symbol:analysis.symbol,data_date:analysis.data_date,base_fingerprint:analysis.fingerprint}
assert.equal(matchingTimeframes(report, analysis), report)
for (const change of [{symbol:'sz000001'}, {data_date:'2026-10-08'}, {base_fingerprint:'old'}, {version:'unknown'}]) assert.equal(matchingTimeframes({...report,...change}, analysis), null)
assert.equal(matchingTimeframes(null, analysis), null)
assert.equal(timeframeAlignmentLabel('conflict'), '周期冲突')
assert.equal(timeframeAlignmentLabel('insufficient'), '样本不足')
assert.equal(timeframeTrendLabel('sideways'), '整理')
assert.equal(timeframeRangeLabel('unavailable'), '区间样本不足')
console.log('PASS: timeframe snapshot binding, availability and direction labels')
