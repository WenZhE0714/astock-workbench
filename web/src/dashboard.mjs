export function finiteNumber(value) {
  if (value == null || typeof value === "boolean" || (typeof value === "string" && !value.trim())) return null
  const number = Number(value)
  return Number.isFinite(number) ? number : null
}

export function boardDistribution(items = []) {
  const values = items.map(item => finiteNumber(item && item.percent)).filter(value => value !== null).sort((a, b) => a - b)
  const buckets = [
    { label: "<-5%", tone: "down", test: v => v < -5 },
    { label: "-5~-3", tone: "down", test: v => v >= -5 && v < -3 },
    { label: "-3~-1", tone: "down", test: v => v >= -3 && v < -1 },
    { label: "-1~0", tone: "down", test: v => v >= -1 && v < 0 },
    { label: "0%", tone: "flat", test: v => v === 0 },
    { label: "0~1", tone: "up", test: v => v > 0 && v < 1 },
    { label: "1~3", tone: "up", test: v => v >= 1 && v < 3 },
    { label: "3~5", tone: "up", test: v => v >= 3 && v < 5 },
    { label: "≥5%", tone: "up", test: v => v >= 5 },
  ].map(({ test, ...bucket }) => ({ ...bucket, count: values.filter(test).length }))
  const total = values.length
  return {
    total, missing: items.length - total, buckets,
    peak: Math.max(1, ...buckets.map(item => item.count)),
    rise: values.filter(value => value > 0).length,
    fall: values.filter(value => value < 0).length,
    flat: values.filter(value => value === 0).length,
    mean: total ? values.reduce((sum, value) => sum + value, 0) / total : null,
    median: total ? (values[Math.floor((total - 1) / 2)] + values[Math.floor(total / 2)]) / 2 : null,
  }
}

export function signalScore(signal) {
  const adjusted = finiteNumber(signal && signal.risk_adjusted_score)
  if (adjusted !== null) return adjusted
  // Go's older archives omit a zero adjusted score even when the overlay ran.
  if (signal && (signal.risk_multiplier > 0 || signal.cross_section_total > 0 || signal.tradable_total > 0 || signal.market_regime)) return 0
  return finiteNumber(signal && signal.score)
}

export function selectMonitorSignals(signals = [], state = "all", query = "") {
  const term = query.trim().toLowerCase()
  return signals.filter(signal => signal && signal.symbol && (state === "all" || signal.state === state)
    && (!term || [signal.symbol, signal.name, signal.industry].some(value => String(value || "").toLowerCase().includes(term))))
    .sort((a, b) => (Date.parse(b.as_of) || 0) - (Date.parse(a.as_of) || 0)
      || (signalScore(b) === null ? -1 : signalScore(b)) - (signalScore(a) === null ? -1 : signalScore(a)) || a.symbol.localeCompare(b.symbol))
}

export function radarPoint(index, value, count = 5) {
  const angle = -Math.PI / 2 + index * 2 * Math.PI / count
  const radius = 72 * value / 100
  return { x: 120 + Math.cos(angle) * radius, y: 108 + Math.sin(angle) * radius }
}

export function radarPolygon(values) {
  return values.map((value, index) => {
    const point = radarPoint(index, value, values.length)
    return `${point.x},${point.y}`
  }).join(" ")
}
