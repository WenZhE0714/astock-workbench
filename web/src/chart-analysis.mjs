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

export function chartPatternBias(pattern) {
  return pattern && pattern.bias === "bearish" ? "看跌结构" : pattern && pattern.bias === "bullish" ? "看涨结构" : ""
}

const structureColors = {
  "range-breakout": "#70bfd5",
  "ma-pullback": "#d5cc93",
  "double-bottom": "#f0b768",
  "double-top": "#e9a0a6",
  "ascending-triangle": "#81cdb0",
  "descending-triangle": "#b5b4f1",
  "head-shoulders-bottom": "#c9d96f",
  "head-shoulders-top": "#ed91ce",
}

export function chartStructureColor(structure) {
  return (structure && structureColors[structure.id]) || "#b6c4ca"
}

export function chartStructureVisible(structure, visibility) {
  if (!structure || !structure.id) return false
  if (visibility && Object.prototype.hasOwnProperty.call(visibility, structure.id)) return visibility[structure.id] === true
  return structure.state !== "invalidated"
}

export function preferredChartStructureID(structures, currentID = "") {
  const items = Array.isArray(structures) ? structures : []
  const selected = items.find(item => item.id === currentID)
    || items.find(item => item.pattern && item.state === "confirmed")
    || items.find(item => item.pattern && item.state !== "invalidated")
    || items.find(item => item.state !== "invalidated")
    || items[0]
  return selected ? selected.id : ""
}

// Clip dated structure geometry in trading-session coordinates, including
// lines whose original anchors are outside the current viewport.
export function chartStructureLines(structure, bars, startIndex, endIndex) {
  if (!structure || !Array.isArray(structure.lines) || !Array.isArray(bars) || startIndex < 0 || endIndex <= startIndex || endIndex > bars.length) return []
  const indices = new Map(bars.map((bar, index) => [bar.date, index]))
  return structure.lines.map(line => {
    if (!line.from || !line.to) return null
    const from = indices.get(line.from.date), to = indices.get(line.to.date)
    const fromPrice = Number(line.from.price), toPrice = Number(line.to.price)
    if (from == null || to == null || to <= from || !Number.isFinite(fromPrice) || !Number.isFinite(toPrice) || fromPrice <= 0 || toPrice <= 0) return null
    const first = Math.max(from, startIndex), last = Math.min(to, endIndex - 1)
    if (last <= first) return null
    const priceAt = index => fromPrice + (toPrice - fromPrice) * (index - from) / (to - from)
    return { ...line, fromIndex: first - startIndex, toIndex: last - startIndex, fromPrice: priceAt(first), toPrice: priceAt(last) }
  }).filter(Boolean)
}
