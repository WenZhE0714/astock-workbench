export function planMonitorPhase(state) {
  if (!state) return "未启用"
  if (state.phase === "expired") return "已过期"
  if (state.phase === "invalidated") return "计划失效"
  if (!state.enabled) return "已暂停"
  return { waiting: "等待日线确认", confirmed: "已确认 · 等待区间", in_zone: "已进入观察区间" }[state.phase] || "等待检查"
}

export function planMonitorHealth(state) {
  if (!state) return "未检查"
  return { waiting: "等待新报价", healthy: "最近校验通过", stale_quote: "报价过旧", stale_history: "日K未更新", price_basis_mismatch: "价格口径不一致", calendar_unavailable: "日历未覆盖", unavailable: "数据不可用", closed: "休市等待", paused: "已暂停", ended: "已结束" }[state.data_status] || "等待检查"
}

export function planMonitorEventLabel(kind) {
  return { enabled: "启用监控", resumed: "恢复监控", paused: "暂停监控", confirmed: "日线确认", zone_entered: "进入区间", invalidated: "计划失效", expired: "计划过期", data_wait: "等待数据", data_recovered: "数据恢复" }[kind] || "监控记录"
}

export function monitorCanToggle(state, expiresOn, today) {
  if (state && state.enabled) return true
  return !(state && (state.phase === "invalidated" || state.phase === "expired")) && Boolean(expiresOn && today && expiresOn >= today)
}

export const PlanMonitorDetails = {
  props: { monitor: { type: Object, required: true }, formatTime: { type: Function, required: true } },
  computed: {
    events() { return (this.monitor.events || []).slice().reverse() },
  },
  methods: { planMonitorHealth, planMonitorEventLabel },
  template: `<div class="plan-monitor-details">
    <div class="plan-monitor-health" :class="monitor.data_status"><strong>{{ planMonitorHealth(monitor) }}</strong><span>{{ monitor.data_message }}</span></div>
    <p class="plan-monitor-rule">{{ monitor.rule.description }}</p>
    <p class="plan-monitor-rule" v-if="monitor.calendar_basis">交易日依据：{{ monitor.calendar_basis }}</p>
    <dl class="plan-monitor-facts"><div><dt>最近检查</dt><dd>{{ formatTime(monitor.last_checked_at) }}</dd></div><div><dt>日线确认日</dt><dd>{{ monitor.confirmed_on || '--' }}</dd></div><div><dt>报价时间</dt><dd>{{ formatTime(monitor.quote_at) }}</dd></div><div><dt>完整日K</dt><dd>{{ monitor.history_date || '--' }}</dd></div></dl>
    <div class="plan-monitor-event-head"><strong>监控事件</strong><span>最近 {{ events.length }} / {{ monitor.sequence || 0 }} 条</span><a :href="'/api/trade-plan-monitors?plan_id=' + monitor.plan_id + '&download=1'" :download="'plan-monitor-' + monitor.plan_id.slice(0, 8) + '.json'">导出记录</a></div>
    <ol class="plan-monitor-events" v-if="events.length"><li v-for="event in events" :key="event.id" :class="event.kind"><div><strong>{{ planMonitorEventLabel(event.kind) }}</strong><time>{{ formatTime(event.observed_at) }}</time></div><p>{{ event.message }}</p><small v-if="event.data_date">{{ event.quote_at && !event.quote_at.startsWith('0001') ? '报价 ' + formatTime(event.quote_at) : '日K ' + event.data_date }}<span v-if="event.price != null"> · {{ Number(event.price).toFixed(2) }} 元</span></small></li></ol>
  </div>`,
}
