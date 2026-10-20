export const replayGroups = ['原规则', '量比 1.5', '原规则 · 双倍成本', '量比 1.5 · 双倍成本']
export const replayStorageKey = 'astock.frozen-replay.v1'
export const alertStorageKey = 'astock.replay-price-alerts.v1'

export function movingAverage(bars, period) {
  let sum = 0
  return bars.flatMap((bar, index) => {
    sum += bar.close
    if (index >= period) sum -= bars[index - period].close
    return index < period - 1 ? [] : [{ time: bar.time, value: sum / period }]
  })
}

export function replayTrades(run, through) {
  if (!run) return []
  const closed = (run.trades || []).filter(t => t.entry.date <= through && t.exit.date <= through).map(t => ({ ...t, key: `${t.symbol}:${t.entry_signal.date}`, state: 'closed', profit: t.net_profit }))
  const open = (run.open_positions || []).filter(t => t.entry.date <= through).map(t => ({ ...t, key: `${t.symbol}:${t.entry_signal.date}`, state: 'open', profit: t.unrealized_profit, exit: null }))
  return [...closed, ...open].sort((a, b) => b.entry.date.localeCompare(a.entry.date) || a.symbol.localeCompare(b.symbol))
}

export function replayMarkers(rows, symbol, through, selectedKey) {
  const markers = []
  rows.filter(t => t.symbol === symbol).forEach(t => {
    const active = t.key === selectedKey
    for (const [kind, date, color, shape, position, text] of [
      ['signal', t.entry_signal.date, '#efb755', 'circle', 'belowBar', '信号'],
      ['buy', t.entry.date, '#ef6b6b', 'arrowUp', 'belowBar', '买'],
      ['sell', t.exit?.date, '#48c5a0', 'arrowDown', 'aboveBar', '卖'],
    ]) if (date && date <= through) markers.push({ id: `${t.key}:${kind}`, time: date, color, shape, position, text: active ? text : '', size: active ? 1.2 : .8, tradeKey: t.key })
  })
  return markers.sort((a, b) => a.time.localeCompare(b.time) || a.id.localeCompare(b.id))
}

export function relativeSeries(stock, benchmark, start) {
  const index = new Map(benchmark.map(b => [b.time, b.close]))
  const shared = stock.filter(b => b.time >= start && index.has(b.time))
  if (!shared.length) return { stock: [], benchmark: [] }
  const first = shared[0]
  return {
    stock: shared.map(b => ({ time: b.time, value: (b.close / first.close - 1) * 100 })),
    benchmark: shared.map(b => ({ time: b.time, value: (index.get(b.time) / index.get(first.time) - 1) * 100 })),
  }
}

export function normalizeReplayLayout(value = {}) {
  return { weekly: value.weekly !== false, benchmark: value.benchmark !== false, ma: value.ma !== false, markers: value.markers !== false, ledger: value.ledger !== false }
}

export function filterReplaySymbols(symbols, run, search, state, watched, watchOnly) {
  const pending = new Map((run?.pending_orders || []).map(o => [o.symbol, o]))
  const held = new Set((run?.open_positions || []).map(p => p.symbol))
  const closed = new Set((run?.trades || []).map(t => t.symbol))
  const query = search.trim().toLowerCase()
  return symbols.filter(s => (!query || `${s.symbol} ${s.name}`.toLowerCase().includes(query)) && (!watchOnly || watched.has(s.symbol)) &&
    (state === 'all' || state === 'held' && held.has(s.symbol) || state === 'pending' && pending.has(s.symbol) || state === 'closed' && closed.has(s.symbol) || state === 'volume' && pending.get(s.symbol)?.signal.volume_ratio >= 1.5))
}

export function quoteTriggersAlert(alert, quote, now = Date.now()) {
  if (!alert.enabled || alert.fired_at || !quote || alert.expires_on < new Date(now + 8 * 3600000).toISOString().slice(0, 10)) return false
  const raw = String(quote.quote_time || '').replace(' ', 'T')
  const timestamp = Date.parse(/[Zz]|[+-]\d{2}:?\d{2}$/.test(raw) ? raw : raw + '+08:00')
  const price = Number(quote.current)
  if (!Number.isFinite(timestamp) || !Number.isFinite(price) || price <= 0 || now - timestamp > 90000 || now < timestamp) return false
  return alert.direction === 'above' ? price >= alert.price : price <= alert.price
}
