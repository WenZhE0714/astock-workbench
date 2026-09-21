import assert from "assert"
import { experimentComparison, experimentOperations, experimentTrialLabel } from "../src/plan-experiment.mjs"

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
console.log("PASS: paired comparison availability, return difference and operation attribution")
