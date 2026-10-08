import assert from "assert"
import { matchingChartAnalysis, chartLevelRows, chartStructureState, chartWeeklyState, savedPlanState, chartDataLagNotice, chartPatternBias, chartStructureLines, chartStructureColor, chartStructureVisible, preferredChartStructureID } from "../src/chart-analysis.mjs"

const analysis = { symbol: "sh600519", data_date: "2026-09-18", fingerprint: "snapshot", levels: [
  { key: "ma20", label: "MA20", value: 49, basis: "MA20" },
  { key: "ma60", label: "MA60", value: null },
  { key: "gap", label: "缺口", value: null },
  { key: "range20", label: "前20日低 / 高", value: 45, upper: 50 },
] }
assert.strictEqual(matchingChartAnalysis(analysis, "sh600519", "2026-09-18"), analysis)
assert.strictEqual(matchingChartAnalysis(analysis, "sz000001", "2026-09-18"), null)
assert.strictEqual(matchingChartAnalysis(analysis, "sh600519", "2026-09-17"), null)
assert.strictEqual(matchingChartAnalysis(null, "sh600519", "2026-09-18"), null)
assert.strictEqual(matchingChartAnalysis({ ...analysis, fingerprint: "" }, "sh600519", "2026-09-18"), null)
const rows = chartLevelRows(analysis, value => Number(value).toFixed(2))
assert.deepStrictEqual(rows.map(row => row.value), ["49.00", "--", "无", "45.00 / 50.00"])
assert.deepStrictEqual(chartLevelRows(null, String), [])
assert.strictEqual(chartStructureState("forming"), "待确认")
assert.strictEqual(chartStructureState("confirmed"), "日线已确认")
assert.strictEqual(chartWeeklyState("insufficient"), "样本不足")
assert.strictEqual(savedPlanState({ expires_on: "2026-09-18" }, "2026-09-18"), "已保存 · 未启用监控")
assert.strictEqual(savedPlanState({ expires_on: "2026-09-18" }, "2026-09-19"), "已过期")
assert.strictEqual(chartDataLagNotice(analysis, "2026-09-18 15:00:00", true), "")
assert.strictEqual(chartDataLagNotice(analysis, "2026-09-21 15:00:00", false), "")
assert.strictEqual(chartDataLagNotice(analysis, "--", true), "")
assert(chartDataLagNotice(analysis, "2026-09-21 15:00:00", true).includes("不能视为最新交易日确认"))
const bars = Array.from({ length: 10 }, (_, index) => ({ date: `2026-09-${String(index + 1).padStart(2, "0")}` }))
const structure = { lines: [{ key: "trend", from: { date: bars[1].date, price: 10 }, to: { date: bars[8].date, price: 17 } }] }
const lines = chartStructureLines(structure, bars, 4, 7)
assert.strictEqual(lines.length, 1)
assert.strictEqual(lines[0].fromIndex, 0)
assert.strictEqual(lines[0].toIndex, 2)
assert.strictEqual(lines[0].fromPrice, 13)
assert.strictEqual(lines[0].toPrice, 15)
assert.strictEqual(chartStructureLines(structure, bars, 9, 10).length, 0)
assert.strictEqual(chartStructureLines(null, bars, 0, 5).length, 0)
assert.strictEqual(chartStructureLines(structure, bars, 0, 15).length, 0)
assert.strictEqual(chartPatternBias({ bias: "bearish" }), "看跌结构")
assert.strictEqual(chartPatternBias(null), "")

const structures = [
  { id: "range-breakout", state: "watching" },
  { id: "ma-pullback", state: "invalidated" },
  { id: "double-bottom", state: "confirmed", pattern: { bias: "bullish" } },
  { id: "ascending-triangle", state: "forming", pattern: { bias: "bullish" } },
]
assert.deepStrictEqual(structures.filter(item => chartStructureVisible(item, {})).map(item => item.id), ["range-breakout", "double-bottom", "ascending-triangle"])
const visibility = { "double-bottom": false, "ma-pullback": true }
assert.strictEqual(chartStructureVisible(structures[1], visibility), true)
assert.strictEqual(chartStructureVisible(structures[2], visibility), false)
assert.strictEqual(chartStructureVisible({ ...structures[2] }, visibility), false)
assert.strictEqual(chartStructureVisible({ ...structures[2], state: "invalidated" }, {}), false)
assert.strictEqual(chartStructureVisible({ ...structures[2], state: "invalidated" }, { "double-bottom": true }), true)
assert.strictEqual(chartStructureVisible(null, {}), false)
assert.strictEqual(chartStructureVisible(structures[1], Object.create({ "ma-pullback": true })), false)
assert.strictEqual(preferredChartStructureID(structures), "double-bottom")
assert.strictEqual(preferredChartStructureID(structures, "range-breakout"), "range-breakout")
assert.strictEqual(preferredChartStructureID(structures, "ma-pullback"), "ma-pullback")
assert.strictEqual(preferredChartStructureID(structures.slice(0, 2), "double-bottom"), "range-breakout")
assert.strictEqual(preferredChartStructureID([structures[1]]), "ma-pullback")
assert.strictEqual(preferredChartStructureID([], "double-bottom"), "")
assert.strictEqual(preferredChartStructureID(null), "")
assert.deepStrictEqual(structures.slice(0, 2).filter(item => chartStructureVisible(item, visibility)).map(item => item.id), ["range-breakout", "ma-pullback"])
const colors = ["range-breakout", "ma-pullback", "double-bottom", "double-top", "ascending-triangle", "descending-triangle"].map(id => chartStructureColor({ id }))
assert.strictEqual(new Set(colors).size, 6)
assert(colors.every(color => /^#[0-9a-f]{6}$/.test(color)))
assert.strictEqual(chartStructureColor(null), chartStructureColor({ id: "unknown" }))
console.log("PASS: chart dates, missing values, frozen plan selection, independent pattern layers and clipped structure geometry")
