import { finiteNumber } from "./dashboard.mjs"

export const boardModules = [
  { id: "market", name: "板块横截面" }, { id: "trend", name: "资金趋势" },
  { id: "signals", name: "关联候选" }, { id: "holdings", name: "模拟持仓" }, { id: "stock", name: "个股联动" },
]
export const boardPresets = [
  { id: "intraday", name: "盘中盯盘", mode: "bubble", modules: ["market", "signals", "stock", "trend", "holdings"] },
  { id: "risk", name: "持仓风控", mode: "table", modules: ["market", "holdings", "stock", "signals", "trend"] },
  { id: "review", name: "收盘复盘", mode: "bubble", modules: ["market", "trend", "signals", "holdings", "stock"] },
]
export const boardLayoutKey = "astock.board-workbench.v1"

export function normalizeBoardLayout(input) {
  const known = new Set(boardModules.map(m => m.id))
  const modules = [...new Set(Array.isArray(input?.modules) ? input.modules : [])].filter(id => known.has(id))
  return { modules: modules.length ? modules : [...boardPresets[0].modules], wide: [...new Set(Array.isArray(input?.wide) ? input.wide : ["market"])].filter(id => known.has(id)), mode: input?.mode === "table" ? "table" : "bubble" }
}

export function boardBubbles(items) {
  const rows = (items || []).filter(item => finiteNumber(item.percent) !== null && finiteNumber(item.main_net_yuan) !== null)
  const maxX = Math.max(1, ...rows.map(item => Math.abs(Number(item.percent)))) * 1.15
  const maxY = Math.max(1e8, ...rows.map(item => Math.abs(Number(item.main_net_yuan)))) * 1.15
  return { maxX, maxY, points: rows.map(item => ({ ...item,
    x: 470 + Number(item.percent) / maxX * 390,
    y: 212 - Number(item.main_net_yuan) / maxY * 160,
    radius: 5 + 13 * Math.sqrt(Math.abs(Number(item.main_net_yuan)) / maxY),
  })) }
}

export function boardDailyStats(rows) {
  const sorted = [...(rows || [])].filter(row => finiteNumber(row.main_net_yuan) !== null).sort((a, b) => a.date.localeCompare(b.date))
  let streak = 0
  for (let index = sorted.length - 1; index >= 0 && sorted[index].main_net_yuan > 0; index--) streak++
  const sum = count => sorted.length >= count ? sorted.slice(-count).reduce((v, item) => v + item.main_net_yuan, 0) : null
  return { streak, sum3: sum(3), sum5: sum(5), count: sorted.length }
}

export function linkedBoardRows(rows, board, selectedSymbol = "") {
  if (!board) return []
  const industry = value => String(value || '').trim().replace(/[ⅠⅡⅢ]+$/u,'').trim()
  return (rows || []).filter(row => industry(row.industry) === industry(board.name) && (!selectedSymbol || row.symbol === selectedSymbol))
}

export function selectedSnapshot(snapshots, index, live) {
  if (!snapshots?.length) return null
  return snapshots[live ? snapshots.length - 1 : Math.max(0, Math.min(index, snapshots.length - 1))]
}

export function shanghaiDate(value = new Date()) { return new Intl.DateTimeFormat("sv-SE", { timeZone: "Asia/Shanghai", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date(value)) }

export function boardDailySession(value = new Date()) {
  const hour = Number(new Intl.DateTimeFormat('en-GB', {timeZone:'Asia/Shanghai', hour:'2-digit', hourCycle:'h23'}).format(new Date(value)))
  return `${shanghaiDate(value)}:${hour >= 15 ? 'closed' : 'intraday'}`
}
