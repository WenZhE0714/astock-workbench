import { ArrowUpRight, Download } from "lucide-vue-next"
import { finiteNumber } from "./dashboard.mjs"
import { experimentComparison, experimentOperations, experimentTrialLabel } from "./plan-experiment.mjs"

export const PlanExperimentView = {
  components: { ArrowUpRight, Download },
  props: { state: Object, runtime: Object, config: Object, monitors: Array, busy: Boolean, error: String },
  emits: ["configure", "open-stock"],
  data() { return { selectedArm: "range", detailView: "positions", hovered: null, resizeObserver: null } },
  computed: {
    arms() { return this.state?.arms || [] },
    arm() { return this.arms.find(item => item.id === this.selectedArm) || this.arms[0] || null },
    report() { return this.arm?.report || {} },
    comparison() { return experimentComparison(this.state) },
    operations() { return experimentOperations(this.arm) },
    plans() {
      const selections = this.state?.selections || {}
      const result = new Map((this.monitors || []).map(item => [item.plan_id, { id: item.plan_id, symbol: item.symbol, name: item.name, monitorEnabled: item.enabled, expires: item.expires_on }]))
      Object.entries(selections).forEach(([id, item]) => {
        const existing = result.get(id)
        result.set(id, { id, symbol: item.plan.symbol, name: item.plan.structure.name, expires: item.plan.expires_on, monitorEnabled: existing?.monitorEnabled || false, selected: item.enabled })
      })
      return [...result.values()]
    },
    points() { return this.state?.equity || [] },
  },
  watch: {
    state: { handler() { this.$nextTick(() => this.draw()) }, deep: false },
  },
  methods: {
    experimentTrialLabel,
    number(value, digits = 2) { const number = finiteNumber(value); return number == null ? "--" : number.toFixed(digits) },
    money(value) { const number = finiteNumber(value); return number == null ? "--" : number.toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 }) },
    tone(value) { return value > 0 ? "up" : value < 0 ? "down" : "flat" },
    time(value) { if (!value || String(value).startsWith("0001")) return "--"; const date = new Date(String(value).includes("T") ? value : String(value).replace(" ", "T") + "+08:00"); return Number.isNaN(date.getTime()) ? "--" : date.toLocaleString("zh-CN", { timeZone: "Asia/Shanghai", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }) },
    planName(id) { return this.state?.selections?.[id]?.plan?.structure?.name || "--" },
    trial(id, arm = this.arm) { return arm?.trials?.[id] || {} },
    configureEntries(event) { this.$emit("configure", { action: "entries", enabled: event.target.checked }, event.target) },
    configurePlan(plan, event) { this.$emit("configure", { action: "select", symbol: plan.symbol, plan_id: plan.id, enabled: event.target.checked }, event.target) },
    pointer(event) {
      if (!this.points.length) return
      const rect = this.$refs.chart.getBoundingClientRect()
      const ratio = Math.max(0, Math.min(1, (event.clientX - rect.left - 48) / Math.max(1, rect.width - 64)))
      this.hovered = this.points[Math.round(ratio * (this.points.length - 1))]
    },
    draw() {
      const canvas = this.$refs.chart
      if (!canvas) return
      const rect = canvas.getBoundingClientRect()
      const width = rect.width, height = rect.height, dpr = window.devicePixelRatio || 1
      canvas.width = Math.round(width * dpr); canvas.height = Math.round(height * dpr)
      const ctx = canvas.getContext("2d"); ctx.scale(dpr, dpr)
      ctx.clearRect(0, 0, width, height)
      ctx.font = '11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      ctx.fillStyle = "#91a0a7"
      if (!this.points.length) { ctx.fillText("等待首个有效报价", 18, 35); return }
      const initial = this.arms[0]?.report?.initial_cash || this.config.initial_cash
      const series = this.points.map(point => ({ ...point, range: (point.range_equity / initial - 1) * 100, confirmed: (point.confirmed_equity / initial - 1) * 100 }))
      const values = series.flatMap(point => [point.range, point.confirmed])
      const low = Math.min(0, ...values) - .15, high = Math.max(0, ...values) + .15
      const left = 48, right = width - 16, top = 14, bottom = height - 30
      const x = index => left + index / Math.max(1, series.length - 1) * (right - left)
      const y = value => bottom - (value - low) / (high - low) * (bottom - top)
      ctx.textAlign = "right"
      for (let index = 0; index < 4; index++) {
        const value = low + (high - low) * index / 3
        ctx.strokeStyle = "#2b363c"; ctx.beginPath(); ctx.moveTo(left, y(value)); ctx.lineTo(right, y(value)); ctx.stroke()
        ctx.fillText(`${value.toFixed(2)}%`, left - 7, y(value) + 4)
      }
      ctx.setLineDash([3, 4]); ctx.strokeStyle = "#91a0a7"; ctx.beginPath(); ctx.moveTo(left, y(0)); ctx.lineTo(right, y(0)); ctx.stroke(); ctx.setLineDash([])
      for (const [key, color] of [["range", "#58b9d7"], ["confirmed", "#f0b768"]]) {
        ctx.strokeStyle = color; ctx.lineWidth = 2; ctx.beginPath()
        series.forEach((point, index) => index ? ctx.lineTo(x(index), y(point[key])) : ctx.moveTo(x(index), y(point[key])))
        ctx.stroke(); ctx.fillStyle = color
        const last = series[series.length - 1]; ctx.beginPath(); ctx.arc(x(series.length - 1), y(last[key]), 3, 0, Math.PI * 2); ctx.fill()
      }
      ctx.fillStyle = "#91a0a7"; ctx.textAlign = "left"; ctx.fillText(this.time(series[0].at).slice(0, -3), left, height - 8)
      if (width > 330) { ctx.textAlign = "right"; ctx.fillText(this.time(series[series.length - 1].at).slice(0, -3), right, height - 8) }
    },
  },
  mounted() { this.resizeObserver = new ResizeObserver(() => this.draw()); this.resizeObserver.observe(this.$refs.chart); this.draw() },
  beforeUnmount() { this.resizeObserver?.disconnect() },
  template: `<section class="plan-experiment" aria-label="双组影子实验">
    <div class="experiment-toolbar"><label class="plan-monitor-switch"><input type="checkbox" :checked="!!state?.entries_enabled" :disabled="busy || !runtime?.supported" aria-label="允许实验新开仓" @change="configureEntries"><span>允许新开仓</span></label><span class="experiment-status">{{ state?.message || '实验尚未初始化' }}</span><a class="icon-button" href="/api/plan-experiment?download=1" download="plan-experiment.json" title="导出实验账本" aria-label="导出实验账本"><download :size="16" /></a></div>
    <p class="chart-plan-error" v-if="error || runtime?.error" role="alert">{{ error || runtime.error }}</p>
    <div class="experiment-policy"><span>每组初始 {{ money(config?.initial_cash) }} 元</span><span>单股 {{ config?.max_position_percent }}%</span><span>现金预留 {{ config?.cash_reserve_percent }}%</span><span>每日新增 ≤ {{ config?.max_daily_deployment_percent }}%</span><span>未分类行业共用 {{ config?.max_industry_percent }}% 额度</span><span>T+1 · 下一报价撮合 · 单计划单次开仓</span></div>
    <div class="experiment-mobile-balances"><div v-for="item in arms" :key="item.id"><div class="experiment-mobile-profit"><span>{{ item.name }}</span><strong :class="tone(item.report.total_profit)">{{ money(item.report.total_profit) }} 元</strong><small :class="tone(item.report.total_return_percent)">{{ number(item.report.total_return_percent) }}%</small></div><dl><div><dt>总资产</dt><dd>{{ money(item.report.total_equity) }}</dd></div><div><dt>可用资金</dt><dd>{{ money(item.report.remaining_cash) }}</dd></div><div><dt>持仓市值</dt><dd>{{ money(item.report.total_market_value) }}</dd></div><div><dt>费用 / 最大回撤</dt><dd>{{ money(item.report.total_fees) }} / {{ number(item.max_drawdown_percent) }}%</dd></div></dl></div></div>
    <div class="experiment-summary-wrap"><table class="experiment-summary"><thead><tr><th>实验组</th><th>总资产</th><th>总收益</th><th>收益率</th><th>可用资金</th><th>持仓市值</th><th>费用</th><th>最大回撤</th></tr></thead><tbody><tr v-for="item in arms" :key="item.id"><th>{{ item.name }}</th><td>{{ money(item.report.total_equity) }}</td><td :class="tone(item.report.total_profit)"><strong>{{ money(item.report.total_profit) }}</strong></td><td :class="tone(item.report.total_return_percent)">{{ number(item.report.total_return_percent) }}%</td><td>{{ money(item.report.remaining_cash) }}</td><td>{{ money(item.report.total_market_value) }}</td><td>{{ money(item.report.total_fees) }}</td><td>{{ number(item.max_drawdown_percent) }}%</td></tr></tbody></table></div>
    <div class="experiment-comparison"><div><span>区间组 − 对照组</span><strong :class="tone(comparison?.profit)">{{ comparison ? money(comparison.profit) + ' 元' : '--' }}</strong><b>{{ comparison ? number(comparison.percentagePoints) + ' 个百分点' : '等待完整估值' }}</b></div><span>起点 {{ time(state?.started_at) }} · 估值 {{ time(state?.valued_at) }}</span></div>
    <figure class="experiment-chart"><figcaption><strong>同窗收益</strong><span class="range">区间条件组</span><span class="confirmed">仅确认对照组</span></figcaption><canvas ref="chart" aria-label="双组收益曲线" @pointermove="pointer" @pointerleave="hovered = null"></canvas><div class="experiment-chart-readout"><template v-if="hovered">{{ time(hovered.at) }} · 区间 {{ money(hovered.range_equity) }} · 对照 {{ money(hovered.confirmed_equity) }}</template><template v-else>{{ state?.valuation_complete ? '当前估值完整' : '等待全部持仓的有效报价' }}</template></div></figure>
    <div class="experiment-section-head"><h2>参与计划</h2><span>{{ plans.filter(plan => plan.selected).length }} 个已纳入</span></div>
    <div class="experiment-plan-list"><div class="experiment-plan-row" v-for="plan in plans" :key="plan.id"><label class="plan-monitor-switch"><input type="checkbox" :checked="!!plan.selected" :disabled="busy || (!plan.selected && !plan.monitorEnabled)" :aria-label="'纳入实验 ' + plan.symbol + ' ' + plan.id.slice(0, 8)" @change="configurePlan(plan, $event)"><span>{{ plan.symbol.slice(2) }} · {{ plan.name }}</span></label><span v-for="item in arms" :key="item.id" :title="trial(plan.id, item).reason">{{ item.name }}：{{ experimentTrialLabel(trial(plan.id, item).status) }}</span><small>{{ trial(plan.id).reason || (plan.monitorEnabled ? '等待纳入实验' : '监控未启用') }}</small></div><p class="chart-analysis-empty" v-if="!plans.length">暂无可参与的计划</p></div>
    <div class="experiment-detail-controls"><div class="dashboard-segment" role="tablist" aria-label="实验账户"><button type="button" role="tab" v-for="item in arms" :key="item.id" :aria-selected="selectedArm === item.id" :aria-pressed="selectedArm === item.id" @click="selectedArm = item.id">{{ item.name }}</button></div><div class="dashboard-segment" aria-label="实验明细"><button type="button" :aria-pressed="detailView === 'positions'" @click="detailView = 'positions'">持仓 {{ report.open_positions || 0 }}</button><button type="button" :aria-pressed="detailView === 'operations'" @click="detailView = 'operations'">操作记录</button></div></div>
    <div class="experiment-table-wrap" v-if="detailView === 'positions'"><table class="experiment-detail-table"><thead><tr><th>股票 / 计划</th><th>浮动盈亏</th><th>持仓 / 可卖</th><th>成本 / 现价</th><th>开仓时间</th><th>失效 / 2R目标</th><th>状态</th></tr></thead><tbody><tr v-for="position in (report.positions || [])" :key="position.signal_id"><td><button type="button" class="plan-monitor-stock" @click="$emit('open-stock', {symbol:position.symbol})">{{ position.symbol.slice(2) }}<arrow-up-right :size="13" /></button><small>{{ planName(position.signal_id) }}</small></td><td :class="tone(position.unrealized_profit)"><strong>{{ money(position.unrealized_profit) }} 元</strong><small>{{ number(position.unrealized_return_percent) }}%</small></td><td>{{ position.quantity }} / {{ position.available_quantity }}</td><td>{{ number(position.entry_price) }} / {{ number(position.last_price) }}</td><td>{{ time(position.entry_time) }}</td><td>{{ number(position.invalidation_price) }} / {{ number(state.selections[position.signal_id]?.plan.structure.plan.target_2) }}</td><td>{{ experimentTrialLabel(trial(position.signal_id).status) }}<small>{{ trial(position.signal_id).reason }}</small></td></tr></tbody></table><p class="chart-analysis-empty" v-if="!(report.positions || []).length">暂无实验持仓</p></div>
    <div class="experiment-table-wrap" v-else><table class="experiment-detail-table"><thead><tr><th>执行时间</th><th>股票 / 计划</th><th>操作</th><th>数量</th><th>模拟成交价</th><th>依据</th></tr></thead><tbody><tr v-for="operation in operations" :key="operation.id"><td>{{ time(operation.at) }}</td><td>{{ operation.symbol.slice(2) }}<small>{{ planName(operation.plan_id) }}</small></td><td :class="operation.status === 'rejected' ? 'experiment-rejected' : ''">{{ operation.kind }}</td><td>{{ operation.quantity || '--' }}</td><td>{{ number(operation.price) }}</td><td>{{ operation.reason }}</td></tr></tbody></table><p class="chart-analysis-empty" v-if="!operations.length">暂无模拟操作</p></div>
  </section>`,
}
