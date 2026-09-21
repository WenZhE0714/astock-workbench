import assert from "assert"
import { matchingChartAnalysis, chartLevelRows, chartStructureState, chartWeeklyState, savedPlanState, chartDataLagNotice } from "../src/chart-analysis.mjs"

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
console.log("PASS: chart date/symbol matching, missing values, fixed precision and saved-plan validity")
