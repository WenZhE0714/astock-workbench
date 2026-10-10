export const alertStorageKey = "astock.local-alerts.v1"

export function emptyAlertState() {
  return { version: 1, initialized: false, enabled: false, enabledAt: 0, watermarks: {}, records: [] }
}

export function parseAlertState(raw) {
  if (!raw) return emptyAlertState()
  const value = JSON.parse(raw)
  if (value.version !== 1 || !Array.isArray(value.records) || !value.watermarks || typeof value.watermarks !== "object") throw new Error("提醒记录格式无效")
  return { ...emptyAlertState(), ...value, enabled: value.enabled === true, records: value.records.filter(item => item && typeof item.id === "string" && typeof item.message === "string").slice(0, 300) }
}

export function collectLocalAlerts(previous, monitors, now) {
  const state = { ...previous, watermarks: { ...previous.watermarks }, records: previous.records.map(item => ({ ...item })) }
  const ids = new Set(state.records.map(item => item.id))
  const incoming = []
  for (const monitor of monitors || []) {
    const watermark = Number(state.watermarks[monitor.plan_id] || 0)
    let nextWatermark = watermark
    for (const event of monitor.events || []) {
      nextWatermark = Math.max(nextWatermark, Number(event.sequence) || 0)
      if (!event.id || event.sequence <= watermark || ids.has(event.id) || !(event.notify || (event.kind === "data_wait" && monitor.enabled))) continue
      const observed = Date.parse(event.observed_at)
      const recent = previous.initialized && Number.isFinite(observed) && now - observed <= 5 * 60000 && observed <= now + 60000
      const record = { id: event.id, planID: monitor.plan_id, symbol: monitor.symbol, name: monitor.name, kind: event.kind, message: event.message,
        observedAt: event.observed_at, quoteAt: event.quote_at || "", dataDate: event.data_date || "", read: !previous.initialized, handled: false,
        delivery: recent ? "pending" : "history", notifiedAt: 0 }
      state.records.push(record)
      ids.add(event.id)
      if (recent && state.enabled && observed >= state.enabledAt) incoming.push(record)
    }
    state.watermarks[monitor.plan_id] = Math.max(nextWatermark, Number(monitor.sequence) || 0)
  }
  state.initialized = true
  state.records.sort((a, b) => (Date.parse(b.observedAt) || 0) - (Date.parse(a.observedAt) || 0) || a.id.localeCompare(b.id))
  state.records = state.records.slice(0, 300)
  return { state, incoming: incoming.filter(item => state.records.some(record => record.id === item.id)) }
}

export function deliveryCandidates(state, incoming, now) {
  const selected = []
  const last = new Map()
  for (const item of state.records) {
    const key = `${item.planID}:${item.kind}`
    last.set(key, Math.max(last.get(key) || 0, item.notifiedAt || 0))
  }
  for (const item of incoming.slice().sort((a, b) => Date.parse(b.observedAt) - Date.parse(a.observedAt))) {
    const key = `${item.planID}:${item.kind}`
    if (now - (last.get(key) || 0) < 5 * 60000) { item.delivery = "cooldown"; continue }
    item.notifiedAt = now
    item.delivery = "shown"
    last.set(key, now)
    selected.push(item)
  }
  return selected
}
