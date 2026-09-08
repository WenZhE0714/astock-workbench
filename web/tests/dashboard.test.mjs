import assert from "assert"
import { finiteNumber, boardDistribution, signalScore, selectMonitorSignals } from "../src/dashboard.mjs"

for (const value of [null, undefined, "", " ", "--", true, NaN, Infinity, -Infinity]) assert.strictEqual(finiteNumber(value), null)
assert.strictEqual(finiteNumber(0), 0)
assert.strictEqual(finiteNumber("0.00"), 0)

const input = [-6, -5, -3, -1, -0.1, 0, 0.1, 1, 3, 5, null, "--"].map(percent => ({ percent }))
const breadth = boardDistribution(input)
assert.strictEqual(breadth.total, 10)
assert.strictEqual(breadth.missing, 2)
assert.deepStrictEqual(breadth.buckets.map(item => item.count), [1, 1, 1, 2, 1, 1, 1, 1, 1])
assert.strictEqual(breadth.rise + breadth.fall + breadth.flat, breadth.total)
assert.strictEqual(breadth.buckets.reduce((sum, item) => sum + item.count, 0), breadth.total)
assert.strictEqual(boardDistribution([]).mean, null)
assert.strictEqual(boardDistribution([{ percent: null }]).median, null)

const signals = [
  { symbol: "sh600000", name: "浦发银行", state: "watching", score: 95, risk_adjusted_score: 0, as_of: "2026-09-07T10:00:00+08:00" },
  { symbol: "sz000001", name: "平安银行", state: "triggered", score: 70, as_of: "2026-09-07T10:01:00+08:00" },
  { symbol: "sh600519", name: "贵州茅台", state: "invalid", score: 80, market_regime: "震荡", as_of: "2026-09-07T10:00:00+08:00" },
]
assert.strictEqual(signalScore(signals[0]), 0)
assert.strictEqual(signalScore(signals[2]), 0)
assert.strictEqual(signalScore(signals[1]), 70)
const original = JSON.stringify(signals)
assert.strictEqual(selectMonitorSignals(signals)[0].symbol, "sz000001")
assert.strictEqual(selectMonitorSignals(signals, "triggered", "银行").length, 1)
assert.strictEqual(selectMonitorSignals(signals, "all", "600519")[0].state, "invalid")
assert.strictEqual(selectMonitorSignals(signals, "watching", "茅台").length, 0)
assert.strictEqual(JSON.stringify(signals), original)
console.log("PASS: missing data, distribution boundaries, signal ordering and filters")
