// The page template is delivered by Go so the browser bundle needs Vue's
// runtime compiler, not the runtime-only default entry.
import { createApp } from "vue/dist/vue.esm-bundler.js"
import "./style.css"

const defaultSymbol = document.querySelector('meta[name="astock-default-symbol"]')?.content || "600519"
const morningStart = 9 * 60 + 30
const morningEnd = 11 * 60 + 30
const afternoonStart = 13 * 60
const afternoonEnd = 15 * 60
const morningSlots = morningEnd - morningStart + 1
const afternoonSlots = afternoonEnd - afternoonStart + 1
const totalSlots = morningSlots + afternoonSlots
const marketIndexDefinitions = [
  { symbol: "sh000001", name: "上证指数" },
  { symbol: "sz399001", name: "深证成指" },
  { symbol: "sz399006", name: "创业板指" },
]
const dailyRangeOptions = [
  { key: "1m", label: "1月", count: 20 },
  { key: "3m", label: "3月", count: 60 },
  { key: "6m", label: "6月", count: 120 },
  { key: "1y", label: "1年", count: 250 },
]
const localDate = value => {
  const date = new Date(value)
  const pad = number => String(number).padStart(2, "0")
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}
const strategyEndDate = new Date()
strategyEndDate.setDate(strategyEndDate.getDate() - 1)
const strategyStartDate = new Date(strategyEndDate)
strategyStartDate.setFullYear(strategyStartDate.getFullYear() - 3)

