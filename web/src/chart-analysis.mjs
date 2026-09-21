export function matchingChartAnalysis(analysis, symbol, date) {
  if (!analysis || !symbol || !date || analysis.symbol !== symbol || analysis.data_date !== date || !analysis.fingerprint) return null
  return analysis
}

export function chartLevelRows(analysis, format) {
  return ((analysis && analysis.levels) || []).map(level => ({
    key: level.key,
    label: level.label,
    basis: [level.basis, level.date].filter(Boolean).join(" · "),
    value: level.value == null ? (level.key === "gap" ? "无" : "--") : `${format(level.value)}${level.upper == null ? "" : ` / ${format(level.upper)}`}`,
  }))
}

export function chartStructureState(state) {
  return { watching: "观察中", forming: "待确认", confirmed: "日线已确认", invalidated: "结构失效" }[state] || "--"
}

export function chartWeeklyState(state) {
  return { bullish: "偏多", bearish: "偏空", sideways: "整理", insufficient: "样本不足" }[state] || "--"
}

export function savedPlanState(plan, today) {
  return plan && plan.expires_on && today && plan.expires_on < today ? "已过期" : "已保存 · 未启用监控"
}

export function chartDataLagNotice(analysis, quoteTime, latestVisible) {
  const match = String(quoteTime || "").match(/^\d{4}-\d{2}-\d{2}/)
  if (!latestVisible || !analysis || !match || !analysis.data_date || analysis.data_date >= match[0]) return ""
  return `行情交易日 ${match[0]}，日K仅截至 ${analysis.data_date}；当前展示历史结构，不能视为最新交易日确认。`
}
