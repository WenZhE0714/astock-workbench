import { GitCompareArrows, RefreshCw } from 'lucide-vue-next'
import { matchingTimeframes, timeframeAlignmentLabel, timeframeTrendLabel, timeframeRangeLabel } from './chart-timeframes.mjs'
import { chartStructureState } from './chart-analysis.mjs'

export const ChartTimeframesView = {
  components: { GitCompareArrows, RefreshCw },
  props: ['analysis', 'structure', 'historical', 'limited'],
  emits: ['refresh'],
  data() { return { report: null, loading: false, error: '', needsChartRefresh: false, controller: null, requestID: 0 } },
  computed: {
    matched() { return matchingTimeframes(this.report, this.analysis) },
    reference() { return this.historical || this.limited || this.matched?.reference_only },
    rows() { return this.matched ? [this.matched.daily, this.matched.weekly] : [] },
    checks() { return (this.matched?.checks || []).filter(item => item.key !== 'alignment') },
  },
  watch: {
    analysis: {
      immediate: true,
      handler(current, previous) {
        if (this.needsChartRefresh || current?.symbol !== previous?.symbol || current?.data_date !== previous?.data_date || current?.fingerprint !== previous?.fingerprint) this.load()
      },
    },
  },
  methods: {
    timeframeAlignmentLabel, timeframeTrendLabel, timeframeRangeLabel, chartStructureState,
    number(value) { return value == null ? '--' : Number(value).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) },
    retry() { if (this.needsChartRefresh) this.$emit('refresh'); else this.load() },
    async load() {
      this.controller?.abort()
      const id = ++this.requestID
      this.report = null; this.error = ''; this.needsChartRefresh = false
      if (!this.analysis?.fingerprint) { this.loading = false; return }
      const analysis = this.analysis
      const controller = new AbortController()
      this.controller = controller; this.loading = true
      try {
        const query = new URLSearchParams({ symbol: analysis.symbol, through: analysis.data_date, fingerprint: analysis.fingerprint })
        const response = await fetch(`/api/chart-timeframes?${query}`, { cache: 'no-store', signal: controller.signal })
        const payload = await response.json()
        if (id !== this.requestID) return
        if (!response.ok) { this.needsChartRefresh = response.status === 409; throw new Error(payload.error || '周期对照读取失败') }
        if (!matchingTimeframes(payload, analysis)) {
          this.needsChartRefresh = true
          throw new Error('周期对照与当前图表不一致，请刷新结构分析')
        }
        this.report = payload
      } catch (error) {
        if (id === this.requestID && error.name !== 'AbortError') this.error = error.message || String(error)
      } finally { if (id === this.requestID) this.loading = false }
    },
  },
  beforeUnmount() { this.requestID++; this.controller?.abort() },
  template: `<section class="chart-timeframes" aria-label="周线与日线对照" :aria-busy="loading">
    <div class="timeframes-heading"><git-compare-arrows :size="16"/><h3>周线 / 日线对照</h3><strong :class="matched?.alignment" aria-live="polite">{{ loading ? '读取中' : timeframeAlignmentLabel(matched?.alignment) }}</strong><button type="button" class="icon-button" title="刷新周期对照" aria-label="刷新周期对照" :disabled="loading" @click="retry"><refresh-cw :size="14"/></button></div>
    <p class="chart-plan-error" v-if="error" role="alert">{{ error }}</p>
    <template v-if="matched">
      <div class="timeframes-basis"><span>{{ historical ? '历史回看' : reference ? '数据仅供参考' : '已收盘样本' }} · {{ matched.data_date }}</span><span v-if="matched.excluded_daily">已排除盘中日K</span><span>{{ matched.price_basis === 'unadjusted' ? '未复权' : matched.price_basis }}</span></div>
      <p class="timeframes-summary">{{ matched.summary }}</p>
      <div class="timeframe-row" v-for="row in rows" :key="row.timeframe" :data-timeframe="row.timeframe">
        <div class="timeframe-name"><strong>{{ row.timeframe === '1d' ? '日线' : '周线' }}</strong><span>{{ row.data_date || '--' }}</span><small>{{ row.samples }} / {{ row.required }} {{ row.timeframe === '1d' ? '根完整日K' : '个已结束周' }}</small><small v-if="row.timeframe === '1w' && row.period_days">最近一周含 {{ row.period_days }} 根日K</small><small v-if="row.period_end && row.period_end !== row.data_date">周边界 {{ row.period_end }}</small></div>
        <div class="timeframe-price"><span>收盘 / 方向</span><strong>{{ number(row.close) }}</strong><b :class="row.state">{{ timeframeTrendLabel(row.state) }}</b></div>
        <div class="timeframe-ma"><span>MA{{ row.fast_period }} / MA{{ row.slow_period }}</span><strong>{{ number(row.fast_ma) }} / {{ number(row.slow_ma) }}</strong><small>前{{ row.range_lookback }}{{ row.timeframe === '1d' ? '日' : '周' }}区间 {{ number(row.range_low) }} - {{ number(row.range_high) }}</small><small>{{ timeframeRangeLabel(row.range_state) }}</small></div>
        <div class="timeframe-volume"><span>完整周期量比</span><strong>{{ row.volume_ratio == null ? '--' : number(row.volume_ratio) + '倍' }}</strong><small>对比前{{ row.volume_lookback }}{{ row.timeframe === '1d' ? '日' : '周' }}均量</small></div>
      </div>
      <div class="timeframes-checks"><p v-for="check in checks" :key="check.key" :class="check.state"><span>{{ check.key === 'volume' ? '日线量能' : '日线位置' }}</span>{{ check.text }}</p><p v-if="structure"><span>所选形态</span>{{ structure.name }} · {{ chartStructureState(structure.state) }}</p></div>
      <details class="timeframes-method"><summary>判断口径与数据边界</summary><p>日线收盘高于 MA20 且 MA20 高于 MA60 时偏多；周线收盘高于 MA5 且 MA5 高于 MA10 时偏多。反向排列为偏空，其余为整理。</p><p v-for="warning in matched.warnings" :key="warning">{{ warning }}</p><p>来源：{{ matched.source || '--' }}。对照仅补充观察，计划触发仍以后台监控校验为准。</p></details>
    </template>
    <p class="timeframes-empty" v-else-if="loading">正在对齐已收盘的日线与周线</p>
  </section>`,
}