createApp({
  data() {
    return {
      query: defaultSymbol,
      requestedSymbol: defaultSymbol,
      data: {},
      workspaceMode: "market",
      realtimeScope: "leaders",
      realtimeLoading: false,
      realtimeSnapshotLoading: false,
      realtimeError: "",
      realtimeResult: null,
      realtimeHistory: [],
      realtimeAutoAt: 0,
      realtimeSessionCheckedAt: 0,
      realtimeMarketState: "",
      realtimeScanAllowed: false,
      realtimeFrozen: false,
      realtimeNextScanAt: "",
      realtimeOutcomeLoading: false,
      realtimeOutcomeError: "",
      realtimeOutcomeReport: null,
      realtimeOutcomeHorizon: 5,
      realtimeOutcomeView: "scores",
      realtimeSection: "candidates",
      shadowLoading: false,
      shadowError: "",
      shadowReport: null,
      shadowCheckedAt: 0,
      chartMode: "intraday",
      chartGeometry: null,
      crosshair: null,
      crosshairFrame: null,
      dailyRangeOptions,
      dailyRangePreset: "6m",
      dailyVisibleCount: 120,
      dailyEndIndex: null,
      chartPointers: new Map(),
      dailyPanState: null,
      dailyPinchState: null,
      dailyDragging: false,
      watchlistOpen: window.innerWidth > 1100,
      watchlist: { groups: [] },
      selectedWatchlistGroup: "全部",
      watchlistInput: "",
      watchlistLoading: false,
      watchlistRefreshing: false,
      watchlistRequestID: 0,
      watchlistError: "",
      marketIndices: marketIndexDefinitions.map(item => ({ ...item })),
      marketAmount: null,
      indicesLoading: false,
      indicesRequestID: 0,
      indicesError: "",
      loading: false,
      error: "",
      timer: null,
      strategyScope: "current",
      strategyAdvanced: false,
      strategyForm: {
        start: localDate(strategyStartDate), end: localDate(strategyEndDate), entry_mode: "breakout",
        fast_ma: 20, slow_ma: 60, breakout_days: 20, volume_ratio_min: 1.2,
        stop_loss_percent: 8, take_profit_percent: 20, max_holding_days: 40, max_position_percent: 20,
        initial_cash: 1000000, commission_bps: 3, stamp_duty_bps: 5, slippage_bps: 5,
      },
      strategyLoading: false,
      strategyError: "",
      strategyResult: null,
      strategyAssessment: null,
      strategyHistory: [],
      strategyHistoryLoading: false,
      strategyHistoryLoaded: false,
      strategyChartGeometry: null,
      strategyCrosshair: null,
    }
  },
  computed: {
    quote() { return this.data.quote || {} },
    board() { return this.data.board || null },
    isSector() { return this.data.kind === "sector" || String(this.data.symbol || "").toLowerCase().startsWith("bk") },
    bars() { return Array.isArray(this.data.bars) ? this.data.bars : [] },
    minutes() { return Array.isArray(this.data.minutes) ? this.data.minutes : [] },
    watchlistGroupNames() {
      return ["全部", ...this.watchlist.groups.map(group => group.name).filter(Boolean)]
    },
    watchlistItems() {
      if (this.selectedWatchlistGroup === "全部") {
        const items = []
        const seen = new Set()
        this.watchlist.groups.forEach(group => {
          ;(Array.isArray(group.items) ? group.items : []).forEach(item => {
            if (item && item.symbol && !seen.has(item.symbol)) {
              seen.add(item.symbol)
              items.push(item)
            }
          })
        })
        return items
      }
      const group = this.watchlist.groups.find(item => item.name === this.selectedWatchlistGroup)
      return group && Array.isArray(group.items) ? group.items : []
    },
    currentStrategyAsset() {
      const symbol = String(this.data.symbol || "")
      if (!symbol || this.data.kind !== "stock" || this.isMarketIndex(symbol)) return null
      return { symbol, name: this.quote.name || this.data.name || this.displayCode(symbol), kind: "stock" }
    },
    strategyPool() {
      if (this.strategyScope === "current") return this.currentStrategyAsset ? [this.currentStrategyAsset] : []
      return this.watchlistItems.filter(item => item && item.kind === "stock" && !this.isMarketIndex(item.symbol))
    },
    strategyMetrics() { return (this.strategyResult && this.strategyResult.metrics) || {} },
    strategyRequest() { return (this.strategyResult && this.strategyResult.request) || {} },
    strategyTrades() { return this.strategyResult && Array.isArray(this.strategyResult.trades) ? this.strategyResult.trades : [] },
    strategyMarketRegimes() { return this.strategyResult && Array.isArray(this.strategyResult.market_regimes) ? this.strategyResult.market_regimes : [] },
    strategyMarketRegimeDays() { return this.strategyMarketRegimes.reduce((total, item) => total + Number(item.days || 0), 0) },
    strategyCoverage() {
      const coverage = (this.strategyResult && this.strategyResult.data_coverage) || {}
      return Object.entries(coverage).map(([symbol, item]) => ({ symbol, ...item }))
    },
    realtimeSignals() { return this.realtimeResult && Array.isArray(this.realtimeResult.signals) ? this.realtimeResult.signals : [] },
    realtimeScanButtonText() {
      if (this.realtimeLoading) return "正在扫描行情与因子…"
      if (this.realtimeSnapshotLoading) return "正在确认交易状态…"
      if (this.realtimeFrozen && this.realtimeMarketState === "break") return "午间休市"
      if (this.realtimeFrozen && this.realtimeMarketState === "closed") return "已收盘 · 已冻结"
      if (this.realtimeFrozen) return "实时扫描已暂停"
      return "运行实时扫描"
    },
    realtimeSessionSummary() {
      const state = this.realtimeStateLabel(this.realtimeMarketState || (this.realtimeResult && this.realtimeResult.market_state))
      const frozen = this.realtimeFrozen ? " · 已冻结" : ""
      const next = this.realtimeNextScanAt ? ` · ${this.formatDateTime(this.realtimeNextScanAt)} 恢复` : ""
      return `${state}${frozen}${next}`
    },
    realtimeOutcomeSummaries() { return this.realtimeOutcomeReport && Array.isArray(this.realtimeOutcomeReport.summaries) ? this.realtimeOutcomeReport.summaries : [] },
    realtimeOutcomeSummary() {
      return this.realtimeOutcomeSummaries.find(item => Number(item.horizon) === Number(this.realtimeOutcomeHorizon)) || this.realtimeOutcomeSummaries[0] || {}
    },
    realtimeOutcomeBreakdowns() {
      if (!this.realtimeOutcomeReport) return []
      const key = this.realtimeOutcomeView === "strategies" ? "strategies" : this.realtimeOutcomeView === "regimes" ? "market_regimes" : "score_buckets"
      return Array.isArray(this.realtimeOutcomeReport[key]) ? this.realtimeOutcomeReport[key] : []
    },
    realtimeOutcomeRecent() {
      const items = this.realtimeOutcomeReport && Array.isArray(this.realtimeOutcomeReport.recent) ? this.realtimeOutcomeReport.recent : []
      return items.filter(item => Number(item.horizon) === Number(this.realtimeOutcomeHorizon)).slice(0, 24)
    },
    realtimeOutcomeAssessment() { return this.realtimeOutcomeReport && this.realtimeOutcomeReport.assessment ? this.realtimeOutcomeReport.assessment : {} },
    realtimeComponentAnalysis() {
      const analysis = this.realtimeOutcomeReport && this.realtimeOutcomeReport.component_analysis
      return analysis && typeof analysis === "object" ? analysis : { components: [], coverage: [], correlations: [], regime_metrics: [] }
    },
    realtimeComponentCoverage() {
      return Array.isArray(this.realtimeComponentAnalysis.coverage) ? this.realtimeComponentAnalysis.coverage : []
    },
    realtimeCorrelationRows() {
      const components = Array.isArray(this.realtimeComponentAnalysis.components) ? this.realtimeComponentAnalysis.components : []
      const correlations = Array.isArray(this.realtimeComponentAnalysis.correlations) ? this.realtimeComponentAnalysis.correlations : []
      return components.map(left => ({
        ...left,
        cells: components.map(right => correlations.find(cell => cell.left_key === left.key && cell.right_key === right.key) || {
          left_key: left.key, right_key: right.key, samples: 0, correlation: 0, rank_correlation: 0, relation: "insufficient", sample_sufficient: false,
        }),
      }))
    },
    realtimeRegimeRows() {
      const components = Array.isArray(this.realtimeComponentAnalysis.components) ? this.realtimeComponentAnalysis.components : []
      const metrics = Array.isArray(this.realtimeComponentAnalysis.regime_metrics) ? this.realtimeComponentAnalysis.regime_metrics : []
      const horizon = Number(this.realtimeOutcomeHorizon)
      const regimes = ["牛市", "熊市", "震荡", "高波动"]
      return components.map(component => ({
        ...component,
        cells: regimes.map(regime => metrics.find(item => item.component_key === component.key && item.regime === regime && Number(item.horizon) === horizon) || {
          component_key: component.key, component_name: component.name, regime, horizon, samples: 0, active_samples: 0, state: "insufficient", sample_sufficient: false,
        }),
      }))
    },
    realtimeRegimeSummary() {
      const summary = { sufficient: 0, positive: 0, negative: 0, mixed: 0 }
      this.realtimeRegimeRows.forEach(row => row.cells.forEach(cell => {
        if (!cell || !cell.sample_sufficient) return
        summary.sufficient += 1
        if (cell.state === "positive") summary.positive += 1
        else if (cell.state === "negative") summary.negative += 1
        else summary.mixed += 1
      }))
      return summary
    },
    realtimeWalkForward() {
      const analysis = this.realtimeOutcomeReport && this.realtimeOutcomeReport.component_walk_forward
      return analysis && typeof analysis === "object" ? analysis : { metrics: [] }
    },
    realtimeWalkForwardMetrics() {
      const metrics = Array.isArray(this.realtimeWalkForward.metrics) ? this.realtimeWalkForward.metrics : []
      return metrics.filter(item => Number(item.horizon) === Number(this.realtimeOutcomeHorizon))
    },
    realtimePortfolioAnalysis() {
      const analysis = this.realtimeOutcomeReport && this.realtimeOutcomeReport.portfolio_analysis
      return analysis && typeof analysis === "object" ? analysis : { horizons: [] }
    },
    realtimePortfolioMetric() {
      const horizons = Array.isArray(this.realtimePortfolioAnalysis.horizons) ? this.realtimePortfolioAnalysis.horizons : []
      return horizons.find(item => Number(item.horizon) === Number(this.realtimeOutcomeHorizon)) || horizons[0] || {}
    },
    shadowTrades() { return this.shadowReport && Array.isArray(this.shadowReport.trades) ? this.shadowReport.trades : [] },
    shadowPositions() { return this.shadowReport && Array.isArray(this.shadowReport.positions) ? this.shadowReport.positions : [] },
    shadowOrders() { return this.shadowReport && Array.isArray(this.shadowReport.orders) ? this.shadowReport.orders : [] },
    shadowRejections() { return this.shadowReport && Array.isArray(this.shadowReport.rejections) ? this.shadowReport.rejections : [] },
    strategyResultTitle() {
      if (!this.strategyResult) return "回测结果"
      const tickers = this.strategyRequest.tickers || []
      if (tickers.length === 1) return `${this.strategyName(tickers[0])} · ${this.strategyModeLabel(this.strategyRequest.technical && this.strategyRequest.technical.entry_mode)}`
      return `${tickers.length} 只股票组合 · ${this.strategyModeLabel(this.strategyRequest.technical && this.strategyRequest.technical.entry_mode)}`
    },
    strategyResultPeriod() {
      if (!this.strategyResult) return ""
      return `${String(this.strategyRequest.start || "").slice(0, 10)} 至 ${String(this.strategyRequest.end || "").slice(0, 10)} · ${this.strategyResult.equity?.length || 0} 个交易日 · ${this.strategyRequest.benchmark === "sh000300" ? "沪深300" : this.strategyRequest.benchmark || "无基准"}`
    },
    strategyParameterSummary() {
      const technical = this.strategyRequest.technical || {}
      return `MA${technical.fast_ma || "--"} / MA${technical.slow_ma || "--"} · ${technical.breakout_days || "--"}日 · 量比≥${this.number(technical.volume_ratio_min, 1)}`
    },
    costSummary() {
      const request = this.strategyRequest
      return `${this.number(Number(request.commission_rate) * 10000, 1)}bp / ${this.number(Number(request.stamp_duty_rate) * 10000, 1)}bp / ${this.number(request.slippage_bps, 1)}bp`
    },
    lastBar() { return this.bars[this.bars.length - 1] || {} },
    lastMinute() { return this.minutes[this.minutes.length - 1] || {} },
    dailyViewEnd() {
      if (!this.bars.length) return 0
      if (this.dailyEndIndex == null) return this.bars.length
      return Math.max(1, Math.min(this.bars.length, Math.round(this.dailyEndIndex)))
    },
    visibleLastBar() {
      return this.bars[this.dailyViewEnd - 1] || {}
    },
    dailyLevels() {
      if (this.dailyViewEnd < 1) return {}
      const currentIndex = this.dailyViewEnd - 1
      const sample = this.bars.slice(Math.max(0, currentIndex - 20), currentIndex)
      const highs = sample.map(bar => Number(bar.high)).filter(Number.isFinite)
      const lows = sample.map(bar => Number(bar.low)).filter(Number.isFinite)
      return {
        resistance: highs.length ? Math.max(...highs) : null,
        support: lows.length ? Math.min(...lows) : null,
        close: Number(this.visibleLastBar.close),
      }
    },
    quoteAmount() {
      if (this.quote.amount == null) return "--"
      const amountInTenThousandYuan = Number(this.quote.amount)
      return this.compact(Number.isFinite(amountInTenThousandYuan) ? amountInTenThousandYuan * 10000 : NaN)
    },
    title() { return `${this.board?.name || this.quote.name || this.data.symbol || "行情图"} · ${this.isSector ? "板块" : "行情图"}` },
    timestamp() {
      if (this.loading) return "正在采集行情..."
      if (this.quote.quote_time) return `行情 ${this.quote.quote_time} · 更新 ${new Date().toLocaleTimeString()}`
      if (this.isSector && this.data.fetched_at) return `板块资金 · 更新 ${new Date(this.data.fetched_at).toLocaleTimeString()}`
      return this.data.fetched_at ? `更新 ${new Date(this.data.fetched_at).toLocaleTimeString()}` : "等待行情"
    },
    changeClass() {
      if (this.quote.percent == null) return "flat"
      const value = Number(this.quote.percent)
      return value > 0 ? "up" : value < 0 ? "down" : "flat"
    },
    changeText() {
      if (this.quote.percent == null) return "--"
      const percent = Number(this.quote.percent)
      const delta = Number(this.quote.delta)
      if (!Number.isFinite(percent)) return "--"
      const sign = percent >= 0 ? "+" : ""
      return `${sign}${this.number(percent)}%  ${delta >= 0 ? "+" : ""}${this.number(delta)}`
    },
    boardChangeClass() {
      const value = Number(this.board && this.board.percent)
      return Number.isFinite(value) ? (value > 0 ? "up" : value < 0 ? "down" : "flat") : "flat"
    },
    boardChangeText() {
      return this.percentText(this.board && this.board.percent)
    },
    messages() {
      const quoteDate = String(this.quote.quote_time || "").slice(0, 10)
      const dailyWarning = !this.isSector && this.chartMode === "daily" && quoteDate && this.lastBar.date === quoteDate ? "最新日 K 包含盘中未完成数据，收盘后再确认当日形态" : ""
      const sourceError = this.isSector ? "" : this.chartMode === "intraday" ? this.data.minute_error : this.data.history_error
      const empty = !this.isSector && this.chartMode === "intraday" && !this.minutes.length && !this.loading ? "暂无有效分时数据" : ""
      return [this.error, this.data.quote_error, this.data.board_error, sourceError, dailyWarning, empty].filter(Boolean)
    },
  },
  methods: {
    number(value, digits = 2) {
      return Number.isFinite(Number(value)) ? Number(value).toFixed(digits) : "--"
    },
    percentText(value) {
      const number = Number(value)
      if (!Number.isFinite(number)) return "--"
      return `${number > 0 ? "+" : ""}${number.toFixed(2)}%`
    },
    assetKindLabel(kind) {
      if (kind === "sector") return "板块"
      if (kind === "convertible_bond") return "转债"
      return "股票"
    },
    compact(value) {
      if (value == null) return "--"
      const number = Number(value)
      if (!Number.isFinite(number)) return "--"
      const absolute = Math.abs(number)
      if (absolute >= 1e8) return `${(number / 1e8).toFixed(2)}亿`
      if (absolute >= 1e4) return `${(number / 1e4).toFixed(2)}万`
      return number.toFixed(0)
    },
    currency(value) {
      const number = Number(value)
      if (!Number.isFinite(number)) return "--"
      return new Intl.NumberFormat("zh-CN", { style: "currency", currency: "CNY", maximumFractionDigits: 0 }).format(number)
    },
    metricClass(value) {
      const number = Number(value)
      return !Number.isFinite(number) || number === 0 ? "flat" : number > 0 ? "up" : "down"
    },
    formatDateTime(value) {
      if (!value) return "--"
      const date = new Date(value)
      if (Number.isNaN(date.getTime())) return String(value)
      return date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false })
    },
    strategyModeLabel(mode) {
      if (mode === "trend-reclaim") return "趋势收复"
      if (mode === "ma-pullback") return "均线回踩"
      return "放量突破"
    },
    strategyName(symbol) {
      const names = (this.strategyRequest && this.strategyRequest.names) || {}
      if (names[symbol]) return names[symbol]
      const item = this.watchlistItems.find(candidate => candidate.symbol === symbol)
      return (item && item.name) || this.displayCode(symbol)
    },
    strategyHistoryTitle(item) {
      const tickers = Array.isArray(item && item.tickers) ? item.tickers : []
      const names = (item && item.names) || {}
      if (tickers.length === 1) return names[tickers[0]] || this.displayCode(tickers[0])
      const first = tickers[0] ? (names[tickers[0]] || this.displayCode(tickers[0])) : "组合"
      return `${first} 等 ${tickers.length} 只`
    },
    realtimeCount(state) { return this.realtimeSignals.filter(item => item.state === state).length },
    shadowValuationLabel(position) {
      if (!position) return "--"
      const quoteTime = position.valuation_time || position.last_date || "--"
      const source = position.realtime_valuation ? (position.valuation_source || "实时行情") : "日 K"
      return `${quoteTime} · ${source}`
    },
    shadowRejectionDetail(item) {
      if (!item) return ""
      const position = this.shadowPositions.find(candidate => candidate.symbol === item.symbol)
      if (String(item.reason || "").includes("同股票影子持仓") && position) {
        const exitPlan = position.target_exit_date ? `计划 ${position.target_exit_date} 收盘退出` : `按 ${this.shadowReport?.config?.holding_days || 5} 个交易日持有窗口退出`
        return `已有持仓：${position.entry_time || `${position.entry_date} 09:30`}，${position.quantity || 0} 股，${exitPlan}；单股单仓约束下保留原仓，本次信号不重复加仓。`
      }
      return item.reason || ""
    },
    switchRealtimeSection(section) {
      if (!['candidates', 'shadow', 'validation'].includes(section)) return
      this.realtimeSection = section
      if (section === 'shadow') this.loadShadowReport()
      if (section === 'validation' && !this.realtimeOutcomeReport) this.loadRealtimeOutcomes()
      window.scrollTo({ top: 0, behavior: 'auto' })
    },
    switchWorkspace(mode) {
      if (mode !== "market" && mode !== "strategy" && mode !== "realtime") return
      this.workspaceMode = mode
      this.strategyCrosshair = null
      if (mode === "strategy") {
        if (!this.strategyHistoryLoaded) this.loadStrategyHistory()
        this.$nextTick(() => this.drawStrategyChart())
      } else if (mode === "realtime") {
        this.loadRealtimeSnapshot().then(() => {
          if (this.workspaceMode === "realtime" && this.realtimeScanAllowed && Date.now() - this.realtimeAutoAt >= 30000) this.runRealtimeScan()
        })
        this.loadRealtimeHistory()
        this.loadRealtimeOutcomes()
        this.loadShadowReport()
      } else {
        this.$nextTick(() => this.drawChart())
      }
    },
    async loadStrategyHistory() {
      if (this.strategyHistoryLoading) return
      this.strategyHistoryLoading = true
      try {
        const response = await fetch("/api/strategy/backtests?limit=20", { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "回测历史读取失败")
        this.strategyHistory = Array.isArray(payload.items) ? payload.items : []
        this.strategyHistoryLoaded = true
      } catch (error) {
        this.strategyError = error instanceof Error ? error.message : String(error)
      } finally {
        this.strategyHistoryLoading = false
      }
    },
    async loadStrategyRun(runID) {
      if (!runID || this.strategyLoading) return
      this.strategyLoading = true
      this.strategyError = ""
      try {
        const response = await fetch(`/api/strategy/backtests?id=${encodeURIComponent(runID)}`, { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "回测归档读取失败")
        this.strategyResult = payload.result || null
        this.strategyAssessment = payload.assessment || null
        this.strategyCrosshair = null
      } catch (error) {
        this.strategyError = error instanceof Error ? error.message : String(error)
      } finally {
        this.strategyLoading = false
        await this.$nextTick()
        this.drawStrategyChart()
      }
    },
    async runStrategyBacktest() {
      if (this.strategyLoading || !this.strategyPool.length) return
      this.strategyLoading = true
      this.strategyError = ""
      this.strategyCrosshair = null
      const payload = { ...this.strategyForm, symbols: this.strategyPool.map(item => item.symbol), benchmark: "sh000300", liquidate_at_end: true }
      try {
        const response = await fetch("/api/strategy/backtests", {
          method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload),
        })
        const body = await response.text()
        const result = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(result.error || "量化回测失败")
        this.strategyResult = result.result || null
        this.strategyAssessment = result.assessment || null
        await this.loadStrategyHistory()
      } catch (error) {
        this.strategyError = error instanceof Error ? error.message : String(error)
      } finally {
        this.strategyLoading = false
        await this.$nextTick()
        this.drawStrategyChart()
      }
    },
    realtimeStateLabel(state) {
      if (state === "triggered") return "触发"
      if (state === "watching") return "观察"
      if (state === "invalid") return "数据不足"
      if (state === "weak") return "偏弱"
      if (state === "trading") return "连续竞价"
      if (state === "auction") return "集合竞价"
      if (state === "break") return "午间休市"
      if (state === "closed") return "已收盘"
      return state || "--"
    },
    realtimeStateClass(state) {
      if (state === "triggered") return "realtime-triggered"
      if (state === "watching") return "realtime-watching"
      return "realtime-weak"
    },
    outcomeStatusLabel(status) {
      if (status === "ready") return "已成熟"
      if (status === "pending") return "待成熟"
      return "无效"
    },
    outcomeStatusClass(status) {
      if (status === "ready") return "up"
      if (status === "pending") return "warn-text"
      return "down"
    },
    outcomeHorizonLabel(horizon) { return `${horizon}日` },
    outcomeBreakdownSummary(item) {
      const summaries = Array.isArray(item && item.summaries) ? item.summaries : []
      return summaries.find(summary => Number(summary.horizon) === Number(this.realtimeOutcomeHorizon)) || summaries[0] || {}
    },
    outcomeCheckClass(check) {
      if (!check) return ""
      if (check.passed) return "pass"
      return check.required ? "fail required" : "pending"
    },
    outcomeWeightText(value) { return Number.isFinite(Number(value)) ? `${(Number(value) * 100).toFixed(1)}%` : "--" },
    componentCoverageClass(item) {
      if (!item || !Number.isFinite(Number(item.available_percent))) return "flat"
      return Number(item.available_percent) >= 70 ? "up" : "warn-text"
    },
    componentCorrelationClass(cell) {
      if (!cell || !cell.sample_sufficient) return "component-cell-insufficient"
      if (cell.relation === "overlap") return "component-cell-overlap"
      if (cell.relation === "inverse") return "component-cell-inverse"
      return "component-cell-distinct"
    },
    componentRelationLabel(relation) {
      if (relation === "overlap") return "重叠"
      if (relation === "inverse") return "反向"
      if (relation === "self") return "自身"
      if (relation === "distinct") return "区分"
      return "样本不足"
    },
    componentCorrelationText(cell) {
      if (!cell || !cell.sample_sufficient) return "--"
      return this.number(cell.rank_correlation, 2)
    },
    componentCorrelationTitle(cell) {
      if (!cell) return "暂无成对样本"
      const sample = `${cell.samples || 0} 个样本`
      if (!cell.sample_sufficient) return `${sample} · 至少需要 ${this.realtimeComponentAnalysis.minimum_correlation_samples || 20} 个`
      return `${this.componentRelationLabel(cell.relation)} · Rank ${this.number(cell.rank_correlation, 3)} · Pearson ${this.number(cell.correlation, 3)} · ${sample}`
    },
    componentRegimeClass(cell) {
      if (!cell || !cell.sample_sufficient) return "component-cell-insufficient"
      if (cell.state === "positive") return "component-cell-positive"
      if (cell.state === "negative") return "component-cell-negative"
      return "component-cell-mixed"
    },
    componentRegimeText(cell) {
      if (!cell || !cell.sample_sufficient) return "--"
      return this.componentRegimeStateLabel(cell.state)
    },
    componentRegimeStateLabel(state) {
      if (state === "positive") return "正向"
      if (state === "negative") return "负向"
      if (state === "mixed") return "混合"
      return "样本不足"
    },
    componentRegimeTitle(cell) {
      if (!cell || !cell.sample_sufficient) return `可用 ${cell && cell.samples || 0} · 激活 ${cell && cell.active_samples || 0}`
      return `${this.componentRegimeStateLabel(cell.state)} · Rank IC ${this.number(cell.rank_information_coefficient, 3)} · 超额 ${this.percentText(cell.average_excess_percent)} · ${cell.samples} 个可用 / ${cell.active_samples} 个激活`
    },
    componentValidationStateLabel(state) {
      if (state === "positive") return "正向"
      if (state === "negative") return "负向"
      if (state === "unstable") return "权重漂移"
      if (state === "mixed") return "混合"
      return "样本不足"
    },
    componentValidationClass(item) {
      if (!item || !item.sample_sufficient) return "component-cell-insufficient"
      if (item.state === "positive") return "component-cell-positive"
      if (item.state === "negative") return "component-cell-negative"
      if (item.state === "unstable") return "component-cell-overlap"
      return "component-cell-mixed"
    },
    componentValidationTitle(item) {
      if (!item || !item.sample_sufficient) return `可用 ${item && item.available_samples || 0} · 充分折 ${item && item.sufficient_folds || 0}`
      return `${this.componentValidationStateLabel(item.state)} · 验证超额 ${this.percentText(item.validation_average_excess_percent)} · Rank IC ${this.number(item.validation_rank_information_coefficient, 3)} · 权重漂移 ${(Number(item.weight_drift || 0) * 100).toFixed(1)}%`
    },
    portfolioConstraintClass(value, threshold, inverse = false) {
      const number = Number(value)
      const limit = Number(threshold)
      if (!Number.isFinite(number) || !Number.isFinite(limit)) return "flat"
      const passed = inverse ? number >= limit : number <= limit
      return passed ? "up" : "down"
    },
    signedPercent(value) {
      if (value == null || !Number.isFinite(Number(value))) return "--"
      const number = Number(value)
      return `${number > 0 ? "+" : ""}${number.toFixed(2)}%`
    },
    async loadRealtimeSnapshot() {
      if (this.realtimeSnapshotLoading) return
      this.realtimeSnapshotLoading = true
      this.realtimeSessionCheckedAt = Date.now()
      this.realtimeError = ""
      try {
        const response = await fetch("/api/strategy/realtime", { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "实时扫描结果读取失败")
        this.applyRealtimeSession(payload)
        if (payload.result) this.realtimeResult = payload.result
      } catch (error) {
        this.realtimeError = error instanceof Error ? error.message : String(error)
      } finally {
        this.realtimeSnapshotLoading = false
      }
    },
    applyRealtimeSession(payload) {
      if (!payload || typeof payload !== "object") return
      this.realtimeMarketState = payload.market_state || (payload.result && payload.result.market_state) || this.realtimeMarketState
      if (typeof payload.scan_allowed === "boolean") this.realtimeScanAllowed = payload.scan_allowed
      if (typeof payload.frozen === "boolean") this.realtimeFrozen = payload.frozen
      this.realtimeNextScanAt = payload.next_scan_at || ""
    },
    async loadRealtimeHistory() {
      try {
        const response = await fetch("/api/strategy/realtime?view=history&limit=30", { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "实时信号历史读取失败")
        this.realtimeHistory = Array.isArray(payload.history) ? payload.history : []
      } catch (error) {
        this.realtimeError = error instanceof Error ? error.message : String(error)
      }
    },
    async loadRealtimeOutcomes() {
      if (this.realtimeOutcomeLoading) return
      this.realtimeOutcomeError = ""
      try {
        const response = await fetch("/api/strategy/realtime?view=outcomes&limit=2000", { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "前测结果读取失败")
        this.realtimeOutcomeReport = payload.report || null
        if (this.realtimeOutcomeReport && Array.isArray(this.realtimeOutcomeReport.horizons) && this.realtimeOutcomeReport.horizons.length && !this.realtimeOutcomeReport.horizons.includes(Number(this.realtimeOutcomeHorizon))) {
          this.realtimeOutcomeHorizon = Number(this.realtimeOutcomeReport.horizons[0])
        }
      } catch (error) {
        this.realtimeOutcomeError = error instanceof Error ? error.message : String(error)
      }
    },
    async refreshRealtimeOutcomes() {
      if (this.realtimeOutcomeLoading) return
      const scrollY = window.scrollY
      this.realtimeOutcomeLoading = true
      this.realtimeOutcomeError = ""
      try {
        const response = await fetch("/api/strategy/realtime?view=outcomes&limit=2000&horizons=1,3,5,10", { method: "POST", cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "前测结果更新失败")
        this.realtimeOutcomeReport = payload.report || null
      } catch (error) {
        this.realtimeOutcomeError = error instanceof Error ? error.message : String(error)
      } finally {
        this.realtimeOutcomeLoading = false
        await this.$nextTick()
        window.scrollTo({ top: scrollY, behavior: 'auto' })
      }
    },
    async loadShadowReport() {
      if (this.shadowLoading) return
      this.shadowCheckedAt = Date.now()
      this.shadowError = ""
      try {
        const response = await fetch("/api/strategy/shadow", { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "影子执行结果读取失败")
        this.shadowReport = payload.report && payload.report.generated_at ? payload.report : null
      } catch (error) {
        this.shadowError = error instanceof Error ? error.message : String(error)
      }
    },
    async refreshShadowReport() {
      if (this.shadowLoading) return
      const scrollY = window.scrollY
      this.shadowCheckedAt = Date.now()
      this.shadowLoading = true
      this.shadowError = ""
      try {
        const response = await fetch("/api/strategy/shadow?minimum_score=55&holding_days=5", { method: "POST", cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "影子执行更新失败")
        this.shadowReport = payload.report || null
      } catch (error) {
        this.shadowError = error instanceof Error ? error.message : String(error)
      } finally {
        this.shadowLoading = false
        await this.$nextTick()
        window.scrollTo({ top: scrollY, behavior: 'auto' })
      }
    },
    async runRealtimeScan() {
      if (this.realtimeLoading || !this.realtimeScanAllowed) return
      this.realtimeLoading = true
      this.realtimeAutoAt = Date.now()
      this.realtimeError = ""
      try {
        const scope = this.realtimeScope === "watchlist" ? "watchlist" : "leaders"
        const response = await fetch(`/api/strategy/realtime?scope=${scope}`, { method: "POST", cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "实时策略扫描失败")
        this.applyRealtimeSession(payload)
        if (payload.result) this.realtimeResult = payload.result
        await this.loadRealtimeHistory()
      } catch (error) {
        this.realtimeError = error instanceof Error ? error.message : String(error)
      } finally {
        this.realtimeLoading = false
      }
    },
    pollRealtimeSession() {
      if (this.workspaceMode !== "realtime" || this.realtimeLoading) return
      const now = Date.now()
      if (this.realtimeSection === 'shadow' && !this.shadowLoading && now - this.shadowCheckedAt >= 30000) this.loadShadowReport()
      if (this.realtimeScanAllowed) {
        if (now - this.realtimeAutoAt >= 30000) this.runRealtimeScan()
        return
      }
      const nextScanAt = Date.parse(this.realtimeNextScanAt)
      if (!this.realtimeSnapshotLoading && Number.isFinite(nextScanAt) && now >= nextScanAt && now - this.realtimeSessionCheckedAt >= 10000) {
        this.loadRealtimeSnapshot().then(() => {
          if (this.workspaceMode === "realtime" && this.realtimeScanAllowed && Date.now() - this.realtimeAutoAt >= 30000) this.runRealtimeScan()
        })
      }
    },
    selectRealtimeSignal(signal) {
      if (!signal || !signal.symbol) return
      this.workspaceMode = "market"
      this.query = this.displayCode(signal.symbol)
      this.requestedSymbol = signal.symbol
      this.load(signal.symbol)
    },
    movingAverage(bars, index, length) {
      if (index + 1 < length) return null
      let sum = 0
      for (let cursor = index - length + 1; cursor <= index; cursor += 1) sum += Number(bars[cursor].close)
      return sum / length
    },
    search() {
      if (!this.query) return
      this.requestedSymbol = this.query
      this.load(this.requestedSymbol)
    },
    displayCode(symbol) {
      return String(symbol || "").replace(/^(sh|sz|th)/i, "")
    },
    watchlistName(item) {
      return item && item.name ? item.name : "名称待更新"
    },
    watchlistPercent(value) {
      if (value == null || !Number.isFinite(Number(value))) return "--"
      const number = Number(value)
      return `${number > 0 ? "+" : ""}${number.toFixed(2)}%`
    },
    watchlistChangeClass(value) {
      if (value == null || !Number.isFinite(Number(value))) return "flat"
      const number = Number(value)
      return number > 0 ? "up" : number < 0 ? "down" : "flat"
    },
    marketIndexChange(item) {
      if (!item || item.percent == null || !Number.isFinite(Number(item.percent))) return "--"
      const percent = Number(item.percent)
      const delta = Number(item.delta)
      const percentText = `${percent > 0 ? "+" : ""}${percent.toFixed(2)}%`
      if (!Number.isFinite(delta)) return percentText
      return `${percentText}  ${delta > 0 ? "+" : ""}${delta.toFixed(2)}`
    },
    marketAmountValue(value) {
      const amount = Number(value)
      if (!Number.isFinite(amount) || amount <= 0) return "--"
      if (amount >= 1e8) return `${(amount / 1e8).toFixed(2)}万亿`
      if (amount >= 1e4) return `${(amount / 1e4).toFixed(2)}亿`
      return `${amount.toFixed(0)}万`
    },
    marketAmountChange(amount) {
      if (!amount || !Number.isFinite(Number(amount.delta_wan_yuan)) || !Number.isFinite(Number(amount.percent))) return "--"
      const delta = Number(amount.delta_wan_yuan)
      const prefix = delta > 0 ? "+" : ""
      return `${prefix}${this.compact(delta * 10000)}  (${delta > 0 ? "+" : ""}${Number(amount.percent).toFixed(2)}%)`
    },
    isMarketIndex(symbol) {
      return marketIndexDefinitions.some(item => item.symbol === symbol)
    },
    async loadIndices() {
      const requestID = ++this.indicesRequestID
      this.indicesLoading = true
      try {
        const response = await fetch("/api/indices", { cache: "no-store" })
        const body = await response.text()
        if (!body) throw new Error("指数行情响应不完整")
        const payload = JSON.parse(body)
        if (requestID !== this.indicesRequestID) return
        const received = new Map((Array.isArray(payload.items) ? payload.items : []).map(item => [item.symbol, item]))
        this.marketIndices = marketIndexDefinitions.map(definition => ({ ...definition, ...(received.get(definition.symbol) || {}) }))
        this.marketAmount = payload.market_amount || this.marketAmount
        const warnings = [payload.warning, payload.amount_warning].filter(Boolean)
        this.indicesError = warnings.join("；")
        if (!response.ok) throw new Error(payload.warning || payload.error || "指数行情暂不可用")
      } catch (error) {
        if (requestID === this.indicesRequestID) this.indicesError = error instanceof Error ? error.message : String(error)
      } finally {
        if (requestID === this.indicesRequestID) this.indicesLoading = false
      }
    },
    selectMarketIndex(item) {
      if (!item || !item.symbol) return
      this.query = item.symbol
      this.requestedSymbol = item.symbol
      this.load(item.symbol)
    },
    async loadWatchlist() {
      const requestID = ++this.watchlistRequestID
      this.watchlistRefreshing = true
      this.watchlistError = ""
      try {
        const response = await fetch("/api/watchlist", { cache: "no-store" })
        const body = await response.text()
        if (!body) throw new Error("自选响应不完整")
        const payload = JSON.parse(body)
        if (!response.ok) throw new Error(payload.error || "自选读取失败")
        if (requestID !== this.watchlistRequestID) return
        const groups = Array.isArray(payload.groups) ? payload.groups : []
        this.watchlist = {
          groups: groups.map(group => {
            const symbols = Array.isArray(group.symbols) ? group.symbols : []
            const items = Array.isArray(group.items) && group.items.length
              ? group.items.filter(item => item && item.symbol)
              : symbols.map(symbol => ({ symbol, name: "" }))
            return { name: group.name, symbols, items }
          }),
        }
        if (!this.watchlistGroupNames.includes(this.selectedWatchlistGroup)) this.selectedWatchlistGroup = "全部"
        if (Array.isArray(payload.warnings) && payload.warnings.length) this.watchlistError = payload.warnings.join("；")
      } catch (error) {
        if (requestID === this.watchlistRequestID) this.watchlistError = error instanceof Error ? error.message : String(error)
      } finally {
        if (requestID === this.watchlistRequestID) this.watchlistRefreshing = false
      }
    },
    selectWatchlistSymbol(symbol) {
      this.query = this.displayCode(symbol)
      this.requestedSymbol = symbol
      this.load(symbol)
    },
    async addWatchlist() {
      if (!this.watchlistInput || this.watchlistLoading) return
      this.watchlistLoading = true
      this.watchlistError = ""
      const group = this.selectedWatchlistGroup === "全部" ? "默认" : this.selectedWatchlistGroup
      try {
        const response = await fetch("/api/watchlist", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ symbol: this.watchlistInput, group }),
        })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "添加自选失败")
        this.watchlistInput = ""
        await this.loadWatchlist()
      } catch (error) {
        this.watchlistError = error instanceof Error ? error.message : String(error)
      } finally {
        this.watchlistLoading = false
      }
    },
    async removeWatchlist(symbol) {
      if (this.watchlistLoading || !window.confirm(`从自选中移除 ${this.displayCode(symbol)}？`)) return
      this.watchlistLoading = true
      this.watchlistError = ""
      const params = new URLSearchParams({ symbol })
      if (this.selectedWatchlistGroup !== "全部") params.set("group", this.selectedWatchlistGroup)
      try {
        const response = await fetch(`/api/watchlist?${params.toString()}`, { method: "DELETE" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "移除自选失败")
        await this.loadWatchlist()
      } catch (error) {
        this.watchlistError = error instanceof Error ? error.message : String(error)
      } finally {
        this.watchlistLoading = false
      }
    },
    setChartMode(mode) {
      if (mode !== "intraday" && mode !== "daily") return
      this.crosshair = null
      this.chartMode = mode
      this.$nextTick(() => this.drawChart())
    },
    dailyWindow() {
      const total = this.bars.length
      if (!total) return { bars: [], startIndex: 0, endIndex: 0, count: 0 }
      const count = Math.max(1, Math.min(total, Math.round(this.dailyVisibleCount)))
      const endIndex = Math.max(count, Math.min(total, this.dailyEndIndex == null ? total : Math.round(this.dailyEndIndex)))
      const startIndex = Math.max(0, endIndex - count)
      return { bars: this.bars.slice(startIndex, endIndex), startIndex, endIndex, count: endIndex - startIndex }
    },
    updateDailyViewport(count, endIndex, preset = "custom") {
      const total = this.bars.length
      if (!total) return
      const minimum = Math.min(20, total)
      const nextCount = Math.max(minimum, Math.min(total, Math.round(count)))
      const nextEnd = Math.max(nextCount, Math.min(total, Math.round(endIndex)))
      this.dailyVisibleCount = nextCount
      this.dailyEndIndex = nextEnd >= total ? null : nextEnd
      this.dailyRangePreset = preset
      this.crosshair = null
      this.scheduleChartDraw()
    },
    setDailyRange(option) {
      if (!option || !Number.isFinite(Number(option.count))) return
      this.dailyVisibleCount = Number(option.count)
      this.dailyEndIndex = null
      this.dailyRangePreset = option.key
      this.crosshair = null
      this.$nextTick(() => this.drawChart())
    },
    resetDailyViewport() {
      this.setDailyRange(dailyRangeOptions.find(option => option.key === "6m"))
    },
    handleChartPointerDown(event) {
      const canvas = this.$refs.chart
      if (canvas && canvas.setPointerCapture) canvas.setPointerCapture(event.pointerId)
      this.chartPointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
      if (this.chartMode === "daily") {
        if (this.chartPointers.size === 1) {
          const viewport = this.dailyWindow()
          this.dailyPanState = { pointerId: event.pointerId, startX: event.clientX, startEnd: viewport.endIndex, count: viewport.count }
        } else if (this.chartPointers.size === 2) {
          this.beginDailyPinch()
        }
      }
      this.handleChartPointer(event)
    },
    beginDailyPinch() {
      if (!this.chartGeometry || this.chartMode !== "daily" || this.chartPointers.size < 2) return
      const points = [...this.chartPointers.values()]
      const distance = Math.max(1, Math.hypot(points[0].x - points[1].x, points[0].y - points[1].y))
      const canvas = this.$refs.chart
      const rect = canvas.getBoundingClientRect()
      const midpointX = (points[0].x + points[1].x) / 2 - rect.left
      const viewport = this.dailyWindow()
      const ratio = Math.max(0, Math.min(1, (midpointX - this.chartGeometry.left) / (this.chartGeometry.right - this.chartGeometry.left)))
      this.dailyPinchState = {
        distance,
        count: viewport.count,
        ratio,
        anchorIndex: viewport.startIndex + ratio * Math.max(0, viewport.count - 1),
      }
      this.dailyPanState = null
      this.crosshair = null
    },
    handleChartPointerMove(event) {
      if (this.chartPointers.has(event.pointerId)) this.chartPointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
      if (this.chartMode === "daily" && this.chartPointers.size >= 2) {
        this.dailyDragging = true
        if (!this.dailyPinchState) this.beginDailyPinch()
        const points = [...this.chartPointers.values()]
        const distance = Math.max(1, Math.hypot(points[0].x - points[1].x, points[0].y - points[1].y))
        const pinch = this.dailyPinchState
        const nextCount = pinch.count * pinch.distance / distance
        const nextStart = pinch.anchorIndex - pinch.ratio * (nextCount - 1)
        this.updateDailyViewport(nextCount, nextStart + nextCount)
        return
      }
      const dragging = this.chartMode === "daily" && this.dailyPanState && this.dailyPanState.pointerId === event.pointerId && (event.pointerType === "touch" || event.buttons > 0)
      if (dragging && Math.abs(event.clientX - this.dailyPanState.startX) > 2) {
        this.dailyDragging = true
        const geometry = this.chartGeometry
        const pixelsPerBar = geometry ? (geometry.right - geometry.left) / this.dailyPanState.count : 1
        const deltaBars = Math.round((event.clientX - this.dailyPanState.startX) / Math.max(1, pixelsPerBar))
        this.updateDailyViewport(this.dailyPanState.count, this.dailyPanState.startEnd - deltaBars)
        return
      }
      this.handleChartPointer(event)
    },
    handleChartPointerUp(event) {
      this.chartPointers.delete(event.pointerId)
      this.dailyDragging = this.chartPointers.size > 0 && this.dailyPinchState != null
      if (this.chartPointers.size < 2) this.dailyPinchState = null
      if (this.chartMode === "daily" && this.chartPointers.size === 1) {
        const [pointerId, point] = this.chartPointers.entries().next().value
        const viewport = this.dailyWindow()
        this.dailyPanState = { pointerId, startX: point.x, startEnd: viewport.endIndex, count: viewport.count }
      } else if (this.chartPointers.size === 0) {
        this.dailyPanState = null
      }
    },
    handleChartPointerLeave() {
      if (this.chartPointers.size === 0) this.clearChartCrosshair()
    },
    handleChartWheel(event) {
      if (this.chartMode !== "daily" || !this.chartGeometry || !this.bars.length) return
      event.preventDefault()
      const geometry = this.chartGeometry
      const rect = this.$refs.chart.getBoundingClientRect()
      const pointerX = event.clientX - rect.left
      const ratio = Math.max(0, Math.min(1, (pointerX - geometry.left) / (geometry.right - geometry.left)))
      const viewport = this.dailyWindow()
      const anchorIndex = viewport.startIndex + ratio * Math.max(0, viewport.count - 1)
      const zoomFactor = Math.exp(Number(event.deltaY) * 0.0015)
      const nextCount = viewport.count * zoomFactor
      const nextStart = anchorIndex - ratio * (nextCount - 1)
      this.updateDailyViewport(nextCount, nextStart + nextCount)
    },
    async load(symbol) {
      if (this.loading) return
      this.loading = true
      this.error = ""
      try {
        const response = await fetch(`/api/stock?symbol=${encodeURIComponent(symbol)}&limit=300`, { cache: "no-store" })
        const body = await response.text()
        if (!body) throw new Error("行情响应不完整，请稍后自动重试")
        const payload = JSON.parse(body)
        if (!response.ok) throw new Error(payload.error || payload.board_error || "行情请求失败")
        const symbolChanged = this.data.symbol && payload.symbol && this.data.symbol !== payload.symbol
        this.data = payload
        if (symbolChanged) {
          this.dailyVisibleCount = 120
          this.dailyEndIndex = null
          this.dailyRangePreset = "6m"
          this.crosshair = null
        }
        if (payload.symbol) this.query = this.isMarketIndex(payload.symbol) || String(payload.symbol).toLowerCase().startsWith("bk") ? payload.symbol : String(payload.symbol).slice(2)
        await this.$nextTick()
        this.drawChart()
      } catch (error) {
        this.error = error instanceof Error ? error.message : String(error)
      } finally {
        this.loading = false
      }
    },
    prepareStrategyCanvas() {
      const canvas = this.$refs.strategyChart
      if (!canvas) return null
      const rect = canvas.getBoundingClientRect()
      const dpr = window.devicePixelRatio || 1
      const pixelWidth = Math.max(1, Math.floor(rect.width * dpr))
      const pixelHeight = Math.max(1, Math.floor(rect.height * dpr))
      if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
        canvas.width = pixelWidth
        canvas.height = pixelHeight
      }
      const context = canvas.getContext("2d")
      context.setTransform(dpr, 0, 0, dpr, 0, 0)
      context.fillStyle = "#171d21"
      context.fillRect(0, 0, rect.width, rect.height)
      return { context, width: rect.width, height: rect.height }
    },
    drawStrategyChart() {
      const canvas = this.prepareStrategyCanvas()
      if (!canvas || !this.strategyResult) return
      const { context, width, height } = canvas
      const equity = Array.isArray(this.strategyResult.equity) ? this.strategyResult.equity.filter(item => Number.isFinite(Number(item.equity))) : []
      if (equity.length < 2) {
        context.fillStyle = "#91a0a7"
        context.font = '12px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
        context.fillText("净值数据不足", 18, 28)
        return
      }
      const benchmarkMap = new Map((Array.isArray(this.strategyResult.benchmark_equity) ? this.strategyResult.benchmark_equity : []).map(item => [item.date, Number(item.return_percent)]))
      const initialCash = Number(this.strategyRequest.initial_cash) || Number(equity[0].equity)
      const points = equity.map(item => ({
        ...item,
        strategyReturn: (Number(item.equity) / initialCash - 1) * 100,
        benchmarkReturn: benchmarkMap.has(item.date) ? benchmarkMap.get(item.date) : null,
        drawdown: Number(item.drawdown_percent),
      }))
      const left = width < 640 ? 44 : 58
      const right = 18
      const top = 22
      const equityBottom = Math.floor(height * .68)
      const drawdownTop = equityBottom + 38
      const drawdownBottom = height - 36
      const plotWidth = Math.max(20, width - left - right)
      const returns = points.flatMap(item => [item.strategyReturn, item.benchmarkReturn]).filter(Number.isFinite)
      returns.push(0)
      let minimum = Math.min(...returns)
      let maximum = Math.max(...returns)
      const padding = (maximum - minimum) * .1 || 1
      minimum -= padding
      maximum += padding
      const x = index => left + index / Math.max(1, points.length - 1) * plotWidth
      const y = value => top + (maximum - value) / (maximum - minimum) * (equityBottom - top)
      const maxDrawdown = Math.max(1, ...points.map(item => Math.abs(Math.min(0, item.drawdown || 0))))
      const drawdownY = value => drawdownTop + Math.abs(Math.min(0, value)) / maxDrawdown * (drawdownBottom - drawdownTop)

      context.font = '11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      context.textAlign = "right"
      context.fillStyle = "#91a0a7"
      context.strokeStyle = "#2b363c"
      for (let index = 0; index < 5; index += 1) {
        const lineY = top + index * (equityBottom - top) / 4
        const value = maximum - (maximum - minimum) * index / 4
        context.beginPath(); context.moveTo(left, lineY); context.lineTo(width - right, lineY); context.stroke()
        context.fillText(`${value >= 0 ? "+" : ""}${value.toFixed(1)}%`, left - 7, lineY + 4)
      }
      context.fillText("0%", left - 7, drawdownTop + 4)
      context.fillText(`-${maxDrawdown.toFixed(1)}%`, left - 7, drawdownBottom + 4)
      context.strokeStyle = "#2b363c"
      context.beginPath(); context.moveTo(left, drawdownTop); context.lineTo(width - right, drawdownTop); context.stroke()

      const drawLine = (field, color, dashed = false) => {
        context.beginPath()
        let started = false
        points.forEach((item, index) => {
          const value = Number(item[field])
          if (!Number.isFinite(value)) return
          if (started) context.lineTo(x(index), y(value))
          else { context.moveTo(x(index), y(value)); started = true }
        })
        context.strokeStyle = color
        context.lineWidth = field === "strategyReturn" ? 2 : 1.4
        context.setLineDash(dashed ? [6, 4] : [])
        context.stroke()
        context.setLineDash([])
      }
      drawLine("benchmarkReturn", "#f0b768", true)
      drawLine("strategyReturn", "#58b9d7")

      context.beginPath()
      context.moveTo(x(0), drawdownTop)
      points.forEach((item, index) => context.lineTo(x(index), drawdownY(item.drawdown)))
      context.lineTo(x(points.length - 1), drawdownTop)
      context.closePath()
      context.fillStyle = "rgba(239, 107, 107, .18)"
      context.fill()
      context.strokeStyle = "rgba(239, 107, 107, .7)"
      context.stroke()

      context.textAlign = "center"
      context.fillStyle = "#91a0a7"
      const labelCount = width < 640 ? 3 : 6
      for (let index = 0; index < labelCount; index += 1) {
        const pointIndex = Math.round(index * (points.length - 1) / Math.max(1, labelCount - 1))
        context.fillText(String(points[pointIndex].date || "").slice(0, 10), x(pointIndex), height - 13)
      }

      this.strategyChartGeometry = { points, left, right: width - right, top, bottom: drawdownBottom, x }
      if (this.strategyCrosshair != null) {
        const index = Math.max(0, Math.min(points.length - 1, this.strategyCrosshair))
        const point = points[index]
        const crossX = x(index)
        context.strokeStyle = "#71838c"
        context.setLineDash([3, 4])
        context.beginPath(); context.moveTo(crossX, top); context.lineTo(crossX, drawdownBottom); context.stroke()
        context.setLineDash([])
        const rows = [
          ["策略", this.percentText(point.strategyReturn), "#58b9d7"],
          ["沪深300", this.percentText(point.benchmarkReturn), "#f0b768"],
          ["回撤", this.percentText(point.drawdown), "#ef6b6b"],
          ["权益", this.currency(point.equity), "#edf3f5"],
        ]
        const boxWidth = 184
        const boxHeight = 104
        const boxX = crossX + boxWidth + 12 < width - right ? crossX + 10 : crossX - boxWidth - 10
        const boxY = top + 8
        context.fillStyle = "rgba(16, 22, 26, .96)"; context.fillRect(boxX, boxY, boxWidth, boxHeight)
        context.strokeStyle = "#50626b"; context.strokeRect(boxX + .5, boxY + .5, boxWidth - 1, boxHeight - 1)
        context.font = '600 11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
        context.textAlign = "left"; context.fillStyle = "#edf3f5"; context.fillText(point.date, boxX + 10, boxY + 17)
        context.font = '11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
        rows.forEach((row, rowIndex) => {
          const rowY = boxY + 38 + rowIndex * 16
          context.fillStyle = "#91a0a7"; context.textAlign = "left"; context.fillText(row[0], boxX + 10, rowY)
          context.fillStyle = row[2]; context.textAlign = "right"; context.fillText(row[1], boxX + boxWidth - 10, rowY)
        })
      }
    },
    handleStrategyChartPointer(event) {
      const geometry = this.strategyChartGeometry
      const canvas = this.$refs.strategyChart
      if (!geometry || !canvas || !geometry.points.length) return
      const rect = canvas.getBoundingClientRect()
      const pointerX = event.clientX - rect.left
      if (pointerX < geometry.left || pointerX > geometry.right) return this.clearStrategyCrosshair()
      this.strategyCrosshair = Math.round((pointerX - geometry.left) / Math.max(1, geometry.right - geometry.left) * (geometry.points.length - 1))
      this.drawStrategyChart()
    },
    clearStrategyCrosshair() {
      if (this.strategyCrosshair == null) return
      this.strategyCrosshair = null
      this.drawStrategyChart()
    },
    handleResize() {
      if (this.workspaceMode === "strategy") this.drawStrategyChart()
      else this.drawChart()
    },
    prepareCanvas() {
      const canvas = this.$refs.chart
      if (!canvas) return null
      const rect = canvas.getBoundingClientRect()
      const dpr = window.devicePixelRatio || 1
      const pixelWidth = Math.max(1, Math.floor(rect.width * dpr))
      const pixelHeight = Math.max(1, Math.floor(rect.height * dpr))
      if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
        canvas.width = pixelWidth
        canvas.height = pixelHeight
      }
      const context = canvas.getContext("2d")
      context.setTransform(dpr, 0, 0, dpr, 0, 0)
      context.fillStyle = "#171d21"
      context.fillRect(0, 0, rect.width, rect.height)
      return { context, width: rect.width, height: rect.height }
    },
    drawChart() {
      const canvas = this.prepareCanvas()
      if (!canvas) return
      this.chartGeometry = null
      if (this.chartMode === "intraday") this.drawIntradayChart(canvas.context, canvas.width, canvas.height)
      else this.drawDailyChart(canvas.context, canvas.width, canvas.height)
      this.drawCrosshair(canvas.context, canvas.width, canvas.height)
    },
    scheduleChartDraw() {
      if (this.crosshairFrame != null) return
      this.crosshairFrame = window.requestAnimationFrame(() => {
        this.crosshairFrame = null
        this.drawChart()
      })
    },
    handleChartPointer(event) {
      const geometry = this.chartGeometry
      const canvas = this.$refs.chart
      if (!geometry || !canvas || !geometry.items.length) return
      const rect = canvas.getBoundingClientRect()
      const pointerX = event.clientX - rect.left
      const pointerY = event.clientY - rect.top
      if (pointerX < geometry.left || pointerX > geometry.right || pointerY < geometry.top || pointerY > geometry.volumeBottom) {
        this.clearChartCrosshair()
        return
      }
      let nearestIndex = 0
      let nearestDistance = Number.POSITIVE_INFINITY
      geometry.items.forEach((item, index) => {
        const distance = Math.abs(geometry.xOf(item, index) - pointerX)
        if (distance < nearestDistance) {
          nearestDistance = distance
          nearestIndex = index
        }
      })
      const item = geometry.items[nearestIndex]
      this.crosshair = {
        mode: geometry.mode,
        x: geometry.xOf(item, nearestIndex),
        y: Math.max(geometry.top, Math.min(geometry.plotBottom, pointerY)),
        item,
      }
      this.scheduleChartDraw()
    },
    clearChartCrosshair() {
      if (!this.crosshair) return
      this.crosshair = null
      this.scheduleChartDraw()
    },
    drawCrosshair(context, width, height) {
      const geometry = this.chartGeometry
      const crosshair = this.crosshair
      if (!geometry || !crosshair || geometry.mode !== crosshair.mode) return
      let nearestIndex = 0
      let nearestDistance = Number.POSITIVE_INFINITY
      geometry.items.forEach((candidate, index) => {
        const distance = Math.abs(geometry.xOf(candidate, index) - crosshair.x)
        if (distance < nearestDistance) {
          nearestDistance = distance
          nearestIndex = index
        }
      })
      const item = geometry.items[nearestIndex]
      const x = geometry.xOf(item, nearestIndex)
      const y = Math.max(geometry.top, Math.min(geometry.plotBottom, crosshair.y))
      context.save()
      context.setLineDash([3, 4])
      context.lineWidth = 1
      context.strokeStyle = "#71838c"
      context.beginPath()
      context.moveTo(x, geometry.top)
      context.lineTo(x, geometry.volumeBottom)
      context.moveTo(geometry.left, y)
      context.lineTo(geometry.right, y)
      context.stroke()
      context.setLineDash([])

      const pointValue = Number(geometry.mode === "intraday" ? item.price : item.close)
      const referenceValue = geometry.referenceOf ? Number(geometry.referenceOf(item, nearestIndex)) : NaN
      const change = pointValue - referenceValue
      const percent = Number.isFinite(change) && Number.isFinite(referenceValue) && referenceValue > 0 ? change / referenceValue * 100 : NaN
      const changeText = Number.isFinite(percent)
        ? `${change > 0 ? "+" : ""}${this.number(change)}  ${percent > 0 ? "+" : ""}${this.number(percent)}%`
        : "--"
      const changeColor = change > 0 ? "#ef6b6b" : change < 0 ? "#48c5a0" : "#91a0a7"
      if (Number.isFinite(pointValue)) {
        context.beginPath()
        context.arc(x, geometry.yOf(pointValue), 3.5, 0, Math.PI * 2)
        context.fillStyle = "#58b9d7"
        context.fill()
        context.strokeStyle = "#d9f2fa"
        context.stroke()
      }

      context.font = '11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      const priceLabel = this.number(geometry.valueAtY(y))
      context.fillStyle = "#344249"
      context.fillRect(1, y - 9, Math.max(42, geometry.left - 4), 18)
      context.fillStyle = "#edf3f5"
      context.textAlign = "right"
      context.fillText(priceLabel, geometry.left - 7, y + 4)

      const xLabel = geometry.mode === "intraday" ? String(item.time || "--") : String(item.date || "--")
      const xLabelWidth = Math.max(48, context.measureText(xLabel).width + 14)
      const xLabelX = Math.max(geometry.left, Math.min(geometry.right - xLabelWidth, x - xLabelWidth / 2))
      context.fillStyle = "#344249"
      context.fillRect(xLabelX, height - 41, xLabelWidth, 18)
      context.fillStyle = "#edf3f5"
      context.textAlign = "center"
      context.fillText(xLabel, xLabelX + xLabelWidth / 2, height - 28)

      const rows = geometry.mode === "intraday"
        ? [{ label: "现价", value: this.number(item.price) }, { label: "涨跌", value: changeText, color: changeColor }, { label: "均价", value: this.number(item.average) }, { label: "分钟量", value: this.compact(item.volume) }]
        : [{ label: "开", value: this.number(item.open) }, { label: "高", value: this.number(item.high) }, { label: "低", value: this.number(item.low) }, { label: "收", value: this.number(item.close) }, { label: "涨跌", value: changeText, color: changeColor }, { label: "成交量", value: this.compact(item.volume) }]
      const boxWidth = geometry.mode === "intraday" ? 174 : 184
      const boxHeight = 30 + rows.length * 19
      let boxX = x + 13
      if (boxX + boxWidth > geometry.right) boxX = x - boxWidth - 13
      boxX = Math.max(geometry.left + 5, Math.min(geometry.right - boxWidth - 5, boxX))
      const boxY = Math.max(geometry.top + 5, Math.min(geometry.volumeBottom - boxHeight - 5, y - boxHeight / 2))
      context.fillStyle = "rgba(16, 22, 26, .94)"
      context.fillRect(boxX, boxY, boxWidth, boxHeight)
      context.strokeStyle = "#50626b"
      context.strokeRect(boxX + .5, boxY + .5, boxWidth - 1, boxHeight - 1)
      context.textAlign = "left"
      context.fillStyle = "#58b9d7"
      context.font = '600 11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      context.fillText(xLabel, boxX + 10, boxY + 19)
      context.font = '11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      rows.forEach((row, index) => {
        const rowY = boxY + 39 + index * 19
        context.fillStyle = "#91a0a7"
        context.textAlign = "left"
        context.fillText(row.label, boxX + 10, rowY)
        context.fillStyle = row.color || "#edf3f5"
        context.textAlign = "right"
        context.fillText(row.value, boxX + boxWidth - 10, rowY)
      })
      context.restore()
    },
    minuteSlot(time) {
      const match = String(time || "").match(/^(\d{2}):(\d{2})$/)
      if (!match) return null
      const total = Number(match[1]) * 60 + Number(match[2])
      if (total >= morningStart && total <= morningEnd) return total - morningStart
      if (total >= afternoonStart && total <= afternoonEnd) return morningSlots + total - afternoonStart
      return null
    },
    drawIntradayChart(context, width, height) {
      if (!this.minutes.length) {
        context.fillStyle = "#91a0a7"
        context.font = '12px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
        context.fillText("暂无分时数据", 18, 28)
        return
      }
      const left = 54
      const right = 16
      const top = 18
      const bottom = 52
      const volumeHeight = 74
      const plotBottom = height - bottom - volumeHeight
      const plotHeight = Math.max(20, plotBottom - top)
      const plotWidth = Math.max(20, width - left - right)
      const points = this.minutes.map(point => ({ ...point, slot: this.minuteSlot(point.time) })).filter(point => point.slot != null && Number.isFinite(Number(point.price)))
      if (!points.length) {
        context.fillStyle = "#91a0a7"
        context.font = '12px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
        context.fillText("暂无有效分时数据", 18, 28)
        return
      }
      const lunchGap = Math.min(52, Math.max(24, plotWidth * 0.06))
      const sessionWidth = (plotWidth - lunchGap) / 2
      const x = slot => {
        if (slot < morningSlots) return left + slot / (morningSlots - 1) * sessionWidth
        return left + sessionWidth + lunchGap + (slot - morningSlots) / (afternoonSlots - 1) * sessionWidth
      }
      const values = points.flatMap(point => [Number(point.price), Number(point.average)]).filter(Number.isFinite)
      const previousClose = Number(this.quote.previous_close)
      if (Number.isFinite(previousClose)) values.push(previousClose)
      let minimum = Math.min(...values)
      let maximum = Math.max(...values)
      const padding = (maximum - minimum) * 0.08 || Math.max(0.1, maximum * 0.002)
      minimum -= padding
      maximum += padding
      const y = value => top + (maximum - value) / (maximum - minimum) * plotHeight
      context.font = '11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      context.textAlign = "right"
      context.fillStyle = "#91a0a7"
      context.strokeStyle = "#2b363c"
      for (let index = 0; index < 5; index += 1) {
        const lineY = top + index * plotHeight / 4
        context.beginPath()
        context.moveTo(left, lineY)
        context.lineTo(width - right, lineY)
        context.stroke()
        context.fillText((maximum - (maximum - minimum) * index / 4).toFixed(2), left - 8, lineY + 4)
      }
      const maxVolume = Math.max(...points.map(point => Number(point.volume) || 0), 0)
      points.forEach(point => {
        const volume = Number(point.volume) || 0
        if (!maxVolume || !volume) return
        const barWidth = Math.max(1, (plotWidth - lunchGap) / totalSlots * 0.72)
        const barHeight = volume / maxVolume * volumeHeight
        context.globalAlpha = 0.42
        context.fillStyle = Number(point.price) >= previousClose ? "#ef6b6b" : "#48c5a0"
        context.fillRect(x(point.slot) - barWidth / 2, plotBottom + volumeHeight - barHeight, barWidth, barHeight)
        context.globalAlpha = 1
      })
      const drawLine = (field, color, widthValue) => {
        context.beginPath()
        let started = false
        let previousSlot = null
        points.forEach(point => {
          const value = Number(point[field])
          if (!Number.isFinite(value)) return
          const crossedLunch = previousSlot != null && previousSlot < morningSlots && point.slot >= morningSlots
          if (previousSlot != null && (point.slot - previousSlot > 1 || crossedLunch)) {
            context.strokeStyle = color
            context.lineWidth = widthValue
            context.stroke()
            context.beginPath()
            started = false
          }
          if (started) context.lineTo(x(point.slot), y(value))
          else { context.moveTo(x(point.slot), y(value)); started = true }
          previousSlot = point.slot
        })
        context.strokeStyle = color
        context.lineWidth = widthValue
        context.stroke()
      }
      drawLine("price", "#58b9d7", 1.8)
      drawLine("average", "#f0b768", 1.3)
      if (Number.isFinite(previousClose) && previousClose >= minimum && previousClose <= maximum) {
        context.setLineDash([5, 4])
        context.strokeStyle = "#91a0a7"
        context.lineWidth = 1
        context.beginPath()
        context.moveTo(left, y(previousClose))
        context.lineTo(width - right, y(previousClose))
        context.stroke()
        context.setLineDash([])
      }
      context.strokeStyle = "#2b363c"
      context.beginPath()
      context.moveTo(left, plotBottom)
      context.lineTo(width - right, plotBottom)
      context.stroke()
      const labels = width >= 560
        ? [[0, "09:30"], [60, "10:30"], [120, "11:30"], [121, "13:00"], [181, "14:00"], [241, "15:00"]]
        : [[0, "09:30"], [241, "15:00"]]
      context.textAlign = "center"
      context.fillStyle = "#91a0a7"
      labels.forEach(([slot, label]) => context.fillText(label, x(slot), height - 24))
      const lunchX = (x(120) + x(121)) / 2
      context.fillStyle = "#66757d"
      context.fillText(width >= 560 ? "午休" : "11:30 / 13:00", lunchX, top + 12)
      context.strokeStyle = "#3a474d"
      context.setLineDash([3, 5])
      context.beginPath()
      context.moveTo(lunchX, top)
      context.lineTo(lunchX, plotBottom)
      context.stroke()
      context.setLineDash([])
      this.chartGeometry = {
        mode: "intraday", items: points, left, right: width - right, top, plotBottom,
        volumeBottom: plotBottom + volumeHeight, xOf: point => x(point.slot), yOf: y,
        valueAtY: position => maximum - (position - top) / plotHeight * (maximum - minimum),
        referenceOf: () => Number(this.quote.previous_close),
      }
    },
    dailyAnnotations(bars) {
      if (bars.length < 3) return []
      const highs = bars.map(bar => Number(bar.high))
      const lows = bars.map(bar => Number(bar.low))
      const validHighs = highs.filter(Number.isFinite)
      const validLows = lows.filter(Number.isFinite)
      if (!validHighs.length || !validLows.length) return []
      const maximum = Math.max(...validHighs)
      const minimum = Math.min(...validLows)
      const range = maximum - minimum || Math.max(0.01, maximum * 0.01)
      const highIndex = highs.indexOf(maximum)
      const lowIndex = lows.indexOf(minimum)
      const selected = []
      const addSelected = (type, index, price, score, major = false) => {
        if (index < 0 || !Number.isFinite(price)) return
        if (!selected.some(item => item.type === type && item.index === index)) selected.push({ type, index, price, score, major })
      }
      addSelected("high", highIndex, maximum, range, true)
      addSelected("low", lowIndex, minimum, range, true)
      const radius = bars.length >= 100 ? 4 : 3
      const candidates = []
      for (let index = radius; index < bars.length - radius; index += 1) {
        const high = highs[index]
        const low = lows[index]
        if (!Number.isFinite(high) || !Number.isFinite(low)) continue
        const windowHigh = Math.max(...highs.slice(index - radius, index + radius + 1).filter(Number.isFinite))
        const windowLow = Math.min(...lows.slice(index - radius, index + radius + 1).filter(Number.isFinite))
        if (high >= windowHigh && high - windowLow >= range * 0.035) candidates.push({ type: "high", index, price: high, score: high - windowLow })
        if (low <= windowLow && windowHigh - low >= range * 0.035) candidates.push({ type: "low", index, price: low, score: windowHigh - low })
      }
      candidates.sort((left, right) => right.score - left.score)
      const minimumGap = Math.max(4, Math.floor(bars.length / 30))
      for (const candidate of candidates) {
        if (selected.length >= 8) break
        if (selected.some(item => Math.abs(item.index - candidate.index) < minimumGap)) continue
        selected.push(candidate)
      }
      return selected.sort((left, right) => left.index - right.index)
    },
    drawDailyAnnotations(context, bars, left, top, plotBottom, xStep, y) {
      const annotations = this.dailyAnnotations(bars)
      annotations.forEach(annotation => {
        const x = left + (annotation.index + .5) * xStep
        const priceY = y(annotation.price)
        const high = annotation.type === "high"
        const color = high ? "#ef6b6b" : "#48c5a0"
        const background = high ? "#342125" : "#18332d"
        const prefix = annotation.major ? (high ? "高 " : "低 ") : ""
        const label = `${prefix}${annotation.price.toFixed(2)}`
        context.font = '10px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
        const labelWidth = context.measureText(label).width + 10
        const labelX = Math.max(left, Math.min(left + bars.length * xStep - labelWidth, x - labelWidth / 2))
        const labelY = high ? Math.max(top + 12, priceY - 18) : Math.min(plotBottom - 4, priceY + 22)
        context.strokeStyle = color
        context.lineWidth = 1
        context.beginPath()
        context.moveTo(x, priceY)
        context.lineTo(x, high ? labelY + 3 : labelY - 13)
        context.stroke()
        context.fillStyle = color
        context.beginPath()
        context.arc(x, priceY, 2.5, 0, Math.PI * 2)
        context.fill()
        context.fillStyle = background
        context.fillRect(labelX, labelY - 12, labelWidth, 16)
        context.fillStyle = color
        context.textAlign = "center"
        context.fillText(label, labelX + labelWidth / 2, labelY)
      })
    },
    drawDailyKeyLevels(context, levels, width, right, top, plotBottom, y) {
      const labelHalfHeight = 9
      const minimumGap = 22
      const minimumY = top + labelHalfHeight
      const maximumY = plotBottom - labelHalfHeight
      const visible = levels
        .map(level => ({ ...level, price: Number(level.value) }))
        .filter(level => Number.isFinite(level.price) && level.price >= level.low && level.price <= level.high)
        .map(level => ({ ...level, lineY: y(level.price), labelY: y(level.price) }))
        .sort((left, rightLevel) => left.lineY - rightLevel.lineY)
      if (!visible.length) return

      visible.forEach((level, index) => {
        const previous = visible[index - 1]
        level.labelY = Math.max(minimumY, level.lineY, previous ? previous.labelY + minimumGap : minimumY)
      })
      if (visible[visible.length - 1].labelY > maximumY) {
        visible[visible.length - 1].labelY = maximumY
        for (let index = visible.length - 2; index >= 0; index -= 1) {
          visible[index].labelY = Math.min(visible[index].labelY, visible[index + 1].labelY - minimumGap)
        }
      }
      if (visible[0].labelY < minimumY) {
        const shift = minimumY - visible[0].labelY
        visible.forEach(level => { level.labelY += shift })
      }

      visible.forEach(level => {
        context.setLineDash(level.dash)
        context.strokeStyle = level.color
        context.lineWidth = 1
        context.beginPath()
        context.moveTo(level.left, level.lineY)
        context.lineTo(width - right, level.lineY)
        context.stroke()
        context.setLineDash([])
      })
      visible.forEach(level => {
        const labelLeft = width - right + 4
        if (Math.abs(level.labelY - level.lineY) > 1) {
          context.strokeStyle = level.color
          context.lineWidth = 1
          context.beginPath()
          context.moveTo(width - right, level.lineY)
          context.lineTo(labelLeft, level.labelY)
          context.stroke()
        }
        context.fillStyle = level.background
        context.fillRect(labelLeft, level.labelY - labelHalfHeight, right - 8, labelHalfHeight * 2)
        context.fillStyle = level.color
        context.font = '10px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
        context.textAlign = "left"
        context.fillText(`${level.label} ${level.price.toFixed(2)}`, labelLeft + 4, level.labelY + 4)
      })
    },
    drawDailyChart(context, width, height) {
      if (!this.bars.length) {
        context.fillStyle = "#91a0a7"
        context.fillText("暂无日 K 数据", 18, 28)
        return
      }
      const left = 48
      const right = width < 560 ? 70 : 94
      const top = 18
      const bottom = 54
      const volumeHeight = 74
      const plotBottom = height - bottom - volumeHeight
      const plotHeight = plotBottom - top
      const allBars = this.bars
      const viewport = this.dailyWindow()
      const bars = viewport.bars
      const firstSourceIndex = viewport.startIndex
      const values = []
      bars.forEach((bar, index) => {
        values.push(Number(bar.low), Number(bar.high))
        ;[5, 20, 60].forEach(length => {
          const value = this.movingAverage(allBars, firstSourceIndex + index, length)
          if (value) values.push(value)
        })
      })
      const minimum = Math.min(...values)
      const maximum = Math.max(...values)
      const padding = (maximum - minimum) * 0.08 || 1
      const low = minimum - padding
      const high = maximum + padding
      const xStep = (width - left - right) / bars.length
      const bodyWidth = Math.max(2, xStep * 0.58)
      const y = value => top + (high - value) / (high - low) * plotHeight
      context.strokeStyle = "#2b363c"
      context.fillStyle = "#91a0a7"
      context.font = '11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      context.textAlign = "right"
      for (let index = 0; index < 5; index += 1) {
        const lineY = top + index * plotHeight / 4
        context.beginPath()
        context.moveTo(left, lineY)
        context.lineTo(width - right, lineY)
        context.stroke()
        context.fillText((high - (high - low) * index / 4).toFixed(2), left - 8, lineY + 4)
      }
      context.textAlign = "center"
      const labelEvery = Math.max(1, Math.ceil(bars.length / 7))
      const maxVolume = Math.max(...bars.map(bar => Number(bar.volume) || 0))
      bars.forEach((bar, index) => {
        const x = left + (index + 0.5) * xStep
        if (index % labelEvery === 0) context.fillText(String(bar.date).slice(5), x, height - 26)
        const open = y(Number(bar.open))
        const close = y(Number(bar.close))
        const rising = Number(bar.close) >= Number(bar.open)
        context.strokeStyle = rising ? "#ef6b6b" : "#48c5a0"
        context.fillStyle = context.strokeStyle
        context.beginPath()
        context.moveTo(x, y(Number(bar.high)))
        context.lineTo(x, y(Number(bar.low)))
        context.stroke()
        context.fillRect(x - bodyWidth / 2, Math.min(open, close), bodyWidth, Math.max(1, Math.abs(close - open)))
        const volume = Number(bar.volume) || 0
        const barHeight = maxVolume ? volume / maxVolume * volumeHeight : 0
        context.globalAlpha = 0.55
        context.fillRect(x - bodyWidth / 2, plotBottom + volumeHeight - barHeight, bodyWidth, barHeight)
        context.globalAlpha = 1
      })
      const drawAverage = (length, color) => {
        context.beginPath()
        let started = false
        bars.forEach((bar, index) => {
          const value = this.movingAverage(allBars, firstSourceIndex + index, length)
          if (!value) return
          const x = left + (index + 0.5) * xStep
          if (started) context.lineTo(x, y(value))
          else { context.moveTo(x, y(value)); started = true }
        })
        context.strokeStyle = color
        context.lineWidth = 1.3
        context.stroke()
      }
      drawAverage(5, "#58b9d7")
      drawAverage(20, "#f0b768")
      drawAverage(60, "#b28ee8")
      this.drawDailyKeyLevels(context, [
        { value: this.dailyLevels.resistance, label: width < 560 ? "压" : "压力", color: "#ef6b6b", background: "#342125", dash: [6, 4], low, high, left },
        { value: this.dailyLevels.support, label: width < 560 ? "撑" : "支撑", color: "#48c5a0", background: "#18332d", dash: [6, 4], low, high, left },
        { value: this.dailyLevels.close, label: width < 560 ? "收" : "收盘", color: "#58b9d7", background: "#1b3038", dash: [2, 3], low, high, left },
      ], width, right, top, plotBottom, y)
      ;[this.quote.limit_up, this.quote.limit_down].forEach((value, index) => {
        const price = Number(value)
        if (!Number.isFinite(price) || price < low || price > high) return
        context.setLineDash([4, 4])
        context.strokeStyle = index === 0 ? "#ef6b6b" : "#48c5a0"
        context.beginPath()
        context.moveTo(left, y(price))
        context.lineTo(width - right, y(price))
        context.stroke()
        context.setLineDash([])
      })
      this.drawDailyAnnotations(context, bars, left, top, plotBottom, xStep, y)
      context.strokeStyle = "#2b363c"
      context.beginPath()
      context.moveTo(left, plotBottom)
      context.lineTo(width - right, plotBottom)
      context.stroke()
      this.chartGeometry = {
        mode: "daily", items: bars, left, right: width - right, top, plotBottom,
        plotWidth: width - left - right, startIndex: firstSourceIndex, endIndex: viewport.endIndex,
        volumeBottom: plotBottom + volumeHeight, xOf: (_bar, index) => left + (index + .5) * xStep, yOf: y,
        valueAtY: position => high - (position - top) / plotHeight * (high - low),
        referenceOf: (_bar, index) => {
          const previousIndex = firstSourceIndex + index - 1
          return previousIndex >= 0 ? Number(allBars[previousIndex].close) : NaN
        },
      }
    },
  },
  mounted() {
    this.load(this.requestedSymbol)
    this.loadIndices()
    this.loadWatchlist()
    this.timer = window.setInterval(() => {
      this.load(this.requestedSymbol)
      this.loadIndices()
      this.loadWatchlist()
      this.pollRealtimeSession()
    }, 10000)
    window.addEventListener("resize", this.handleResize)
  },
  beforeUnmount() {
    window.clearInterval(this.timer)
    if (this.crosshairFrame != null) window.cancelAnimationFrame(this.crosshairFrame)
    window.removeEventListener("resize", this.handleResize)
  },
}).mount("#app")
