import assert from "assert"
import { experimentComparison, experimentOperations, experimentTrialLabel, experimentReviewPlans, experimentReviewStatusLabel } from "../src/plan-experiment.mjs"

assert.strictEqual(experimentComparison(null), null)
const state = { started_at: "2026-09-21T09:30:00+08:00", valuation_complete: true, arms: [
  { id:"range", report:{ total_return_percent:1.25, total_profit:12500 } },
  { id:"confirmed", report:{ total_return_percent:.5, total_profit:5000 } },
] }
assert.deepStrictEqual(experimentComparison(state), {percentagePoints:.75, profit:7500})
assert.strictEqual(experimentComparison({...state,valuation_complete:false}), null)
assert.strictEqual(experimentComparison({...state,started_at:""}), null)
const operations = experimentOperations({report:{ orders:[{id:"a",execution_time:"2026-09-21 10:00:00",side:"buy",plan_id:"plan-a"}], rejections:[{order_id:"b",event_time:"2026-09-21 11:00:00",side:"sell",plan_id:"plan-b"}] }})
assert.deepStrictEqual(operations.map(item => item.id),["b","a"])
assert.strictEqual(operations[0].plan_id,"plan-b")
assert.strictEqual(experimentTrialLabel("pending_exit"),"等待退出")
const review = { plans: [
  { plan_id: "first", symbol: "sh600519", name: "贵州茅台", status: "paired_closed", structure_name: "区间突破" },
  { plan_id: "second", symbol: "sz000001", name: "平安银行", status: "single_sided", structure_name: "趋势回踩" },
] }
assert.strictEqual(experimentReviewPlans(null).length, 0)
assert.deepStrictEqual(experimentReviewPlans(review, "single_sided").map(item => item.plan_id), ["second"])
assert.strictEqual(experimentReviewPlans(review, "paired_closed", "银行").length, 0)
assert.strictEqual(experimentReviewPlans(review, "all", " 茅台 ")[0].plan_id, "first")
assert.strictEqual(experimentReviewPlans(review, "all", "600519").length, 1)
assert.strictEqual(experimentReviewStatusLabel("unavailable"), "账本待核对")
console.log("PASS: paired comparison availability, return difference, operation attribution and review filters")
