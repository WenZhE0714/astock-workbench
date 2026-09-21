import { finiteNumber } from "./dashboard.mjs"

export function experimentTrialLabel(status) {
  return { waiting: "等待条件", pending_entry: "等待成交报价", rejected: "未成交", open: "持仓中", pending_exit: "等待退出", closed: "已平仓" }[status] || "等待条件"
}

export function experimentComparison(state) {
  if (!state || !state.started_at || state.started_at.startsWith("0001") || !state.valuation_complete || !Array.isArray(state.arms) || state.arms.length !== 2) return null
  const range = state.arms.find(arm => arm.id === "range")
  const control = state.arms.find(arm => arm.id === "confirmed")
  if (!range || !control) return null
  const left = finiteNumber(range.report.total_return_percent)
  const right = finiteNumber(control.report.total_return_percent)
  const leftProfit = finiteNumber(range.report.total_profit)
  const rightProfit = finiteNumber(control.report.total_profit)
  if ([left, right, leftProfit, rightProfit].some(value => value == null)) return null
  return { percentagePoints: left - right, profit: leftProfit - rightProfit }
}

export function experimentOperations(arm) {
  if (!arm || !arm.report) return []
  const orders = (arm.report.orders || []).map(order => ({ ...order, at: order.execution_time, kind: order.side === "buy" ? "模拟买入" : "模拟卖出" }))
  const rejections = (arm.report.rejections || []).map(item => ({ ...item, id: item.order_id, at: item.event_time, status: "rejected", kind: item.side === "buy" ? "买入拒单" : "卖出拒单" }))
  return orders.concat(rejections).sort((a, b) => String(b.at || "").localeCompare(String(a.at || "")))
}
