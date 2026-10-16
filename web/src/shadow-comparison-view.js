import { ChartNoAxesCombined, RefreshCw, Download, ChevronDown, ArrowUpRight } from 'lucide-vue-next'

export const ShadowComparisonView = {
  components: { ChartNoAxesCombined, RefreshCw, Download, ChevronDown, ArrowUpRight },
  props: ['selection', 'refreshKey'],
  emits: ['selection', 'open-profile'],
  data() { return { start: this.selection?.start || '', end: this.selection?.end || '', selectedID: this.selection?.id || '', report: null, loading: false, error: '', controller: null, requestID: 0 } },
  computed: {
    requestKey() { return `${this.start}|${this.end}|${this.refreshKey}` },
    selected() { return this.report?.rows.find(row => row.id === this.selectedID) || this.report?.rows[0] },
    baseline() { return this.report?.rows.find(row => row.id === this.selected?.baseline_id) },
    downloadURL() { return this.report ? '/api/strategy/shadow-comparison?' + new URLSearchParams({start: this.report.start, end: this.report.end, download: '1'}) : '' },
    plot() {
      const points = this.selected?.points || [], baseline = new Map((this.baseline?.points || []).map(p => [p.date, p.return_percent]))
      const values = points.flatMap(p => [p.return_percent, baseline.get(p.date)]).filter(v => v != null && Number.isFinite(v))
      const min = Math.min(0, ...values), max = Math.max(0, ...values), pad = Math.max(.5, (max-min)*.15)
      const low = min-pad, high = max+pad
      const y = value => 190-(value-low)/(high-low)*180
      const series = pick => {
        let path = '', connected = false
        const dots = []
        points.forEach((p, i) => {
          const value = pick(p)
          if (value == null || !Number.isFinite(value)) { connected = false; return }
          const x = points.length > 1 ? 8+i/(points.length-1)*784 : 400, cy = y(value)
          path += `${connected ? 'L' : 'M'}${x},${cy} `; connected = true
          dots.push({x, y: cy, date: p.date, value})
        })
        return {path, dots}
      }
      return {account: series(p => p.return_percent), baseline: series(p => baseline.get(p.date)), low, high, zero: y(0), first: points[0]?.date, last: points.at(-1)?.date}
    },
  },
  watch: {
    requestKey: {immediate: true, handler() { this.load() }},
    selectedID() { this.remember() },
  },
  beforeUnmount() { this.requestID++; this.controller?.abort() },
  methods: {
    number(value, digits = 0) { return value == null || !Number.isFinite(Number(value)) ? '--' : Number(value).toLocaleString('zh-CN', {minimumFractionDigits: digits, maximumFractionDigits: digits}) },
    percent(value) { return value == null ? '--' : this.number(value, 2) + '%' },
    remember() { this.$emit('selection', {start: this.start, end: this.end, id: this.selectedID}) },
    async load() {
      this.controller?.abort(); const controller = new AbortController(); this.controller = controller
      const id = ++this.requestID
      this.loading = true; this.error = ''; this.report = null
      this.remember()
      try {
        const params = new URLSearchParams()
        if (this.start) params.set('start', this.start)
        if (this.end) params.set('end', this.end)
        const response = await fetch('/api/strategy/shadow-comparison?' + params, {cache: 'no-store', signal: controller.signal})
        const data = await response.json()
        if (id !== this.requestID) return
        if (!response.ok) throw new Error(data.error || '多日对照读取失败')
        this.report = data
        if (!this.start) this.start = data.start
        if (!this.end) this.end = data.end
        this.remember()
      } catch (error) { if (id === this.requestID && error.name !== 'AbortError') this.error = error.message }
      finally { if (id === this.requestID) this.loading = false }
    },
  },
  template: `<section class="shadow-comparison" aria-label="多日对照评估" :aria-busy="loading">
    <div class="sd-toolbar"><h3><chart-no-axes-combined :size="17"/>多日对照</h3><div class="sc-controls">
      <input type="date" v-model="start" :max="end || undefined" aria-label="对照开始日期"><span>至</span><input type="date" v-model="end" :min="start || undefined" :max="report?.today" aria-label="对照结束日期">
      <button type="button" class="icon-button" title="刷新多日对照" aria-label="刷新多日对照" :disabled="loading" @click="load"><refresh-cw :size="16"/></button>
      <a v-if="downloadURL" class="icon-button" :href="downloadURL" title="导出多日对照" aria-label="导出多日对照"><download :size="16"/></a>
    </div></div>
    <p v-if="error" class="sd-warning" role="alert">{{ error }}</p><p v-if="loading && !report" class="sd-empty">正在读取封存快照</p>
    <template v-if="report">
      <div class="sd-source"><span>{{ report.rows.length }}个优化对照</span><span>{{ report.start }} 至 {{ report.end }}</span><span>{{ report.calendar_known ? '交易日日历已覆盖' : '交易日日历覆盖不足' }}</span></div>
      <div v-if="report.warnings.length" class="sd-warnings"><p v-for="warning in report.warnings" :key="warning">{{ warning }}</p></div>
      <div class="sc-table" role="table" aria-label="多日账户对照">
        <div class="sc-row sc-heading" role="row"><span>账户 / 权益时点</span><span>封存日</span><span>起点累计收益</span><span>较基线（百分点）</span><span>区间收盘回撤</span><span>平仓批次</span><span>费用（元）</span><span></span></div>
        <div class="sc-row" role="row" v-for="row in report.rows" :key="row.id" :data-comparison-profile="row.id" :class="{selected: selected?.id === row.id}">
          <div class="sc-identity"><strong>{{ row.name }}</strong><small>{{ row.as_of || '尚无权益时点' }} · {{ row.status }}</small></div>
          <div><small>封存日</small><b>{{ row.snapshot_days }} / {{ report.calendar_known ? row.expected_days : '--' }}</b></div>
          <div><small>起点累计收益</small><b>{{ percent(row.return_percent) }}</b></div>
          <div><small>较基线（百分点）</small><b>{{ number(row.return_difference, 2) }}</b></div>
          <div><small>区间收盘回撤</small><b>{{ percent(row.max_drawdown_percent) }}</b></div>
          <div><small>平仓批次</small><b>{{ number(row.activity?.closed_trades) }}</b></div>
          <div><small>费用（元）</small><b>{{ number(row.activity?.fees, 2) }}</b></div>
          <button type="button" class="icon-button" :title="'查看' + row.name + '曲线'" :aria-label="'查看' + row.name + '曲线'" :aria-expanded="selected?.id === row.id" @click="selectedID = row.id"><chevron-down :size="16"/></button>
        </div>
      </div>
      <div class="sc-detail" v-if="selected">
        <div class="sd-detail-heading"><h4>{{ selected.name }} · 累计收益率</h4><button type="button" class="icon-button" title="打开当前账本" aria-label="打开对照账本" @click="$emit('open-profile', selected.id)"><arrow-up-right :size="16"/></button></div>
        <div class="sc-legend"><span class="sc-account-key">{{ selected.name }}</span><span v-if="baseline" class="sc-baseline-key">{{ baseline.name }}</span><span>{{ selected.status }}</span></div>
        <p v-if="selected.warnings.length" class="sd-warning">{{ selected.warnings.join('；') }}</p>
        <p v-if="selected.missing_dates.length" class="sd-warning" data-comparison-missing>缺失或不可用：{{ selected.missing_dates.join('、') }}</p>
        <div v-if="selected.snapshot_days" class="sc-chart" role="img" :aria-label="selected.name + '封存收益曲线'">
          <div class="sc-chart-labels"><span>{{ percent(plot.high) }}</span><span>{{ percent(plot.low) }}</span></div>
          <svg viewBox="0 0 800 200" preserveAspectRatio="none" aria-hidden="true"><line x1="0" :y1="plot.zero" x2="800" :y2="plot.zero" class="sc-zero"/>
            <path :d="plot.baseline.path" class="sc-baseline"/><path :d="plot.account.path" class="sc-account"/>
            <circle v-for="dot in plot.account.dots" :key="dot.date" :cx="dot.x" :cy="dot.y" r="3" class="sc-dot"><title>{{ dot.date }} {{ percent(dot.value) }}</title></circle>
          </svg>
          <div class="sc-chart-dates"><span>{{ plot.first }}</span><span>{{ plot.last }}</span></div>
        </div><p v-else class="sd-empty">尚无有效收盘快照</p>
        <dl class="sd-facts"><div><dt>规则</dt><dd>{{ selected.policy }}</dd></div><div><dt>区间成交笔数</dt><dd>{{ number(selected.activity?.orders) }}</dd></div><div><dt>平仓胜率</dt><dd>{{ percent(selected.activity?.win_rate_percent) }}</dd></div><div><dt>区间已实现净盈亏（元）</dt><dd>{{ number(selected.activity?.realized_profit, 2) }}</dd></div></dl>
      </div>
      <p v-if="!report.rows.length" class="sd-empty">尚无冻结对照账户</p>
    </template>
  </section>`,
}
