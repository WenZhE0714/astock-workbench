import assert from "assert"
import { planMonitorPhase, planMonitorHealth, planMonitorEventLabel, monitorCanToggle } from "../src/plan-monitor.mjs"

assert.strictEqual(planMonitorPhase(null), "未启用")
assert.strictEqual(planMonitorPhase({ enabled:true, phase:"waiting" }), "等待日线确认")
assert.strictEqual(planMonitorPhase({ enabled:false, phase:"confirmed" }), "已暂停")
assert.strictEqual(planMonitorPhase({ enabled:true, phase:"in_zone" }), "已进入观察区间")
assert.strictEqual(planMonitorPhase({ enabled:false, phase:"invalidated" }), "计划失效")
assert.strictEqual(planMonitorHealth({ data_status:"stale_history" }), "日K未更新")
assert.strictEqual(planMonitorHealth({ data_status:"price_basis_mismatch" }), "价格口径不一致")
assert.strictEqual(planMonitorEventLabel("confirmed"), "日线确认")
assert.strictEqual(monitorCanToggle(null, "2026-09-18", "2026-09-18"), true)
assert.strictEqual(monitorCanToggle(null, "2026-09-18", "2026-09-19"), false)
assert.strictEqual(monitorCanToggle({ enabled:true }, "2026-09-18", "2026-09-19"), true)
assert.strictEqual(monitorCanToggle({ enabled:false, phase:"invalidated" }, "2026-09-20", "2026-09-19"), false)
console.log("PASS: monitoring phases, freshness states and enable/pause controls")
