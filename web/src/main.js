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
const globalMarketDefinitions = [
  { symbol: "rt_hkHSI", region: "港股", name: "恒生指数" },
  { symbol: "rt_hkHSTECH", region: "港股", name: "恒生科技" },
  { symbol: "b_NKY", region: "日本", name: "日经225" },
  { symbol: "b_KOSPI", region: "韩国", name: "KOSPI" },
  { symbol: "b_KOSDAQ", region: "韩国", name: "KOSDAQ" },
  { symbol: "gb_ixic", region: "美国", name: "纳斯达克" },
  { symbol: "gb_inx", region: "美国", name: "标普500" },
  { symbol: "gb_dji", region: "美国", name: "道琼斯" },
]
const automationTaskDefinitions = [
  { key: "scan", label: "扫描" },
  { key: "outcomes", label: "验证" },
  { key: "shadow", label: "影子" },
  { key: "research", label: "研究" },
]
const dailyRangeOptions = [
  { key: "1m", label: "1月", count: 20 },
  { key: "3m", label: "3月", count: 60 },
  { key: "6m", label: "6月", count: 120 },
  { key: "1y", label: "1年", count: 250 },
]
const globalRangeOptions = [
  { key: "1m", label: "1月", count: 22 },
  { key: "3m", label: "3月", count: 66 },
  { key: "6m", label: "6月", count: 132 },
  { key: "1y", label: "1年", count: 252 },
]
const defaultAIConfigForm = {
  enabled: true,
  execution_mode: "codex",
  provider: "openai",
  model: "gpt-5.5",
  quick_model: "gpt-5.5",
  deep_model: "gpt-5.5",
  base_url: "",
  codex_bin: "",
  codex_home: "",
  codex_profile: "",
  timeout_seconds: 600,
  reasoning_effort: "",
}
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
      workspaceMode: "dashboard",
      boardsLoading: false,
      boardsError: "",
      boardItems: [],
      boardSort: "hot",
      boardSortField: "percent",
      boardSortDirection: "desc",
      boardSelectedCode: "",
      boardHistory: [],
      boardMembers: [],
      boardMembersLoading: false,
      boardMembersError: "",
      boardsFetchedAt: 0,
      marketRankings: { gainers: [], losers: [], rapid_rise: [], amount: [], turnover: [] },
      marketRankingsLoading: false,
      marketRankingsError: "",
      marketRankingsFetchedAt: 0,
      sentimentSnapshot: null,
      sentimentHistory: [],
      sentimentLoading: false,
      sentimentError: "",
      sentimentRequestID: 0,
      sentimentFetchedAt: 0,
      sentimentComponents: [
        { key: "index_signal", label: "指数环境", note: "上证、深证、创业板综合", tone: "index" },
        { key: "turnover_signal", label: "成交额动能", note: "相对上一交易日成交额", tone: "turnover" },
        { key: "industry_breadth", label: "行业广度", note: "行业样本涨跌扩散", tone: "breadth" },
        { key: "positive_industry_rate", label: "上涨行业占比", note: "上涨行业 / 行业样本", tone: "positive" },
        { key: "industry_flow_signal", label: "行业资金方向", note: "主力净流入行业占比", tone: "flow" },
        { key: "northbound_signal", label: "北向资金信号", note: "盘中累计净流入标准化", tone: "northbound" },
      ],
      realtimeScope: "leaders",
      realtimeLoading: false,
      realtimeSnapshotLoading: false,
      realtimeError: "",
      realtimeResult: null,
      realtimeHistory: [],
      realtimeAutoAt: 0,
      realtimeSessionCheckedAt: 0,
      realtimeMarketState: "",
      realtimeTradingDay: null,
      realtimeCalendarKnown: null,
      realtimeScanAllowed: false,
      realtimeFrozen: false,
      realtimeNextScanAt: "",
      realtimeAutomation: { enabled: false, running: false, tasks: [], task_states: {} },
      automationTriggerLoading: false,
      realtimeOutcomeLoading: false,
      realtimeOutcomeError: "",
      realtimeOutcomeReport: null,
      realtimeOutcomeHorizon: 5,
      realtimeOutcomeView: "scores",
      realtimeSection: "candidates",
      realtimeMonsterOnly: false,
      shadowLoading: false,
      shadowError: "",
      shadowPreserveReason: "",
      shadowReport: null,
      shadowProfiles: [],
      shadowProfileID: "balanced",
      shadowRequestID: 0,
      shadowCheckedAt: 0,
      shadowOrderSymbol: "",
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
      globalRangeOptions,
      globalChartMode: "intraday",
      globalSelectedSymbol: globalMarketDefinitions[0].symbol,
      globalChartData: {},
      globalChartLoading: false,
      globalChartError: "",
      globalChartStatus: "",
      globalChartFetchedAt: "",
      globalChartLastRequestedAt: 0,
      globalChartRequestID: 0,
      globalDailyRangePreset: "6m",
      globalDailyVisibleCount: 132,
      globalCrosshair: null,
      globalChartGeometry: null,
      assistantOpen: false,
      assistantLoading: false,
      assistantSending: false,
      assistantContext: null,
      assistantConversation: [],
      assistantAlerts: [],
      assistantDraft: "",
      assistantError: "",
      assistantJobID: "",
      assistantJobStatus: "",
      assistantJobProgress: "",
      assistantContextRequestID: 0,
      assistantAlertsRequestID: 0,
      assistantAlertsCheckedAt: 0,
      assistantContextCheckedAt: 0,
      assistantJobTimer: null,
      assistantUnread: 0,
      assistantAlertSeen: new Set(),
      assistantAlertInitialized: false,
      aiConfigLoading: false,
      aiConfigSaving: false,
      aiConfigTesting: false,
      aiConfigError: "",
      aiConfigNotice: "",
      aiConfigSnapshot: null,
      aiConfigProviders: [],
      aiConfigProfiles: [],
      aiConfigForm: { ...defaultAIConfigForm },
      aiConfigToken: "",
      aiConfigClearToken: false,
      aiConfigTestResult: null,
      aiConfigLoaded: false,
      watchlistOpen: window.innerWidth > 1100,
      watchlist: { groups: [] },
      selectedWatchlistGroup: "全部",
      watchlistInput: "",
      watchlistLoading: false,
      watchlistRefreshing: false,
      watchlistRequestID: 0,
      watchlistError: "",
      marketIndices: marketIndexDefinitions.map(item => ({ ...item })),
      globalMarkets: [],
      globalMarketsFetchedAt: "",
      globalMarketsError: "",
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
      strategyEntryModes: [
        { id: "breakout", label: "放量突破" },
        { id: "trend-reclaim", label: "趋势收复" },
        { id: "ma-pullback", label: "均线回踩" },
        { id: "momentum-continuation", label: "动量延续" },
        { id: "mean-reversion", label: "均值回归反弹" },
        { id: "volatility-squeeze", label: "波动收缩突破" },
        { id: "adaptive-ensemble", label: "自适应多形态" },
      ],
      strategyLoading: false,
      strategyError: "",
      strategyResult: null,
      strategyAssessment: null,
      strategyHistory: [],
      strategyHistoryLoading: false,
      strategyHistoryLoaded: false,
	  strategyCandidates: [],
	  strategyCandidatesLoading: false,
	  strategyCandidatesLoaded: false,
	  strategyCandidateError: "",
	  strategyCandidateID: "",
	  strategyCandidateActionLoading: "",
	  strategyCandidateAutoRefreshing: false,
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
    assistantSymbol() {
      return String(this.data.symbol || this.requestedSymbol || defaultSymbol || "").trim()
    },
    assistantName() {
      return String((this.assistantContext && this.assistantContext.name) || this.quote.name || this.data.name || this.displayCode(this.assistantSymbol) || "当前股票")
    },
    assistantFacts() {
      return this.assistantContext && this.assistantContext.facts ? this.assistantContext.facts : {}
    },
    assistantQuote() {
      return this.assistantFacts.quote || {}
    },
    assistantTechnical() {
      return this.assistantFacts.technical || {}
    },
    assistantFundAvailable() {
      const fund = this.assistantFacts.fund || {}
      const warnings = Array.isArray(this.assistantFacts.warnings) ? this.assistantFacts.warnings : []
      return Boolean(fund.symbol) && !warnings.some(item => String(item || "").includes("主力资金"))
    },
    assistantContextAge() {
      if (!this.assistantContextCheckedAt) return "等待上下文"
      const seconds = Math.max(0, Math.round((Date.now() - this.assistantContextCheckedAt) / 1000))
      if (seconds < 5) return "刚刚采集"
      if (seconds < 60) return `${seconds}秒前采集`
      return `${Math.round(seconds / 60)}分钟前采集`
    },
    assistantVisibleAlerts() {
      return Array.isArray(this.assistantAlerts) ? this.assistantAlerts.slice(0, 12) : []
    },
    assistantLevels() {
      return this.assistantContext && Array.isArray(this.assistantContext.key_levels) ? this.assistantContext.key_levels : []
    },
    assistantTurns() {
      return Array.isArray(this.assistantConversation) ? this.assistantConversation : []
    },
    assistantBusy() {
      return this.assistantSending || ["queued", "running"].includes(this.assistantJobStatus)
    },
    sentimentReadout() {
      const snapshot = this.sentimentSnapshot || {}
      const score = Number(snapshot.score)
      const index = Number(snapshot.index_signal)
      const breadth = Number(snapshot.industry_breadth)
      const positiveRate = Number(snapshot.positive_industry_rate)
      const tags = []
      if (Number.isFinite(index) && Number.isFinite(breadth) && Math.abs(index - breadth) >= 12) {
        tags.push({ text: index > breadth ? "权重强于小票" : "小票扩散领先", tone: index > breadth ? "warn" : "up" })
      }
      if (Number.isFinite(positiveRate)) tags.push({ text: `上涨行业 ${positiveRate.toFixed(0)}%`, tone: positiveRate >= 50 ? "up" : "down" })
      if (Number.isFinite(Number(snapshot.industry_flow_signal))) tags.push({ text: Number(snapshot.industry_flow_signal) >= 50 ? "资金偏流入" : "资金偏流出", tone: Number(snapshot.industry_flow_signal) >= 50 ? "up" : "down" })
      if (!tags.length) tags.push({ text: "等待更多盘中样本", tone: "muted" })
      if (!Number.isFinite(score)) return { title: "数据不足，暂不判断", detail: "当前行情源尚未形成有效的情绪合成结果。", tags }
      if (score >= 75) return { title: "情绪偏热，注意追高风险", detail: "综合分已进入强势区间，优先观察量能是否继续放大以及强势行业是否扩散。", tags }
      if (score >= 55) return { title: "市场处于修复或强势段", detail: "指数与行业扩散大体同向，回撤时关注行业广度能否保持在中轴上方。", tags }
      if (score >= 40) return { title: "情绪在中轴附近震荡", detail: "市场尚未形成一致方向，适合等待行业扩散和资金方向同时改善。", tags }
      return { title: "情绪偏弱，等待止跌信号", detail: "当前分数位于弱势区间，避免仅凭单一指数上涨判断市场已经反转。", tags }
    },
    aiConfigStatusText() {
      if (this.aiConfigLoading) return "读取中"
      if (!this.aiConfigSnapshot) return "未读取"
      if (!this.aiConfigSnapshot.config || !this.aiConfigSnapshot.config.enabled) return "已停用"
      return this.aiConfigSnapshot.token_configured ? "已配置" : (this.aiConfigSnapshot.config.execution_mode === "codex" ? "待测试" : "待凭证")
    },
    aiConfigTokenStatus() {
      if (this.aiConfigToken) return "待保存"
      if (this.aiConfigSnapshot && this.aiConfigSnapshot.token_configured) return this.aiConfigSnapshot.token_hint || "已配置"
      return this.aiConfigForm.execution_mode === "codex" ? "由 Profile 提供" : "未配置"
    },
    assistantRuntimeLabel() {
      const config = this.aiConfigSnapshot && this.aiConfigSnapshot.config
      if (!config) return "AI / READ ONLY"
      if (config.execution_mode === "api") return `${String(config.provider || "API").toUpperCase()} / READ ONLY`
      return "CODEX / READ ONLY"
    },
    globalSelectedMarket() {
      return this.globalMarkets.find(item => item && item.symbol === this.globalSelectedSymbol) || null
    },
    globalBars() { return Array.isArray(this.globalChartData.bars) ? this.globalChartData.bars : [] },
    globalMinutes() { return Array.isArray(this.globalChartData.minutes) ? this.globalChartData.minutes : [] },
    globalLatestBar() { return this.globalBars[this.globalBars.length - 1] || {} },
    globalLatestMinute() { return this.globalMinutes[this.globalMinutes.length - 1] || {} },
    sortedBoardItems() {
      const items = Array.isArray(this.boardItems) ? [...this.boardItems] : []
      const field = this.boardSortField
      const direction = this.boardSortDirection === "asc" ? 1 : -1
      const value = item => {
        if (field === "name") return String(item && item.name || "")
        if (field === "rise") return Number(item && item.rise_count) || 0
        if (field === "fall") return Number(item && item.fall_count) || 0
        if (field === "flow") return Number(item && item.main_net_yuan)
        return Number(item && item.percent)
      }
      items.sort((left, right) => {
        const lv = value(left); const rv = value(right)
        if (typeof lv === "string" || typeof rv === "string") return direction * String(lv).localeCompare(String(rv), "zh-CN")
        const lFinite = Number.isFinite(lv); const rFinite = Number.isFinite(rv)
        if (!lFinite && !rFinite) return 0
        if (!lFinite) return 1
        if (!rFinite) return -1
        return (lv - rv) * direction
      })
      return items
    },
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
	strategyActiveCandidate() {
	  return this.strategyCandidates.find(item => item && item.experiment_id === this.strategyCandidateID) || this.strategyCandidates[0] || null
	},
	strategyCandidateLifecycle() { return (this.strategyActiveCandidate && this.strategyActiveCandidate.lifecycle) || {} },
    strategyCandidateAssessment() { return this.strategyCandidateLifecycle.assessment || { checks: [] } },
    strategyCandidateObservationMetrics() { return (this.strategyActiveCandidate && this.strategyActiveCandidate.observation_metrics) || {} },
	strategyLifecycleHeadline() {
	  if (!this.strategyCandidatesLoaded) return "候选生命周期"
	  if (!this.strategyCandidates.length) return "等待持续优化实验"
	  return this.candidateStatusLabel(this.strategyCandidateLifecycle.status, this.strategyActiveCandidate.research_stage)
	},
    realtimeSignals() { return this.realtimeResult && Array.isArray(this.realtimeResult.signals) ? this.realtimeResult.signals : [] },
    marketPulseSignals() {
      return [...this.realtimeSignals]
        .filter(item => item && item.symbol)
        .sort((left, right) => Number(right.risk_adjusted_score || right.score || 0) - Number(left.risk_adjusted_score || left.score || 0))
        .slice(0, 5)
    },
    marketPulseRadarItems() {
      const snapshot = this.sentimentSnapshot || {}
      return [
        { key: "index_signal", label: "指数", value: snapshot.index_signal, tone: "index" },
        { key: "industry_breadth", label: "宽度", value: snapshot.industry_breadth, tone: "breadth" },
        { key: "turnover_signal", label: "成交额", value: snapshot.turnover_signal, tone: "turnover" },
        { key: "industry_flow_signal", label: "资金", value: snapshot.industry_flow_signal, tone: "flow" },
        { key: "northbound_signal", label: "北向", value: snapshot.northbound_signal, tone: "northbound" },
      ]
    },
    marketPulseBreadthRate() {
      const value = this.sentimentSnapshot && Number(this.sentimentSnapshot.positive_industry_rate)
      return Number.isFinite(value) ? Math.max(0, Math.min(100, value)) : null
    },
    marketRankingColumns() {
      return [
        { key: "gainers", label: "涨幅榜", tone: "up", metric: "percent", items: this.marketRankings.gainers || [] },
        { key: "losers", label: "跌幅榜", tone: "down", metric: "percent", items: this.marketRankings.losers || [] },
        { key: "amount", label: "成交额榜", tone: "", metric: "amount", items: this.marketRankings.amount || [] },
        { key: "turnover", label: "活跃换手", tone: "warn-text", metric: "turnover", items: this.marketRankings.turnover || [] },
      ]
    },
    realtimeScopeLabel() {
      return this.realtimeScope === "watchlist" ? "仅自选" : "自选 + 强势候选"
    },
    realtimeFilterLabel() {
      return this.realtimeMonsterOnly ? `${this.realtimeScopeLabel} · 抓妖观察` : this.realtimeScopeLabel
    },
    realtimeSnapshotScopeLabel() {
      const universe = this.realtimeResult && String(this.realtimeResult.universe || "").toLowerCase()
      if (universe === "watchlist") return "仅自选"
      if (universe === "watchlist+leaders") return "自选 + 强势候选"
      return universe || this.realtimeScopeLabel
    },
    realtimeScopeSignals() {
      const signals = this.realtimeSignals
      if (this.realtimeScope !== "watchlist") return signals

      // The scanner records the source of every candidate. Older archived
      // snapshots do not have that field, so fall back to the current full
      // watchlist by code to keep the scope switch useful across upgrades.
      const watchlistCodes = new Set()
      const groups = this.watchlist && Array.isArray(this.watchlist.groups) ? this.watchlist.groups : []
      groups.forEach(group => {
        const items = Array.isArray(group && group.items) ? group.items : []
        const symbols = items.length ? items.map(item => item && item.symbol) : (Array.isArray(group && group.symbols) ? group.symbols : [])
        symbols.forEach(symbol => {
          const code = this.displayCode(symbol)
          if (code) watchlistCodes.add(code)
        })
      })
      return signals.filter(item => {
        if (!item || !item.symbol) return false
        const sources = Array.isArray(item.candidate_sources) ? item.candidate_sources : []
        if (sources.some(source => String(source).trim() === "自选")) return true
        return watchlistCodes.has(this.displayCode(item.symbol))
      })
    },
    realtimeVisibleSignals() {
      if (!this.realtimeMonsterOnly) return this.realtimeScopeSignals
      return this.realtimeScopeSignals.filter(item => item && item.monster && item.monster.eligible === true)
    },
    realtimeFilterEmptyText() {
      if (this.realtimeMonsterOnly) return "当前范围没有满足边界条件的抓妖观察候选"
      if (this.realtimeScope === "watchlist") return "当前快照没有匹配的自选候选"
      return "当前快照没有候选信号"
    },
    realtimeMonsterCandidates() {
      const threshold = Number(this.realtimeResult && this.realtimeResult.monster_minimum_score) || 58
      return this.realtimeScopeSignals.filter(item => {
        const radar = item && item.monster
        return radar && radar.stage && radar.stage !== "数据不足" && Number(radar.score) >= threshold
      }).length
    },
    realtimeMonsterEligible() {
      return this.realtimeScopeSignals.filter(item => item && item.monster && item.monster.eligible === true).length
    },
    realtimeAutomationTasks() {
      const states = this.realtimeAutomation && this.realtimeAutomation.task_states && typeof this.realtimeAutomation.task_states === "object"
        ? this.realtimeAutomation.task_states
        : {}
      return automationTaskDefinitions.map(definition => ({
        ...definition,
        ...(states[definition.key] || {}),
        status: (states[definition.key] && states[definition.key].status) || "waiting",
      }))
    },
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
      const calendar = this.realtimeCalendarKnown === false ? " · 交易日历未覆盖，按工作日降级" : ""
      const automation = this.realtimeAutomation && this.realtimeAutomation.enabled ? " · 服务端自动运行" : ""
      const automationError = this.realtimeAutomation && this.realtimeAutomation.last_error ? " · 自动任务异常" : ""
      return `${state}${frozen}${next}${calendar}${automation}${automationError}`
    },
    realtimeOutcomeSummaries() { return this.realtimeOutcomeReport && Array.isArray(this.realtimeOutcomeReport.summaries) ? this.realtimeOutcomeReport.summaries : [] },
    realtimeOutcomeSummary() {
      return this.realtimeOutcomeSummaries.find(item => Number(item.horizon) === Number(this.realtimeOutcomeHorizon)) || this.realtimeOutcomeSummaries[0] || {}
    },
    realtimeCalibration() {
      return (this.realtimeOutcomeReport && this.realtimeOutcomeReport.calibration) || {}
    },
    realtimeTuning() {
      return (this.realtimeOutcomeReport && this.realtimeOutcomeReport.tuning) || {}
    },
    realtimeMonsterAnalysis() {
      return (this.realtimeOutcomeReport && this.realtimeOutcomeReport.monster_analysis) || {}
    },
    realtimeOutcomeBreakdowns() {
      if (!this.realtimeOutcomeReport) return []
      const key = this.realtimeOutcomeView === "strategies" ? "strategies" : this.realtimeOutcomeView === "regimes" ? "market_regimes" : this.realtimeOutcomeView === "monster" ? "monster_stages" : "score_buckets"
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
    shadowPositions() {
      const positions = this.shadowReport && Array.isArray(this.shadowReport.positions) ? this.shadowReport.positions : []
      return positions.slice().sort((left, right) => Number(right.market_value || 0) - Number(left.market_value || 0))
    },
    shadowOrders() { return this.shadowReport && Array.isArray(this.shadowReport.orders) ? this.shadowReport.orders : [] },
    shadowFilteredOrders() {
      if (!this.shadowOrderSymbol) return this.shadowOrders
      return this.shadowOrders.filter(item => item && item.symbol === this.shadowOrderSymbol)
    },
    shadowOrderPosition() {
      return this.shadowPositions.find(item => item && item.symbol === this.shadowOrderSymbol) || null
    },
    shadowRejections() { return this.shadowReport && Array.isArray(this.shadowReport.rejections) ? this.shadowReport.rejections : [] },
    shadowDecisions() { return this.shadowReport && Array.isArray(this.shadowReport.decisions) ? this.shadowReport.decisions : [] },
    shadowLatestDecision() {
      const realtime = this.shadowDecisions.filter(item => item && item.event_source === 'realtime')
      const items = realtime.length ? realtime : this.shadowDecisions
      return items.slice().sort((left, right) => String(left.event_time || left.date || '').localeCompare(String(right.event_time || right.date || ''))).pop() || null
    },
    shadowLatestOrder() {
      const realtime = this.shadowOrders.filter(item => item && String(item.event_source || '').startsWith('realtime'))
      const items = realtime.length ? realtime : this.shadowOrders
      return items.slice().sort((left, right) => String(left.execution_time || left.attempt_date || '').localeCompare(String(right.execution_time || right.attempt_date || ''))).pop() || null
    },
    shadowAutomationTask() {
      return this.realtimeAutomationTasks.find(item => item.key === 'shadow') || { status: 'waiting', detail: '等待服务端调度' }
    },
    shadowIndustryExposures() { return this.shadowReport && Array.isArray(this.shadowReport.industry_exposures) ? this.shadowReport.industry_exposures : [] },
    shadowConfig() { return (this.shadowReport && this.shadowReport.config) || {} },
    shadowSelectedProfile() {
      return this.shadowProfiles.find(item => item && item.id === this.shadowProfileID) || { id: this.shadowProfileID, name: this.shadowProfileID, strategy: "", description: "" }
    },
    shadowInvestedPercent() {
      const report = this.shadowReport || {}
      const equity = Number(report.total_equity)
      const marketValue = Number(report.total_market_value)
      return Number.isFinite(equity) && equity > 0 && Number.isFinite(marketValue) ? marketValue / equity * 100 : 0
    },
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
      const closes = this.bars.slice(Math.max(0, currentIndex - 60), currentIndex + 1).map(bar => Number(bar.close)).filter(Number.isFinite)
      const latest = Number(this.visibleLastBar.close)
      const high20 = highs.length ? Math.max(...highs) : null
      const low20 = lows.length ? Math.min(...lows) : null
      const mean20 = closes.length >= 20 ? closes.slice(-20).reduce((sum, value) => sum + value, 0) / 20 : null
      const std20 = mean20 == null ? null : Math.sqrt(closes.slice(-20).reduce((sum, value) => sum + (value - mean20) ** 2, 0) / 20)
      const ema = (period) => {
        if (closes.length < period) return null
        let value = closes[0]
        const alpha = 2 / (period + 1)
        for (const close of closes.slice(1)) value = alpha * close + (1 - alpha) * value
        return value
      }
      const ema20 = ema(20)
      const trueRangeStart = Math.max(1, currentIndex - 60)
      const trueRanges = this.bars.slice(trueRangeStart, currentIndex + 1).map((bar, offset) => {
        const previous = this.bars[trueRangeStart + offset - 1]
        const previousClose = Number(previous?.close)
        const high = Number(bar.high); const low = Number(bar.low)
        return Number.isFinite(previousClose) ? Math.max(high - low, Math.abs(high - previousClose), Math.abs(low - previousClose)) : NaN
      }).filter(Number.isFinite)
      const atr14 = trueRanges.length >= 14 ? trueRanges.slice(-14).reduce((sum, value) => sum + value, 0) / 14 : null
      const pivot = high20 != null && low20 != null && Number.isFinite(latest) ? (high20 + low20 + latest) / 3 : null
      let gap = null
      for (let index = currentIndex; index > 0; index -= 1) {
        const current = this.bars[index]; const previous = this.bars[index - 1]
        const low = Number(current.low); const high = Number(current.high); const previousHigh = Number(previous.high); const previousLow = Number(previous.low)
        if (low > previousHigh && (gap == null || Math.abs(low - latest) < Math.abs(gap - latest))) gap = low
        if (high < previousLow && (gap == null || Math.abs(high - latest) < Math.abs(gap - latest))) gap = high
      }
      const fib382 = high20 != null && low20 != null ? low20 + (high20 - low20) * .382 : null
      const fib618 = high20 != null && low20 != null ? low20 + (high20 - low20) * .618 : null
      const formatLevel = (value) => value == null || !Number.isFinite(value) ? "--" : this.number(value)
      return {
        resistance: high20,
        support: low20,
        close: latest,
        keyLevels: [
          { label: "压力 / 支撑", value: `${formatLevel(high20)} / ${formatLevel(low20)}` },
          { label: "成交密集区", value: formatLevel(mean20) },
          { label: "枢轴点", value: formatLevel(pivot) },
          { label: "前高 / 前低", value: `${formatLevel(high20)} / ${formatLevel(low20)}` },
          { label: "Keltner 通道", value: ema20 == null || atr14 == null ? "--" : `${formatLevel(ema20 + 2 * atr14)} / ${formatLevel(ema20 - 2 * atr14)}` },
          { label: "ATR 波动通道", value: !Number.isFinite(latest) || atr14 == null ? "--" : `${formatLevel(latest + 2 * atr14)} / ${formatLevel(latest - 2 * atr14)}` },
          { label: "缺口位", value: formatLevel(gap) },
          { label: "斐波那契", value: `${formatLevel(fib382)} / ${formatLevel(fib618)}` },
          { label: "整数关口", value: Number.isFinite(latest) ? this.number(Math.round(latest)) : "--" },
        ],
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
    priceText(value) {
      return Number.isFinite(Number(value)) ? Number(value).toFixed(2) : "--"
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
    intradayWhiteLabel() {
      return this.isMarketIndex(this.data.symbol) ? "白线·上证指数" : "白线·实时价"
    },
    intradayYellowLabel() {
      return this.isMarketIndex(this.data.symbol) ? "黄线·上证领先" : "黄线·均价"
    },
    formatDateTime(value) {
      if (!value) return "--"
      const date = new Date(value)
      if (Number.isNaN(date.getTime())) return String(value)
      return date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false })
    },
    applyAIConfigSnapshot(payload) {
      if (!payload || typeof payload !== "object") return
      const config = payload.config && typeof payload.config === "object" ? payload.config : payload
      this.aiConfigSnapshot = payload
      this.aiConfigProviders = Array.isArray(payload.providers) ? payload.providers : this.aiConfigProviders
      this.aiConfigProfiles = Array.isArray(payload.profiles) ? payload.profiles : []
      this.aiConfigForm = {
        ...defaultAIConfigForm,
        ...Object.fromEntries(Object.keys(defaultAIConfigForm).map(key => [key, config[key] !== undefined ? config[key] : defaultAIConfigForm[key]])),
      }
      this.aiConfigLoaded = true
    },
    aiProviderChanged() {
      const provider = this.aiConfigProviders.find(item => item && item.id === this.aiConfigForm.provider)
      if (!provider) return
      if (!this.aiConfigForm.base_url) this.aiConfigForm.base_url = provider.default_base_url || ""
      if (!this.aiConfigForm.model && Array.isArray(provider.models) && provider.models.length) this.aiConfigForm.model = provider.models[0]
      if (!this.aiConfigForm.quick_model) this.aiConfigForm.quick_model = this.aiConfigForm.model
      if (!this.aiConfigForm.deep_model) this.aiConfigForm.deep_model = this.aiConfigForm.model
    },
    selectAIProfile(profile) {
      if (!profile || !profile.path) return
      this.aiConfigForm.execution_mode = "codex"
      this.aiConfigForm.codex_home = profile.path
      if (profile.model) {
        this.aiConfigForm.model = profile.model
        this.aiConfigForm.quick_model = profile.model
        this.aiConfigForm.deep_model = profile.model
      }
      if (profile.base_url) this.aiConfigForm.base_url = profile.base_url
    },
    aiConfigPayload() {
      const config = {
        ...defaultAIConfigForm,
        ...this.aiConfigForm,
        timeout_seconds: Number(this.aiConfigForm.timeout_seconds) || 600,
      }
      const payload = { config, clear_token: Boolean(this.aiConfigClearToken) }
      if (this.aiConfigToken) payload.token = this.aiConfigToken
      return payload
    },
    async loadAIConfig(force = false) {
      if (this.aiConfigLoading || (this.aiConfigLoaded && !force)) return
      this.aiConfigLoading = true
      this.aiConfigError = ""
      try {
        const response = await fetch("/api/ai/config", { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "AI 配置读取失败")
        this.applyAIConfigSnapshot(payload)
      } catch (error) {
        this.aiConfigError = error instanceof Error ? error.message : String(error)
      } finally {
        this.aiConfigLoading = false
      }
    },
    async saveAIConfig() {
      if (this.aiConfigSaving || this.aiConfigTesting) return
      this.aiConfigSaving = true
      this.aiConfigError = ""
      this.aiConfigNotice = ""
      try {
        const response = await fetch("/api/ai/config", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(this.aiConfigPayload()), cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "AI 配置保存失败")
        this.applyAIConfigSnapshot(payload)
        this.aiConfigToken = ""
        this.aiConfigClearToken = false
        this.aiConfigNotice = "配置已保存，后续新请求立即使用。"
      } catch (error) {
        this.aiConfigError = error instanceof Error ? error.message : String(error)
      } finally {
        this.aiConfigSaving = false
      }
    },
    async testAIConfig() {
      if (this.aiConfigTesting || this.aiConfigSaving) return
      this.aiConfigTesting = true
      this.aiConfigError = ""
      this.aiConfigNotice = ""
      this.aiConfigTestResult = null
      try {
        const response = await fetch("/api/ai/config/test", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(this.aiConfigPayload()), cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok && !payload.message) throw new Error(payload.error || "AI 连接测试失败")
        this.aiConfigTestResult = payload
      } catch (error) {
        this.aiConfigTestResult = { ok: false, message: error instanceof Error ? error.message : String(error) }
      } finally {
        this.aiConfigTesting = false
      }
    },
    async resetAIConfig() {
      if (this.aiConfigSaving || this.aiConfigTesting || !window.confirm("恢复默认 AI 配置并清除本机 Token？")) return
      this.aiConfigSaving = true
      this.aiConfigError = ""
      this.aiConfigNotice = ""
      try {
        const response = await fetch("/api/ai/config/reset", { method: "POST", cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "AI 默认配置恢复失败")
        this.applyAIConfigSnapshot(payload)
        this.aiConfigToken = ""
        this.aiConfigClearToken = false
        this.aiConfigTestResult = null
        this.aiConfigNotice = "已恢复默认配置，Token 已清除。"
      } catch (error) {
        this.aiConfigError = error instanceof Error ? error.message : String(error)
      } finally {
        this.aiConfigSaving = false
      }
    },
    setAssistantBodyLock(locked) {
      if (typeof document === "undefined") return
      document.documentElement.classList.toggle("assistant-open", Boolean(locked))
      document.body.classList.toggle("assistant-open", Boolean(locked))
    },
    assistantToggle() {
      this.assistantOpen = !this.assistantOpen
      this.setAssistantBodyLock(this.assistantOpen)
      if (this.assistantOpen) {
        this.assistantUnread = 0
        this.loadAssistantContext(this.assistantSymbol)
        this.loadAssistantAlerts(true)
        this.$nextTick(() => this.$refs.assistantInput?.focus())
      }
    },
    assistantClose() {
      this.assistantOpen = false
      this.setAssistantBodyLock(false)
    },
    assistantAlertClass(alert) {
      if (!alert) return "info"
      return alert.severity === "high" ? "high" : alert.severity === "medium" ? "medium" : "low"
    },
    assistantAlertKindLabel(alert) {
      if (!alert) return "提示"
      if (alert.kind === "risk") return "风险"
      if (alert.kind === "level" || alert.kind === "fact-level") return "点位"
      return alert.state === "triggered" ? "触发" : "观察"
    },
    assistantAgentStatusLabel(status) {
      return ({ ok: "完成", running: "分析中", failed: "失败", unavailable: "不可用" })[status] || status || "--"
    },
    assistantMarketStateLabel(state) {
      return ({ trading: "交易中", auction: "集合竞价", break: "午间休市", closed: "已收盘", weekend: "周末" })[state] || state || "状态未知"
    },
    assistantShortHash(value) {
      const text = String(value || "")
      return text.length > 18 ? `${text.slice(0, 18)}…` : text
    },
    assistantLevelClass(level) {
      if (!level) return ""
      if (level.kind === "invalidation" || level.kind === "limit_down") return "risk"
      if (level.kind === "buy_trigger" || level.kind === "support") return "support"
      if (level.kind === "limit_up" || level.kind === "resistance") return "resistance"
      return "observe"
    },
    assistantLevelText(level) {
      return level && level.text ? level.text : "数据暂缺"
    },
    assistantSignalName(alert) {
      if (!alert) return ""
      return alert.name || this.displayCode(alert.symbol)
    },
    assistantPrompt(question) {
      if (this.assistantBusy) return
      this.assistantDraft = question
      this.$nextTick(() => this.$refs.assistantInput?.focus())
    },
    scrollAssistantThread() {
      this.$nextTick(() => {
        const scroll = this.$refs.assistantScroll
        const thread = this.$refs.assistantThread
        const target = scroll || thread
        if (!target) return
        const top = target.scrollHeight
        const reducedMotion = typeof window !== "undefined" && window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches
        if (typeof target.scrollTo === "function") {
          target.scrollTo({ top, behavior: reducedMotion ? "auto" : "smooth" })
        } else {
          target.scrollTop = top
        }
      })
    },
    async loadAssistantContext(symbol = this.assistantSymbol) {
      const target = String(symbol || "").trim()
      if (!target) return
      const requestID = ++this.assistantContextRequestID
      this.assistantLoading = true
      this.assistantError = ""
      try {
        const response = await fetch(`/api/assistant/context?symbol=${encodeURIComponent(target)}`, { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "AI上下文读取失败")
        if (requestID !== this.assistantContextRequestID) return
        this.assistantContext = payload
        this.assistantConversation = Array.isArray(payload.history) ? payload.history : []
        if (Array.isArray(payload.alerts)) this.assistantAlerts = payload.alerts
        this.assistantContextCheckedAt = Date.now()
        this.assistantAlertsCheckedAt = Date.now()
      } catch (error) {
        if (requestID === this.assistantContextRequestID) this.assistantError = error instanceof Error ? error.message : String(error)
      } finally {
        if (requestID === this.assistantContextRequestID) this.assistantLoading = false
      }
    },
    async loadAssistantAlerts(force = false) {
      if (this.assistantAlertsRequestID && !force && Date.now() - this.assistantAlertsCheckedAt < 7000) return
      const requestID = ++this.assistantAlertsRequestID
      try {
        const response = await fetch("/api/assistant/alerts", { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "实时提示读取失败")
        if (requestID !== this.assistantAlertsRequestID) return
        const signalAlerts = Array.isArray(payload.alerts) ? payload.alerts : []
        const contextualAlerts = this.assistantOpen && this.assistantContext && Array.isArray(this.assistantContext.alerts)
          ? this.assistantContext.alerts.filter(item => item && item.kind === "fact-level" && String(item.symbol || "").toLowerCase() === String(this.assistantContext.symbol || this.assistantSymbol || "").toLowerCase())
          : []
        const merged = new Map()
        ;[...signalAlerts, ...contextualAlerts].forEach(item => { if (item && item.id) merged.set(item.id, item) })
        const next = [...merged.values()]
        const nextKeys = next.map(item => item && item.id).filter(Boolean)
        if (!this.assistantAlertInitialized) {
          this.assistantAlertSeen = new Set(nextKeys)
          this.assistantAlertInitialized = true
        }
        const newKeys = nextKeys.filter(key => !this.assistantAlertSeen.has(key))
        newKeys.forEach(key => this.assistantAlertSeen.add(key))
        const newCount = newKeys.length
        if (newCount > 0 && !this.assistantOpen) this.assistantUnread += newCount
        this.assistantAlerts = next
        this.assistantAlertsCheckedAt = Date.now()
      } catch (error) {
        if (requestID === this.assistantAlertsRequestID && !this.assistantContext) this.assistantError = error instanceof Error ? error.message : String(error)
      }
    },
    async sendAssistantMessage() {
      const question = String(this.assistantDraft || "").trim()
      if (!question || this.assistantBusy) return
      const symbol = String((this.assistantContext && this.assistantContext.symbol) || this.assistantSymbol || "").trim()
      if (!symbol) {
        this.assistantError = "当前没有可咨询的股票"
        return
      }
      this.assistantSending = true
      this.assistantError = ""
      this.assistantJobStatus = "queued"
      this.assistantJobProgress = "等待Agent启动"
      try {
        const response = await fetch("/api/assistant/chat", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ symbol, question }),
          cache: "no-store",
        })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (response.status === 409 && payload.job_id) {
          this.assistantJobID = payload.job_id
          this.assistantJobStatus = payload.status || "running"
          this.assistantJobProgress = payload.progress || "已有咨询任务正在处理"
          this.assistantDraft = ""
          this.pollAssistantJob()
          return
        }
        if (!response.ok) throw new Error(payload.error || "AI咨询任务提交失败")
        if (!payload.job_id) throw new Error("AI咨询任务缺少任务编号")
        this.assistantJobID = payload.job_id || ""
        this.assistantJobStatus = payload.status || "queued"
        this.assistantJobProgress = payload.progress || "等待Agent启动"
        this.assistantDraft = ""
        this.pollAssistantJob()
      } catch (error) {
        this.assistantError = error instanceof Error ? error.message : String(error)
        this.assistantSending = false
        this.assistantJobStatus = ""
      }
    },
    async pollAssistantJob() {
      if (!this.assistantJobID) return
      const jobID = this.assistantJobID
      try {
        const response = await fetch(`/api/assistant/chat?job_id=${encodeURIComponent(jobID)}`, { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "AI咨询状态读取失败")
        if (jobID !== this.assistantJobID) return
        this.assistantJobStatus = payload.status || ""
        this.assistantJobProgress = payload.progress || ""
        if (["queued", "running"].includes(payload.status)) {
          this.assistantJobTimer = window.setTimeout(() => this.pollAssistantJob(), 1200)
          return
        }
        this.assistantSending = false
        if (payload.status === "completed" && payload.response) {
          const result = payload.response
          this.assistantConversation = [...this.assistantConversation, {
            asked_at: result.asked_at,
            facts_at: result.facts_at,
            facts_hash: result.facts_hash,
            question: payload.question,
            answer: result.answer,
            fallback: result.fallback === true,
            agents: result.agents || [],
          }]
          if (this.assistantContext) {
            this.assistantContext.facts_at = result.facts_at || this.assistantContext.facts_at
            this.assistantContext.facts_hash = result.facts_hash || this.assistantContext.facts_hash
          }
          this.assistantError = result.save_warning ? `回答已完成，但历史保存失败：${result.save_warning}` : ""
          if (!this.assistantOpen) this.assistantUnread += 1
          this.scrollAssistantThread()
        } else if (payload.status === "failed" || payload.status === "canceled") {
          this.assistantError = payload.error || "AI咨询未完成"
        }
        this.assistantJobID = ""
        this.assistantJobStatus = ""
        this.assistantJobProgress = ""
      } catch (error) {
        if (jobID !== this.assistantJobID) return
        this.assistantSending = false
        this.assistantError = error instanceof Error ? error.message : String(error)
        this.assistantJobID = ""
        this.assistantJobStatus = ""
      }
    },
    async cancelAssistantJob() {
      if (!this.assistantJobID) return
      const jobID = this.assistantJobID
      try {
        await fetch(`/api/assistant/chat?job_id=${encodeURIComponent(jobID)}`, { method: "DELETE", cache: "no-store" })
      } finally {
        if (this.assistantJobTimer != null) window.clearTimeout(this.assistantJobTimer)
        this.assistantJobTimer = null
        this.assistantSending = false
        this.assistantJobID = ""
        this.assistantJobStatus = ""
        this.assistantJobProgress = ""
      }
    },
    selectAssistantAlert(alert) {
      if (!alert || !alert.symbol) return
      const symbol = alert.symbol
      this.query = this.displayCode(symbol)
      this.requestedSymbol = symbol
      this.workspaceMode = "market"
      this.load(symbol)
      this.loadAssistantContext(symbol)
      this.$nextTick(() => {
        const scroll = this.$refs.assistantScroll
        if (scroll) scroll.scrollTop = 0
      })
    },
    strategyModeLabel(mode) {
      if (mode === "trend-reclaim") return "趋势收复"
      if (mode === "ma-pullback") return "均线回踩"
	  if (mode === "momentum-continuation") return "动量延续"
	  if (mode === "mean-reversion") return "均值回归反弹"
	  if (mode === "volatility-squeeze") return "波动收缩突破"
	  if (mode === "adaptive-ensemble") return "自适应多形态"
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
    realtimeCount(state) { return this.realtimeVisibleSignals.filter(item => item.state === state).length },
    shadowValuationLabel(position) {
      if (!position) return "--"
      const quoteTime = position.valuation_time || position.last_date || "--"
      const source = position.realtime_valuation ? (position.valuation_source || "实时行情") : "日 K"
      return `${quoteTime} · ${source}`
    },
    shadowRejectionDetail(item) {
      if (!item) return ""
      if (String(item.reason || "").includes("同股票影子持仓")) {
        return `旧版单股单仓规则留下的历史记录；当前策略允许分批加仓与分批减仓。`
      }
      return item.reason || ""
    },
    shadowActionLabel(action) {
      return ({ open: "首次建仓", add: "加仓", rotate_in: "轮入", rotate_out: "轮出", reduce: "减仓", t_reduce: "做T高抛", t_rebuy: "做T回补", risk_exit: "风险退出", exit: "到期退出", hold: "持有", wait: "等待" })[action] || action || "--"
    },
    shadowLotSummary(position) {
      const lots = Array.isArray(position && position.lots) ? position.lots : []
      const sellable = lots.filter(lot => lot.entry_date && this.shadowReport && String(lot.entry_date) < String(this.shadowReport.as_of || "")).reduce((sum, lot) => sum + Number(lot.quantity || 0), 0)
      return `${lots.length || 1} 批 · 可卖 ${sellable || Number(position && position.available_quantity || 0)} 股`
    },
    shadowPositionWeight(position) {
      const equity = Number(this.shadowReport && this.shadowReport.total_equity)
      const marketValue = Number(position && position.market_value)
      if (!Number.isFinite(equity) || equity <= 0 || !Number.isFinite(marketValue)) return 0
      return marketValue / equity * 100
    },
    shadowPositionMeterWidth(position) {
      const weight = this.shadowPositionWeight(position)
      const limit = Number(this.shadowConfig.max_position_percent || 20)
      if (!Number.isFinite(limit) || limit <= 0) return "0%"
      return String(Math.max(0, Math.min(100, weight / limit * 100))) + "%"
    },
    shadowPositionStatus(position) {
      if (position && position.risk_exit_pending) return "待风险退出"
      if (Number(position && position.available_quantity) > 0) return "可卖"
      return "T+1 锁定"
    },
    shadowPositionStatusClass(position) {
      if (position && position.risk_exit_pending) return "risk"
      if (Number(position && position.available_quantity) > 0) return "ready"
      return "locked"
    },
    shadowPositionAvailability(position) {
      const quantity = Number(position && position.quantity)
      const available = Number(position && position.available_quantity)
      if (!Number.isFinite(quantity) || quantity <= 0) return "无有效数量"
      if (available > 0 && available < quantity) return `部分可卖 · ${quantity - available} 股锁定`
      if (available >= quantity) return "全部可卖"
      return "今日新开仓，下一交易日可卖"
    },
    viewShadowPositionMarket(position) {
      if (!position || !position.symbol) return
      this.workspaceMode = "market"
      this.query = this.displayCode(position.symbol)
      this.requestedSymbol = position.symbol
      this.load(position.symbol)
      window.scrollTo({ top: 0, behavior: "auto" })
    },
    showShadowOrderHistory(position) {
      if (!position || !position.symbol) return
      this.shadowOrderSymbol = position.symbol
      this.$nextTick(() => {
        document.getElementById("shadow-order-ledger")?.scrollIntoView({ behavior: "smooth", block: "start" })
      })
    },
    clearShadowOrderHistory() {
      this.shadowOrderSymbol = ""
    },
    shadowIndustryWidth(item) {
      const exposure = Number(item && item.exposure_percent)
      const limit = Number(item && item.limit_percent)
      if (!Number.isFinite(exposure) || !Number.isFinite(limit) || limit <= 0) return "0%"
      return `${Math.max(0, Math.min(100, exposure / limit * 100))}%`
    },
    shadowProfileName(id) {
      const item = this.shadowProfiles.find(profile => profile && profile.id === id)
      return item ? item.name : id || "影子账户"
    },
    shadowAccountSetLabel() {
      const count = this.shadowProfiles.filter(profile => profile && profile.id).length
      return count > 0 ? `${count}个影子账户` : "影子账户"
    },
    shadowSyncButtonText() {
      return this.shadowLoading ? `同步${this.shadowAccountSetLabel()}…` : `同步${this.shadowAccountSetLabel()}`
    },
    shadowSyncStatusText() {
      return `正在独立推进${this.shadowAccountSetLabel()}，当前账户详情保持可见`
    },
    shadowEmptyHint() {
      return `点击“${this.shadowSyncButtonText()}”后，会从同一份归档信号分别推进各账户。抓妖实验账户只用于前向观察。`
    },
    shadowProfileMetricClass(value) {
      return this.metricClass(value)
    },
    automationStatusLabel(status) {
      return ({ success: '已推进', waiting: '等待', paused: '已暂停', warning: '同步保护', error: '异常', running: '推进中', busy: '忙碌' })[status] || status || '等待'
    },
    selectShadowProfile(id) {
      if (!id || id === this.shadowProfileID || this.shadowLoading) return
      if (!this.shadowProfiles.some(item => item && item.id === id)) return
      const scrollY = window.scrollY
      this.shadowProfileID = id
      this.shadowOrderSymbol = ""
      // Do not leave the previous account's positions visible while the new
      // ledger is loading; account identity must always match its details.
      this.shadowReport = null
      this.shadowPreserveReason = ""
      this.loadShadowReport(id).then(() => {
        window.requestAnimationFrame(() => window.scrollTo({ top: scrollY, behavior: "auto" }))
      })
    },
    switchRealtimeSection(section) {
      if (!['candidates', 'shadow', 'validation'].includes(section)) return
      this.realtimeSection = section
      if (section === 'shadow') this.loadShadowReport()
      if (section === 'validation' && !this.realtimeOutcomeReport) this.loadRealtimeOutcomes()
      window.scrollTo({ top: 0, behavior: 'auto' })
    },
    switchWorkspace(mode) {
      if (!["dashboard", "market", "sentiment", "boards", "global", "strategy", "realtime", "settings"].includes(mode)) return
      this.workspaceMode = mode
      this.strategyCrosshair = null
      if (mode === "settings") {
        this.loadAIConfig()
        window.scrollTo({ top: 0, behavior: "auto" })
      } else if (mode === "strategy") {
        if (!this.strategyHistoryLoaded) this.loadStrategyHistory()
		if (!this.strategyCandidatesLoaded) this.loadStrategyCandidates(true)
        this.$nextTick(() => this.drawStrategyChart())
      } else if (mode === "realtime") {
        this.loadRealtimeSnapshot()
        this.loadRealtimeHistory()
        this.loadRealtimeOutcomes()
        this.loadShadowReport()
      } else if (mode === "global") {
        this.loadIndices()
        if (!this.globalSelectedSymbol && this.globalMarkets.length) this.globalSelectedSymbol = this.globalMarkets[0].symbol
        this.loadGlobalChart(this.globalSelectedSymbol, true)
        this.$nextTick(() => this.drawGlobalChart())
      } else if (mode === "sentiment") {
        this.loadSentiment(true)
      } else if (mode === "boards") {
        this.loadBoards(true)
      } else if (mode === "dashboard") {
        this.loadIndices()
        this.loadSentiment(true)
        this.loadBoards(true)
        this.loadMarketRankings(true)
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
        if (Array.isArray(payload.entry_modes) && payload.entry_modes.length) this.strategyEntryModes = payload.entry_modes
        this.strategyHistoryLoaded = true
      } catch (error) {
        this.strategyError = error instanceof Error ? error.message : String(error)
      } finally {
        this.strategyHistoryLoading = false
      }
    },
	async loadStrategyCandidates(autoRefresh = false) {
	  if (this.strategyCandidatesLoading) return
	  this.strategyCandidatesLoading = true
	  this.strategyCandidateError = ""
	  try {
		const response = await fetch("/api/strategy/candidates?limit=20", { cache: "no-store" })
		const body = await response.text()
		const payload = body ? JSON.parse(body) : {}
		if (!response.ok) throw new Error(payload.error || "策略候选读取失败")
		this.strategyCandidates = Array.isArray(payload.items) ? payload.items : []
		this.strategyCandidatesLoaded = true
		if (!this.strategyCandidates.some(item => item.experiment_id === this.strategyCandidateID)) {
		  this.strategyCandidateID = this.strategyCandidates[0]?.experiment_id || ""
		}
		if (autoRefresh && !this.strategyCandidateAutoRefreshing) {
		  const due = this.strategyCandidates.filter(item => item && item.refresh_due && ["observing", "approval-ready"].includes(item.lifecycle && item.lifecycle.status)).slice(0, 3)
		  if (due.length) {
			this.strategyCandidateAutoRefreshing = true
			for (const item of due) await this.updateStrategyCandidate(item, "refresh-observation", "", true)
			this.strategyCandidateAutoRefreshing = false
		  }
		}
	  } catch (error) {
		this.strategyCandidateError = error instanceof Error ? error.message : String(error)
	  } finally {
		this.strategyCandidatesLoading = false
	  }
	},
	selectStrategyCandidate(id) {
	  if (id) this.strategyCandidateID = id
	},
	async runStrategyCandidateAction(action) {
	  const item = this.strategyActiveCandidate
	  if (!item || this.strategyCandidateActionLoading) return
	  let note = ""
	  if (action === "start-observation" && !window.confirm("确认冻结该候选参数，并从下一个交易日开始真实时间观察？")) return
	  if (action === "approve-baseline") {
		note = String(window.prompt("请输入批准为下一轮研究基线的依据：", "前向观察门禁全部通过，批准进入下一轮 Champion/Challenger 研究。") || "").trim()
		if (!note) return
	  }
	  if (action === "reject-candidate" || action === "revoke-approval") {
		const title = action === "revoke-approval" ? "请输入撤销批准的原因：" : "请输入拒绝候选的原因："
		note = String(window.prompt(title, "") || "").trim()
		if (!note) return
	  }
	  await this.updateStrategyCandidate(item, action, note, false)
	},
	async updateStrategyCandidate(item, action, note = "", silent = false) {
	  if (!item || !item.experiment_id) return
	  this.strategyCandidateActionLoading = item.experiment_id
	  if (!silent) this.strategyCandidateError = ""
	  try {
		const response = await fetch(`/api/strategy/candidates?id=${encodeURIComponent(item.experiment_id)}`, {
		  method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action, note }),
		})
		const body = await response.text()
		const payload = body ? JSON.parse(body) : {}
		if (!response.ok) throw new Error(payload.error || "候选状态更新失败")
		const index = this.strategyCandidates.findIndex(candidate => candidate.experiment_id === payload.experiment_id)
		if (index >= 0) this.strategyCandidates.splice(index, 1, payload)
		else this.strategyCandidates.unshift(payload)
		this.strategyCandidateID = payload.experiment_id
	  } catch (error) {
		this.strategyCandidateError = error instanceof Error ? error.message : String(error)
	  } finally {
		this.strategyCandidateActionLoading = ""
	  }
	},
	candidateStatusLabel(status, researchStage = "") {
	  if (researchStage !== "shadow-ready") return "研究候选"
	  if (status === "awaiting-observation") return "待批准观察"
	  if (status === "observing") return "前向观察中"
	  if (status === "approval-ready") return "待人工批准"
	  if (status === "approved") return "已批准研究基线"
	  if (status === "rejected") return "已拒绝"
	  if (status === "revoked") return "批准已撤销"
	  return "模拟观察候选"
	},
	candidateStatusClass(status, researchStage = "") {
	  if (status === "approved") return "approved"
	  if (status === "approval-ready") return "ready"
	  if (status === "observing") return "observing"
	  if (status === "rejected" || status === "revoked" || researchStage !== "shadow-ready") return "blocked"
	  return "pending"
	},
	candidateStepClass(step) {
	  const candidate = this.strategyActiveCandidate
	  const status = this.strategyCandidateLifecycle.status
	  if (!candidate || candidate.research_stage !== "shadow-ready") return step === 0 ? "blocked" : "pending"
	  const ranks = { "awaiting-observation": 0, observing: 1, "approval-ready": 2, approved: 3 }
	  const rank = ranks[status]
	  if (status === "rejected" || status === "revoked") return step === 0 ? "done" : "blocked"
	  if (rank == null) return step === 0 ? "done" : "pending"
	  if (step < rank) return "done"
	  if (step === rank) return status === "approved" ? "done" : "active"
	  return "pending"
	},
	candidateCheckClass(check) { return check && check.passed ? "pass" : "pending" },
	candidateTickerText(item) {
	  const tickers = Array.isArray(item && item.tickers) ? item.tickers : []
	  return tickers.map(symbol => (item.names && item.names[symbol]) || this.displayCode(symbol)).join(" · ") || "--"
	},
	candidateParameterText(item) {
	  const p = item && item.parameters
	  if (!p) return "没有锁定参数"
	  return `${this.strategyModeLabel(p.entry_mode)} · MA${p.fast_ma}/${p.slow_ma} · 突破${p.breakout_days}日 · 量比≥${this.number(p.volume_ratio_min, 2)} · 止损${this.number(Number(p.stop_loss) * 100, 0)}% · 止盈${this.number(Number(p.take_profit) * 100, 0)}%`
	},
	candidateActionBusy(item) { return Boolean(item && this.strategyCandidateActionLoading === item.experiment_id) },
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
        if (Array.isArray(payload.entry_modes) && payload.entry_modes.length) this.strategyEntryModes = payload.entry_modes
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
    monsterStageClass(stage) {
      return ({ "潜伏": "monster-dormant", "启动": "monster-starting", "加速": "monster-accelerating", "高位分歧": "monster-diverging", "退潮": "monster-ebbing", "数据不足": "monster-insufficient" })[stage] || "monster-insufficient"
    },
    monsterConfidenceClass(confidence) {
      return confidence === "高" ? "monster-confidence-high" : confidence === "中" ? "monster-confidence-medium" : "monster-confidence-low"
    },
    monsterDistanceText(value, ready = true) {
      if (!ready || value == null || !Number.isFinite(Number(value))) return "--"
      return `${Number(value).toFixed(2)}%`
    },
    monsterGapText(radar) {
      if (!radar || !radar.opening_gap_ready || !Number.isFinite(Number(radar.opening_gap_percent))) return "--"
      const value = this.signedPercent(radar.opening_gap_percent)
      if (Number(radar.opening_gap_percent) >= 0.5) return `${value} · ${radar.opening_gap_held ? "守住" : "失守"}`
      if (Number(radar.opening_gap_percent) <= -0.5) return `${value} · ${radar.opening_gap_recovered ? "收复" : "未收复"}`
      return value
    },
    monsterOpeningRangeText(radar) {
      if (!radar || !radar.opening_range_ready) return "--"
      return radar.opening_range_breakout ? "上破" : "区间内"
    },
    monsterStructureText(radar) {
      if (!radar) return "--"
      if (radar.breakout_failure) return "突破失败"
      if (radar.volume_price_divergence) return "量价背离"
      return "正常"
    },
    monsterStructureClass(radar) {
      if (!radar) return ""
      return radar.breakout_failure || radar.volume_price_divergence ? "down" : "up"
    },
    monsterSourcesText(signal) {
      const sources = signal && Array.isArray(signal.candidate_sources) ? signal.candidate_sources.filter(Boolean) : []
      return sources.length ? sources.join(" · ") : "常规扫描"
    },
    hasSignalOverlay(signal) {
      return Boolean(signal && (Number(signal.risk_multiplier) > 0 || Number(signal.cross_section_total) > 0 || Number(signal.tradable_total) > 0 || signal.market_regime))
    },
    signalPortfolioLabel(signal) {
      if (!this.hasSignalOverlay(signal)) return "历史信号"
      return signal.portfolio_eligible ? "组合候选" : "风险降级"
    },
    signalPortfolioClass(signal) {
      if (!this.hasSignalOverlay(signal)) return "portfolio-legacy"
      return signal.portfolio_eligible ? "portfolio-eligible" : "portfolio-degraded"
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
    calibrationStatusClass(status) {
      if (status === "候选领先") return "pass"
      if (status === "候选落后") return "fail required"
      if (status === "观察中") return "pending"
      return ""
    },
    calibrationMetricSummary(metric) {
      if (!metric || !Number(metric.samples)) return "暂无可交易样本"
      return `${metric.samples} 个入选 · 命中 ${this.percentText(metric.hit_rate_percent)} · 超额 ${this.percentText(metric.average_excess_percent)}`
    },
    tuningPriorityLabel(priority) {
      return ({ high: "优先", medium: "观察", low: "常规" })[priority] || "观察"
    },
    tuningPriorityClass(priority) {
      return priority === "high" ? "high" : priority === "low" ? "low" : "medium"
    },
    monsterMetricStatusClass(status) {
      if (status === "正向") return "up"
      if (status === "偏弱") return "down"
      if (status === "样本不足" || status === "可观察但可执行样本不足") return "warn-text"
      return ""
    },
    monsterMetricReturn(item) {
      if (!item || !Number(item.eligible_ready)) return "--"
      return `${this.percentText(item.eligible_average_excess_percent)} · ${this.percentText(item.eligible_hit_rate_percent)}`
    },
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
    async triggerAutomation() {
      if (this.automationTriggerLoading) return
      this.automationTriggerLoading = true
      this.realtimeError = ""
      try {
        const response = await fetch("/api/strategy/automation?action=run", { method: "POST", cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok && response.status !== 202) throw new Error(payload.error || "自动任务触发失败")
        this.realtimeAutomation = payload
        // The endpoint is asynchronous. Poll the status a few times so the
        // button feedback reflects completion even when the scan takes longer
        // than one browser refresh interval.
        for (let attempt = 0; attempt < 8; attempt += 1) {
          await new Promise(resolve => window.setTimeout(resolve, attempt === 0 ? 400 : 1000))
          await this.loadRealtimeSnapshot()
          if (!this.realtimeAutomation || !this.realtimeAutomation.running) break
        }
      } catch (error) {
        this.realtimeError = error instanceof Error ? error.message : String(error)
      } finally {
        this.automationTriggerLoading = false
      }
    },
    applyRealtimeSession(payload) {
      if (!payload || typeof payload !== "object") return
      this.realtimeMarketState = payload.market_state || (payload.result && payload.result.market_state) || this.realtimeMarketState
      if (typeof payload.trading_day === "boolean") this.realtimeTradingDay = payload.trading_day
      if (typeof payload.calendar_known === "boolean") this.realtimeCalendarKnown = payload.calendar_known
      if (typeof payload.scan_allowed === "boolean") this.realtimeScanAllowed = payload.scan_allowed
      if (typeof payload.frozen === "boolean") this.realtimeFrozen = payload.frozen
      this.realtimeNextScanAt = payload.next_scan_at || ""
      if (payload.automation && typeof payload.automation === "object") this.realtimeAutomation = payload.automation
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
        const response = await fetch("/api/strategy/realtime?view=outcomes&full=1", { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "信号验证结果读取失败")
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
        const response = await fetch("/api/strategy/realtime?view=outcomes&full=1&horizons=1,3,5,10", { method: "POST", cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "信号验证结果更新失败")
        this.realtimeOutcomeReport = payload.report || null
      } catch (error) {
        this.realtimeOutcomeError = error instanceof Error ? error.message : String(error)
      } finally {
        this.realtimeOutcomeLoading = false
        await this.$nextTick()
        window.scrollTo({ top: scrollY, behavior: 'auto' })
      }
    },
    async loadShadowReport(profileID = this.shadowProfileID) {
      if (this.shadowLoading) return
	  const requestID = ++this.shadowRequestID
      this.shadowCheckedAt = Date.now()
      this.shadowError = ""
      this.shadowPreserveReason = ""
      try {
        const response = await fetch(`/api/strategy/shadow?profile=${encodeURIComponent(profileID)}`, { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "影子执行结果读取失败")
		if (requestID !== this.shadowRequestID || profileID !== this.shadowProfileID) return
        if (Array.isArray(payload.profiles)) this.shadowProfiles = payload.profiles
		this.shadowReport = payload.report || null
		this.shadowPreserveReason = payload.preserve_reason || ""
      } catch (error) {
		if (requestID === this.shadowRequestID) this.shadowError = error instanceof Error ? error.message : String(error)
      }
    },
    async refreshShadowReport() {
      if (this.shadowLoading) return
      const scrollY = window.scrollY
      this.shadowCheckedAt = Date.now()
      this.shadowLoading = true
	  const requestID = ++this.shadowRequestID
      this.shadowError = ""
	  this.shadowPreserveReason = ""
      try {
        const response = await fetch(`/api/strategy/shadow?profile=all&selected=${encodeURIComponent(this.shadowProfileID)}`, { method: "POST", cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "影子执行更新失败")
		if (requestID === this.shadowRequestID) {
		  if (Array.isArray(payload.profiles)) this.shadowProfiles = payload.profiles
		  this.shadowReport = payload.report || null
		  this.shadowPreserveReason = payload.preserve_reason || ""
		}
      } catch (error) {
		if (requestID === this.shadowRequestID) this.shadowError = error instanceof Error ? error.message : String(error)
      } finally {
        this.shadowLoading = false
        await this.$nextTick()
        window.scrollTo({ top: scrollY, behavior: 'auto' })
      }
    },
    setRealtimeScope(scope) {
      if (scope !== "leaders" && scope !== "watchlist") return
      if (this.realtimeScope === scope) return
      this.realtimeScope = scope

      // A broad snapshot already contains the watchlist rows, so narrowing to
      // "仅自选" can update immediately without another network scan. If the
      // current snapshot was produced in watchlist-only mode, request a fresh
      // broad scan when the market is open so the reverse switch is useful too.
      if (scope === "leaders" && this.realtimeResult && this.realtimeResult.universe !== "watchlist+leaders" && this.realtimeScanAllowed) {
        this.runRealtimeScan()
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
      if (!this.realtimeSnapshotLoading && now - this.realtimeSessionCheckedAt >= 10000) {
        this.loadRealtimeSnapshot()
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
    globalMarketValue(item) {
      if (!item || !Number.isFinite(Number(item.current))) return "--"
      return Number(item.current).toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 })
    },
    globalMarketChange(item) {
      if (!item || item.percent == null || !Number.isFinite(Number(item.percent))) return "--"
      const percent = Number(item.percent)
      const delta = Number(item.delta)
      const text = `${percent > 0 ? "+" : ""}${percent.toFixed(2)}%`
      if (!Number.isFinite(delta)) return text
      return `${text}  ${delta > 0 ? "+" : ""}${delta.toFixed(2)}`
    },
    globalMarketChangeClass(item) {
      return this.watchlistChangeClass(item && item.percent)
    },
    globalMarketTimeText(item) {
      if (!item) return "暂无行情时间"
      const raw = String(item.quote_time || "").trim()
      return raw || "暂无行情时间"
    },
    globalMarketTimeTitle(item) {
      if (!item) return ""
      const parts = []
      if (item.quote_time) parts.push(`指数时间：${item.quote_time}`)
      if (item.source) parts.push(`来源：${item.source}`)
      if (item.extended && item.extended.quote_time) parts.push(`${item.extended.session || "延长"}：${item.extended.quote_time}`)
      return parts.join(" · ")
    },
    globalMarketExtendedText(item) {
      const extended = item && item.extended
      if (!extended || !Number.isFinite(Number(extended.price))) return ""
      const change = Number.isFinite(Number(extended.percent))
        ? `${Number(extended.percent) > 0 ? "+" : ""}${Number(extended.percent).toFixed(2)}%`
        : ""
      return `${extended.session || "延长"} ${extended.symbol || "ETF"} ${this.globalMarketValue({ current: extended.price })}${change ? ` ${change}` : ""}`
    },
    globalNumber(value, digits = 2) {
      return Number.isFinite(Number(value)) ? Number(value).toFixed(digits) : "--"
    },
    globalCompact(value) {
      const number = Number(value)
      if (!Number.isFinite(number)) return "--"
      const absolute = Math.abs(number)
      if (absolute >= 1e8) return (number / 1e8).toFixed(2) + "亿"
      if (absolute >= 1e4) return (number / 1e4).toFixed(2) + "万"
      return number.toFixed(0)
    },
    globalMarketTimezone(item) {
      const symbol = String(item && item.symbol || "").toLowerCase()
      if (symbol === "rt_hkhsi" || symbol === "rt_hkhstech") return "港"
      if (symbol === "b_nky") return "东京"
      if (symbol === "b_kospi" || symbol === "b_kosdaq") return "首尔"
      return "纽约"
    },
    globalQuoteTimestamp(item) {
      if (!item || !item.quote_time) return "暂无报价时间"
      return String(item.quote_time) + " · " + this.globalMarketTimezone(item) + "时区"
    },
    globalDailyWindow() {
      const total = this.globalBars.length
      if (!total) return []
      const count = Math.max(1, Math.min(total, Math.round(this.globalDailyVisibleCount)))
      return this.globalBars.slice(Math.max(0, total - count))
    },
    globalMovingAverage(bars, index, length) {
      if (index + 1 < length) return null
      let sum = 0
      for (let cursor = index - length + 1; cursor <= index; cursor += 1) {
        const close = Number(bars[cursor] && bars[cursor].close)
        if (!Number.isFinite(close)) return null
        sum += close
      }
      return sum / length
    },
    setGlobalChartMode(mode) {
      if (mode !== "intraday" && mode !== "daily") return
      this.globalChartMode = mode
      this.globalCrosshair = null
      this.$nextTick(() => this.drawGlobalChart())
    },
    setGlobalDailyRange(option) {
      if (!option || !Number.isFinite(Number(option.count))) return
      this.globalDailyVisibleCount = Number(option.count)
      this.globalDailyRangePreset = option.key
      this.globalCrosshair = null
      this.$nextTick(() => this.drawGlobalChart())
    },
    selectGlobalMarket(item) {
      if (!item || !item.symbol) return
      const changed = this.globalSelectedSymbol !== item.symbol
      this.globalSelectedSymbol = item.symbol
      if (this.workspaceMode !== "global") this.workspaceMode = "global"
      if (changed) {
        this.globalChartData = {}
        this.globalChartError = ""
        this.globalCrosshair = null
      }
      this.loadGlobalChart(item.symbol, true)
      window.scrollTo({ top: 0, behavior: "auto" })
    },
    async loadGlobalChart(symbol = this.globalSelectedSymbol, force = false) {
      if (!symbol) return
      const now = Date.now()
      if (!force && (this.globalChartLoading || (this.globalSelectedSymbol === symbol && this.globalChartLastRequestedAt > 0 && now - this.globalChartLastRequestedAt < 25000))) return
      const requestID = ++this.globalChartRequestID
      this.globalSelectedSymbol = symbol
      this.globalChartLastRequestedAt = now
      this.globalChartLoading = true
      this.globalChartError = ""
      try {
        const response = await fetch("/api/global/chart?symbol=" + encodeURIComponent(symbol) + "&mode=all&limit=300", { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || payload.warning || "外盘图表暂不可用")
        if (requestID !== this.globalChartRequestID) return
        this.globalChartData = payload
        this.globalChartFetchedAt = payload.fetched_at || new Date().toISOString()
        const received = payload.market
        if (received && received.symbol) {
          this.globalMarkets = globalMarketDefinitions.map(definition => definition.symbol === received.symbol
            ? { ...definition, ...received }
            : (this.globalMarkets.find(item => item && item.symbol === definition.symbol) || { ...definition }))
        }
        const warnings = [payload.warning, payload.daily_error, payload.minute_error].filter(Boolean)
        this.globalChartError = warnings.join("；")
        this.globalChartStatus = this.globalChartError ? "部分数据可用" : "已更新"
        await this.$nextTick()
        this.drawGlobalChart()
      } catch (error) {
        if (requestID === this.globalChartRequestID) {
          this.globalChartError = error instanceof Error ? error.message : String(error)
          this.globalChartStatus = "数据暂不可用"
        }
      } finally {
        if (requestID === this.globalChartRequestID) this.globalChartLoading = false
      }
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
      const normalized = String(symbol || "").toLowerCase()
      return marketIndexDefinitions.some(item => item.symbol === normalized) || /^sh000\d{3}$/.test(normalized) || /^sz399\d{3}$/.test(normalized)
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
        if (Array.isArray(payload.global_markets)) {
          const globalReceived = new Map(payload.global_markets.map(item => [item.symbol, item]))
          this.globalMarkets = globalMarketDefinitions.map(definition => ({ ...definition, ...(globalReceived.get(definition.symbol) || {}) }))
        }
        this.globalMarketsFetchedAt = payload.global_fetched_at || this.globalMarketsFetchedAt
        this.globalMarketsError = payload.global_warning || ""
        this.marketAmount = payload.market_amount || this.marketAmount
        const warnings = [payload.warning, payload.amount_warning].filter(Boolean)
        this.indicesError = warnings.join("；")
        if (!response.ok) throw new Error(payload.warning || payload.error || "指数行情暂不可用")
      } catch (error) {
        if (requestID === this.indicesRequestID) {
          const message = error instanceof Error ? error.message : String(error)
          this.indicesError = message
          if (!this.globalMarkets.length) this.globalMarketsError = message
        }
      } finally {
        if (requestID === this.indicesRequestID) this.indicesLoading = false
      }
    },
    sentimentValue(value) {
      const number = Number(value)
      return Number.isFinite(number) ? number.toFixed(1) : "--"
    },
    sentimentNumber(value) {
      return this.sentimentValue(value)
    },
    sentimentPercent(value) {
      const number = Number(value)
      return Number.isFinite(number) ? `${number >= 0 ? "+" : ""}${number.toFixed(1)}%` : "--"
    },
    sentimentBarWidth(value) {
      const number = Number(value)
      return Number.isFinite(number) ? `${Math.max(0, Math.min(100, number))}%` : "0%"
    },
    sentimentTrendDelta(key) {
      const current = this.sentimentSnapshot ? Number(this.sentimentSnapshot[key]) : NaN
      const points = this.sentimentHistory || []
      if (!Number.isFinite(current) || points.length < 2) return "暂无前值"
      const latestAt = Date.parse(points[points.length - 1].at || "")
      const previous = [...points].reverse().find(item => Number.isFinite(Number(item[key])) && Date.parse(item.at || "") < latestAt)
      if (!previous) return "暂无前值"
      const delta = current - Number(previous[key])
      return `${delta >= 0 ? "较前次 +" : "较前次 "}${delta.toFixed(1)}`
    },
    async loadSentiment(force = false) {
      const now = Date.now()
      if (!force && (this.sentimentLoading || now - this.sentimentFetchedAt < 8000)) return
      const requestID = ++this.sentimentRequestID
      this.sentimentLoading = true
      this.sentimentError = ""
      try {
        const response = await fetch("/api/sentiment", { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.error || "市场情绪暂不可用")
        if (requestID !== this.sentimentRequestID) return
        this.sentimentSnapshot = payload.snapshot || null
        this.sentimentHistory = Array.isArray(payload.history) ? payload.history : []
        this.sentimentFetchedAt = now
        await this.$nextTick()
        this.drawSentimentChart()
      } catch (error) {
        if (requestID === this.sentimentRequestID) this.sentimentError = error instanceof Error ? error.message : String(error)
      } finally {
        if (requestID === this.sentimentRequestID) this.sentimentLoading = false
      }
    },
    async loadBoards(force = false) {
      const now = Date.now()
      if (!force && (this.boardsLoading || now - this.boardsFetchedAt < 20000)) return
      this.boardsLoading = true
      this.boardsError = ""
      try {
        const response = await fetch(`/api/boards?sort=${encodeURIComponent(this.boardSort)}`, { cache: "no-store" })
        const body = await response.text()
        const payload = body ? JSON.parse(body) : {}
        if (!response.ok) throw new Error(payload.warning || payload.error || "板块行情暂不可用")
        this.boardItems = Array.isArray(payload.items) ? payload.items : []
        this.boardsFetchedAt = now
        if (this.boardSort === "flow") { this.boardSortField = "flow"; this.boardSortDirection = "desc" }
        else if (this.boardSort === "weak") { this.boardSortField = "percent"; this.boardSortDirection = "asc" }
        else if (this.boardSort === "hot") { this.boardSortField = "percent"; this.boardSortDirection = "desc" }
        if (!this.boardSelectedCode && this.boardItems.length) this.boardSelectedCode = this.boardItems[0].code
        const selected = this.boardItems.find(item => item.code === this.boardSelectedCode)
        if (selected) {
          if (this.boardSamplingActive()) {
            const today = localDate(new Date())
            this.boardHistory = [...this.boardHistory.filter(point => localDate(new Date(point.at)) === today), { at: Date.now(), code: selected.code, percent: Number(selected.percent), mainNet: Number(selected.main_net_yuan), breadth: this.boardBreadth(selected) }].slice(-240)
          }
        }
        await this.$nextTick()
        this.drawBoardChart()
        if (selected) this.loadBoardMembers(selected.code)
      } catch (error) {
        this.boardsError = error instanceof Error ? error.message : String(error)
      } finally {
        this.boardsLoading = false
      }
    },
    async loadMarketRankings(force = false) {
      const now = Date.now()
      if (!force && (this.marketRankingsLoading || now - this.marketRankingsFetchedAt < 20000)) return
      this.marketRankingsLoading = true
      this.marketRankingsError = ""
      try {
      const kinds = ["gainers", "losers", "rapid_rise", "amount", "turnover"]
        const responses = await Promise.all(kinds.map(kind => fetch(`/api/rankings?kind=${kind}&limit=8`, { cache: "no-store" })))
        const payloads = await Promise.all(responses.map(async response => ({ response, payload: await response.json() })))
        const next = {}
        payloads.forEach(({ response, payload }, index) => {
          const kind = kinds[index]
          if (!response.ok) throw new Error(payload.warning || payload.error || `${kind} 榜单暂不可用`)
          next[kind] = Array.isArray(payload.items) ? payload.items : []
        })
        this.marketRankings = { ...this.marketRankings, ...next }
        this.marketRankingsFetchedAt = now
      } catch (error) {
        this.marketRankingsError = error instanceof Error ? error.message : String(error)
      } finally {
        this.marketRankingsLoading = false
      }
    },
    marketRankingMetric(item, metric) {
      if (metric === "amount") return this.compact(item && item.amount_yuan)
      if (metric === "turnover") return this.percentText(item && item.turnover_percent)
      return this.percentText(item && item.percent)
    },
    boardBreadth(item) {
      const rise = Number(item && item.rise_count) || 0
      const fall = Number(item && item.fall_count) || 0
      const flat = Number(item && item.flat_count) || 0
      const total = rise + fall + flat
      return total > 0 ? Math.max(0, Math.min(100, 50 + (rise - fall) / total * 50)) : NaN
    },
    selectBoard(item) {
      if (!item || !item.code) return
      this.boardSelectedCode = item.code
      this.boardHistory = []
      this.boardMembers = []
      this.loadBoardMembers(item.code)
      this.$nextTick(() => this.drawBoardChart())
    },
    setBoardSort(field) {
      if (!field) return
      if (this.boardSortField === field) this.boardSortDirection = this.boardSortDirection === "desc" ? "asc" : "desc"
      else { this.boardSortField = field; this.boardSortDirection = field === "name" ? "asc" : "desc" }
      this.boardSort = field === "flow" ? "flow" : (field === "percent" && this.boardSortDirection === "asc" ? "weak" : "hot")
    },
    boardSortIndicator(field) {
      if (this.boardSortField !== field) return ""
      return this.boardSortDirection === "asc" ? " ↑" : " ↓"
    },
    newsTone(title) {
      const text = String(title || "")
      if (/(增持|回购|中标|签署|获批|涨价|突破|净利增|预增|分红|利好|订单|投资者关系)/.test(text)) return "up"
      if (/(减持|亏损|下滑|预减|处罚|违规|诉讼|立案|风险|利空|跌停|解禁|质押)/.test(text)) return "down"
      return "muted"
    },
    newsToneText(title) {
      const tone = this.newsTone(title)
      return tone === "up" ? "利好线索" : tone === "down" ? "利空线索" : "中性线索"
    },
    boardSamplingActive() {
      const now = new Date()
      const day = now.getDay()
      if (day === 0 || day === 6) return false
      const minutes = now.getHours() * 60 + now.getMinutes()
      return (minutes >= 570 && minutes <= 690) || (minutes >= 780 && minutes <= 900)
    },
    async loadBoardMembers(code) {
      if (!code || this.boardMembersLoading) return
      this.boardMembersLoading = true
      this.boardMembersError = ""
      try {
        const response = await fetch(`/api/stock?symbol=${encodeURIComponent(code)}&limit=300`, { cache: "no-store" })
        const payload = await response.json()
        if (!response.ok) throw new Error(payload.board_error || payload.error || "成分股暂不可用")
        if (code === this.boardSelectedCode) this.boardMembers = payload.board && Array.isArray(payload.board.leaders) ? payload.board.leaders : []
      } catch (error) {
        if (code === this.boardSelectedCode) this.boardMembersError = error instanceof Error ? error.message : String(error)
      } finally {
        this.boardMembersLoading = false
      }
    },
    selectBoardMember(member) {
      if (!member || !member.symbol) return
      this.workspaceMode = "market"
      this.query = this.displayCode(member.symbol)
      this.requestedSymbol = member.symbol
      this.load(member.symbol)
      window.scrollTo({ top: 0, behavior: "auto" })
    },
    openBoardMarket(item) {
      if (!item || !item.code) return
      this.workspaceMode = "market"
      this.query = item.code
      this.requestedSymbol = item.code
      this.load(item.code)
      window.scrollTo({ top: 0, behavior: "auto" })
    },
    prepareBoardCanvas() {
      const canvas = this.$refs.boardChart
      if (!canvas) return null
      const rect = canvas.getBoundingClientRect(); const dpr = window.devicePixelRatio || 1
      canvas.width = Math.max(1, Math.floor(rect.width * dpr)); canvas.height = Math.max(1, Math.floor(rect.height * dpr))
      const context = canvas.getContext("2d"); context.setTransform(dpr, 0, 0, dpr, 0, 0); context.fillStyle = "#171d21"; context.fillRect(0, 0, rect.width, rect.height)
      return { context, width: rect.width, height: rect.height }
    },
    drawBoardChart() {
      const canvas = this.prepareBoardCanvas(); if (!canvas) return
      const { context, width, height } = canvas
      const points = this.boardHistory.filter(item => item.code === this.boardSelectedCode && Number.isFinite(item.percent))
      if (points.length < 1) { context.fillStyle = "#91a0a7"; context.font = "12px sans-serif"; context.fillText("暂无板块分时数据", 18, 28); return }
      const left = 42, right = 14, top = 18, bottom = 30, plotWidth = width - left - right, plotHeight = height - top - bottom
      const values = points.map(item => Number(item.percent)); const min = Math.min(...values, 0); const max = Math.max(...values, 0); const pad = Math.max(.2, (max - min) * .15); const low = min - pad, high = max + pad
      const xOf = index => left + index / Math.max(1, points.length - 1) * plotWidth; const yOf = value => top + (high - value) / Math.max(.001, high - low) * plotHeight
      context.strokeStyle = "#2b363c"; context.lineWidth = 1; for (let index = 0; index < 5; index++) { const y = top + index * plotHeight / 4; context.beginPath(); context.moveTo(left, y); context.lineTo(width - right, y); context.stroke(); context.fillStyle = "#718188"; context.font = "10px sans-serif"; context.fillText((high - index * (high - low) / 4).toFixed(2) + "%", 4, y + 3) }
      const zeroY = yOf(0); context.setLineDash([5, 4]); context.strokeStyle = "#87969b"; context.beginPath(); context.moveTo(left, zeroY); context.lineTo(width - right, zeroY); context.stroke(); context.setLineDash([])
      if (points.length > 1) { context.beginPath(); points.forEach((point, index) => { const x = xOf(index), y = yOf(Number(point.percent)); index ? context.lineTo(x, y) : context.moveTo(x, y) }); context.lineTo(xOf(points.length - 1), zeroY); context.lineTo(left, zeroY); context.closePath(); context.fillStyle = "rgba(88,185,215,.10)"; context.fill(); context.beginPath(); points.forEach((point, index) => { const x = xOf(index), y = yOf(Number(point.percent)); index ? context.lineTo(x, y) : context.moveTo(x, y) }); context.strokeStyle = values[values.length - 1] >= 0 ? "#ef6b6b" : "#48c5a0"; context.lineWidth = 2; context.stroke() }
      const currentX = xOf(points.length - 1), currentY = yOf(values[values.length - 1]); context.fillStyle = values[values.length - 1] >= 0 ? "#ef6b6b" : "#48c5a0"; context.beginPath(); context.arc(currentX, currentY, 4, 0, Math.PI * 2); context.fill()
      context.fillStyle = "#718188"; context.font = "10px sans-serif"; context.textAlign = "center"; ["09:30", "10:30", "11:30", "13:00", "14:00", "15:00"].forEach((label, index, labels) => context.fillText(label, left + index / (labels.length - 1) * plotWidth, height - 9)); context.textAlign = "left"
    },
    prepareSentimentCanvas() {
      const canvas = this.$refs.sentimentChart
      if (!canvas) return null
      const rect = canvas.getBoundingClientRect()
      const dpr = window.devicePixelRatio || 1
      canvas.width = Math.max(1, Math.floor(rect.width * dpr)); canvas.height = Math.max(1, Math.floor(rect.height * dpr))
      const context = canvas.getContext("2d")
      context.setTransform(dpr, 0, 0, dpr, 0, 0)
      context.fillStyle = "#171d21"; context.fillRect(0, 0, rect.width, rect.height)
      return { context, width: rect.width, height: rect.height }
    },
    drawSentimentChart() {
      const canvas = this.prepareSentimentCanvas()
      if (!canvas) return
      const { context, width, height } = canvas
      const fields = [
        { key: "score", color: "#f0b768", width: 2.5 },
        { key: "index_signal", color: "#58b9d7", width: 1.5 },
        { key: "industry_breadth", color: "#77c99a", width: 1.5 },
        { key: "turnover_signal", color: "#c58bd8", width: 1.5 },
        { key: "northbound_signal", color: "#d68e6f", width: 1.35 },
      ]
      let points = this.sentimentHistory.filter(item => fields.some(field => Number.isFinite(Number(item[field.key]))))
      if (points.length > 1) {
        const latestDate = new Date(points[points.length - 1].at || "").toLocaleDateString("zh-CN")
        const sameDay = points.filter(item => new Date(item.at || "").toLocaleDateString("zh-CN") === latestDate)
        if (sameDay.length > 1) points = sameDay
      }
      if (points.length < 1) {
        context.fillStyle = "#91a0a7"; context.font = '12px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'; context.fillText("暂无情绪数据", 18, 28); return
      }
      const left = 42, right = 16, top = 18, bottom = 34, plotWidth = width - left - right, plotHeight = height - top - bottom
      context.strokeStyle = "#2b363c"; context.lineWidth = 1
      for (const [low, high, color] of [[0, 20, "rgba(72,197,160,.06)"], [20, 40, "rgba(72,197,160,.025)"], [60, 80, "rgba(239,107,107,.025)"], [80, 100, "rgba(239,107,107,.06)"]]) { const y = top + plotHeight * (1 - high / 100); const bandHeight = plotHeight * (high - low) / 100; context.fillStyle = color; context.fillRect(left, y, plotWidth, bandHeight) }
      for (const value of [0, 20, 40, 60, 80, 100]) { const y = top + plotHeight * (1 - value / 100); context.beginPath(); context.moveTo(left, y); context.lineTo(width - right, y); context.stroke(); context.fillStyle = "#718188"; context.font = "10px sans-serif"; context.fillText(String(value), 8, y + 3) }
      const neutralY = top + plotHeight * .5; context.setLineDash([5, 4]); context.strokeStyle = "#87969b"; context.beginPath(); context.moveTo(left, neutralY); context.lineTo(width - right, neutralY); context.stroke(); context.setLineDash([]); context.fillStyle = "#aab7ba"; context.fillText("中性", width - right - 28, neutralY - 5)
      const scorePoints = points.filter(point => Number.isFinite(Number(point.score)))
      if (scorePoints.length > 1) { context.beginPath(); scorePoints.forEach((point, index) => { const x = left + points.indexOf(point) / (points.length - 1) * plotWidth; const y = top + plotHeight * (1 - Number(point.score) / 100); index === 0 ? context.moveTo(x, y) : context.lineTo(x, y) }); context.lineTo(left + points.indexOf(scorePoints[scorePoints.length - 1]) / (points.length - 1) * plotWidth, top + plotHeight); context.lineTo(left, top + plotHeight); context.closePath(); context.fillStyle = "rgba(240,183,104,.10)"; context.fill() }
      fields.forEach(field => {
        const available = points.filter(point => Number.isFinite(Number(point[field.key])))
        if (available.length < 2) return
        context.strokeStyle = field.color; context.lineWidth = field.width; context.beginPath()
        available.forEach((point, index) => { const x = left + (points.length === 1 ? 0 : points.indexOf(point) / (points.length - 1)) * plotWidth; const y = top + plotHeight * (1 - Number(point[field.key]) / 100); if (index === 0) context.moveTo(x, y); else context.lineTo(x, y) })
        context.stroke()
      })
      const last = points[points.length - 1]
      fields.forEach(field => {
        const value = Number(last[field.key]); if (!Number.isFinite(value)) return
        const x = width - right; const y = top + plotHeight * (1 - value / 100)
        context.fillStyle = field.color; context.beginPath(); context.arc(x, y, field.key === "score" ? 4 : 2.5, 0, Math.PI * 2); context.fill()
      })
      if (Number.isFinite(Number(last.score))) { const y = top + plotHeight * (1 - Number(last.score) / 100); context.fillStyle = "#edf3f5"; context.font = "11px sans-serif"; context.fillText(`${Number(last.score).toFixed(1)} · ${last.phase || ""}`, Math.max(left, width - right - 125), Math.max(13, y - 9)) }
      context.fillStyle = "#718188"; context.font = "10px sans-serif"; context.textAlign = "center"; const labels = ["09:30", "10:30", "11:30", "13:00", "14:00", "15:00"]; labels.forEach((label, index) => { const x = left + index / (labels.length - 1) * plotWidth; context.fillText(label, x, height - 10) }); context.textAlign = "left"
    },
    selectMarketIndex(item) {
      if (!item || !item.symbol) return
      this.workspaceMode = "market"
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
      this.workspaceMode = "market"
      this.query = this.displayCode(symbol)
      this.requestedSymbol = symbol
      this.load(symbol)
      window.scrollTo({ top: 0, behavior: "auto" })
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
          if (this.assistantOpen) {
            this.assistantContext = null
            this.assistantConversation = []
            this.assistantError = ""
          }
        }
        if (payload.symbol) this.query = this.isMarketIndex(payload.symbol) || String(payload.symbol).toLowerCase().startsWith("bk") ? payload.symbol : String(payload.symbol).slice(2)
        if (payload.symbol) {
          this.loadAssistantAlerts()
          if (this.assistantOpen) this.loadAssistantContext(payload.symbol)
        }
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
      if (this.workspaceMode === "sentiment") this.drawSentimentChart()
      else if (this.workspaceMode === "boards") this.drawBoardChart()
      else if (this.workspaceMode === "strategy") this.drawStrategyChart()
      else if (this.workspaceMode === "global") this.drawGlobalChart()
      else this.drawChart()
    },
    prepareGlobalCanvas() {
      const canvas = this.$refs.globalChart
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
    drawGlobalChart() {
      const canvas = this.prepareGlobalCanvas()
      if (!canvas) return
      this.globalChartGeometry = null
      if (this.globalChartMode === "intraday") this.drawGlobalIntradayChart(canvas.context, canvas.width, canvas.height)
      else this.drawGlobalDailyChart(canvas.context, canvas.width, canvas.height)
      this.drawGlobalCrosshair(canvas.context, canvas.width, canvas.height)
    },
    drawGlobalEmpty(context, message) {
      context.fillStyle = "#91a0a7"
      context.font = '12px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      context.fillText(message, 18, 28)
    },
    drawGlobalIntradayChart(context, width, height) {
      const raw = this.globalMinutes.filter(point => Number.isFinite(Number(point.price)) && Number(point.price) > 0)
      if (!raw.length) { this.drawGlobalEmpty(context, "暂无外盘分时数据"); return }
      const left = width < 560 ? 58 : 70
      const right = width < 560 ? 18 : 28
      const top = 18
      const bottom = 52
      const volumeHeight = 72
      const plotBottom = height - bottom - volumeHeight
      const plotHeight = Math.max(24, plotBottom - top)
      const plotWidth = Math.max(20, width - left - right)
      let previousClose = Number((this.globalSelectedMarket || {}).previous_close)
      const values = raw.flatMap(point => [Number(point.price), Number(point.average)]).filter(value => Number.isFinite(value) && value > 0)
      const seriesMinimum = Math.min(...values)
      const seriesMaximum = Math.max(...values)
      if (Number.isFinite(previousClose) && previousClose > 0 && previousClose >= seriesMinimum * 0.5 && previousClose <= seriesMaximum * 1.5) values.push(previousClose)
      else previousClose = NaN
      let minimum = Math.min(...values)
      let maximum = Math.max(...values)
      const padding = (maximum - minimum) * 0.08 || Math.max(0.1, maximum * 0.002)
      minimum -= padding
      maximum += padding
      const x = index => left + (raw.length === 1 ? plotWidth / 2 : index / (raw.length - 1) * plotWidth)
      const y = value => top + (maximum - value) / Math.max(0.000001, maximum - minimum) * plotHeight
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
      const maxVolume = Math.max(...raw.map(point => Number(point.volume) || 0), 0)
      const colorReference = Number.isFinite(previousClose) ? previousClose : Number(raw[0].price)
      raw.forEach((point, index) => {
        const volume = Number(point.volume) || 0
        if (!maxVolume || !volume) return
        const barWidth = Math.max(1, Math.min(10, plotWidth / Math.max(1, raw.length) * 0.72))
        const barHeight = volume / maxVolume * volumeHeight
        context.globalAlpha = 0.42
        context.fillStyle = Number(point.price) >= colorReference ? "#ef6b6b" : "#48c5a0"
        context.fillRect(x(index) - barWidth / 2, plotBottom + volumeHeight - barHeight, barWidth, barHeight)
        context.globalAlpha = 1
      })
      const drawLine = (field, color, widthValue) => {
        context.beginPath()
        raw.forEach((point, index) => {
          const value = Number(point[field])
          if (!Number.isFinite(value) || value <= 0) return
          if (index === 0) context.moveTo(x(index), y(value))
          else context.lineTo(x(index), y(value))
        })
        context.strokeStyle = color
        context.lineWidth = widthValue
        context.stroke()
      }
      drawLine("price", "#58b9d7", 1.8)
      drawLine("average", "#f0b768", 1.25)
      if (Number.isFinite(previousClose) && previousClose >= minimum && previousClose <= maximum) {
        const referenceY = y(previousClose)
        context.setLineDash([5, 4])
        context.strokeStyle = "#91a0a7"
        context.lineWidth = 1
        context.beginPath()
        context.moveTo(left, referenceY)
        context.lineTo(width - right, referenceY)
        context.stroke()
        context.setLineDash([])
        context.font = '700 10px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'
        context.textAlign = "right"
        const label = "0%"
        const labelWidth = context.measureText(label).width + 8
        const labelX = width - right
        const labelY = Math.max(top + 7, Math.min(plotBottom - 7, referenceY))
        context.fillStyle = "#171d21"
        context.fillRect(labelX - labelWidth, labelY - 7, labelWidth, 14)
        context.strokeStyle = "#66757d"
        context.strokeRect(labelX - labelWidth, labelY - 7, labelWidth, 14)
        context.fillStyle = "#cbd5d9"
        context.fillText(label, labelX - 4, labelY + 3)
      }
      context.strokeStyle = "#2b363c"
      context.beginPath()
      context.moveTo(left, plotBottom)
      context.lineTo(width - right, plotBottom)
      context.stroke()
      context.textAlign = "center"
      context.fillStyle = "#91a0a7"
      context.font = '11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      const labels = width < 560 ? [0, Math.max(0, raw.length - 1)] : [0, Math.floor((raw.length - 1) / 2), raw.length - 1]
      labels.forEach(index => {
        const point = raw[index]
        context.fillText(String(point.trade_date || "") + " " + String(point.time || ""), x(index), height - 24)
      })
      this.globalChartGeometry = {
        mode: "intraday", items: raw, left, right: width - right, top, plotBottom,
        volumeBottom: plotBottom + volumeHeight, xOf: (_item, index) => x(index), yOf: y,
        valueAtY: position => maximum - (position - top) / plotHeight * (maximum - minimum),
        referenceOf: () => previousClose,
      }
    },
    drawGlobalDailyChart(context, width, height) {
      const allBars = this.globalBars
      const bars = this.globalDailyWindow()
      if (!bars.length) { this.drawGlobalEmpty(context, "暂无外盘日 K 数据"); return }
      const firstIndex = Math.max(0, allBars.length - bars.length)
      const left = width < 560 ? 58 : 70
      const right = width < 560 ? 68 : 88
      const top = 18
      const bottom = 52
      const volumeHeight = 72
      const plotBottom = height - bottom - volumeHeight
      const plotHeight = Math.max(24, plotBottom - top)
      const values = []
      bars.forEach((bar, index) => {
        ;[bar.high, bar.low, bar.open, bar.close].forEach(value => {
          if (Number.isFinite(Number(value)) && Number(value) > 0) values.push(Number(value))
        })
        ;[5, 20, 60].forEach(length => {
          const average = this.globalMovingAverage(allBars, firstIndex + index, length)
          if (Number.isFinite(average) && average > 0) values.push(average)
        })
      })
      if (!values.length) { this.drawGlobalEmpty(context, "暂无有效外盘日 K 数据"); return }
      const minimum = Math.min(...values)
      const maximum = Math.max(...values)
      const padding = (maximum - minimum) * 0.08 || Math.max(0.1, maximum * 0.002)
      const low = minimum - padding
      const high = maximum + padding
      const xStep = (width - left - right) / Math.max(1, bars.length)
      const bodyWidth = Math.max(2, xStep * 0.58)
      const x = index => left + (index + 0.5) * xStep
      const y = value => top + (high - value) / Math.max(0.000001, high - low) * plotHeight
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
        context.fillText((high - (high - low) * index / 4).toFixed(2), left - 8, lineY + 4)
      }
      const maxVolume = Math.max(...bars.map(bar => Number(bar.volume) || 0), 0)
      bars.forEach((bar, index) => {
        const candleX = x(index)
        const open = y(Number(bar.open))
        const close = y(Number(bar.close))
        const rising = Number(bar.close) >= Number(bar.open)
        const color = rising ? "#ef6b6b" : "#48c5a0"
        context.strokeStyle = color
        context.fillStyle = color
        context.lineWidth = 1
        context.beginPath()
        context.moveTo(candleX, y(Number(bar.high)))
        context.lineTo(candleX, y(Number(bar.low)))
        context.stroke()
        context.fillRect(candleX - bodyWidth / 2, Math.min(open, close), bodyWidth, Math.max(1, Math.abs(close - open)))
        const volume = Number(bar.volume) || 0
        if (maxVolume && volume) {
          const volumeHeight = volume / maxVolume * 72
          context.globalAlpha = 0.5
          context.fillRect(candleX - bodyWidth / 2, plotBottom + 72 - volumeHeight, bodyWidth, volumeHeight)
          context.globalAlpha = 1
        }
      })
      const drawAverage = (length, color) => {
        context.beginPath()
        let started = false
        bars.forEach((bar, index) => {
          const value = this.globalMovingAverage(allBars, firstIndex + index, length)
          if (!Number.isFinite(value)) return
          if (started) context.lineTo(x(index), y(value))
          else { context.moveTo(x(index), y(value)); started = true }
        })
        context.strokeStyle = color
        context.lineWidth = 1.25
        context.stroke()
      }
      drawAverage(5, "#58b9d7")
      drawAverage(20, "#f0b768")
      drawAverage(60, "#b28ee8")
      context.strokeStyle = "#2b363c"
      context.beginPath()
      context.moveTo(left, plotBottom)
      context.lineTo(width - right, plotBottom)
      context.stroke()
      context.textAlign = "center"
      context.fillStyle = "#91a0a7"
      const labelEvery = Math.max(1, Math.ceil(bars.length / 7))
      bars.forEach((bar, index) => {
        if (index % labelEvery === 0) context.fillText(String(bar.date || "").slice(5), x(index), height - 24)
      })
      this.globalChartGeometry = {
        mode: "daily", items: bars, left, right: width - right, top, plotBottom,
        volumeBottom: plotBottom + 72, xOf: (_item, index) => x(index), yOf: y,
        valueAtY: position => high - (position - top) / plotHeight * (high - low),
        referenceOf: (_item, index) => index > 0 ? Number(bars[index - 1].close) : NaN,
      }
    },
    handleGlobalChartPointer(event) {
      const geometry = this.globalChartGeometry
      const canvas = this.$refs.globalChart
      if (!geometry || !canvas || !geometry.items.length) return
      const rect = canvas.getBoundingClientRect()
      const pointerX = event.clientX - rect.left
      const pointerY = event.clientY - rect.top
      if (pointerX < geometry.left || pointerX > geometry.right || pointerY < geometry.top || pointerY > geometry.volumeBottom) return this.clearGlobalChartCrosshair()
      let nearestIndex = 0
      let nearestDistance = Number.POSITIVE_INFINITY
      geometry.items.forEach((item, index) => {
        const distance = Math.abs(geometry.xOf(item, index) - pointerX)
        if (distance < nearestDistance) { nearestDistance = distance; nearestIndex = index }
      })
      const item = geometry.items[nearestIndex]
      const indexChart = this.isMarketIndex(this.data.symbol)
      this.globalCrosshair = { mode: geometry.mode, x: geometry.xOf(item, nearestIndex), y: Math.max(geometry.top, Math.min(geometry.plotBottom, pointerY)), item, index: nearestIndex }
      this.drawGlobalChart()
    },
    clearGlobalChartCrosshair() {
      if (this.globalCrosshair == null) return
      this.globalCrosshair = null
      this.drawGlobalChart()
    },
    drawGlobalCrosshair(context, width, height) {
      const geometry = this.globalChartGeometry
      const crosshair = this.globalCrosshair
      if (!geometry || !crosshair || geometry.mode !== crosshair.mode) return
      const index = Math.max(0, Math.min(geometry.items.length - 1, Number(crosshair.index)))
      const item = geometry.items[index]
      const x = geometry.xOf(item, index)
      const y = Math.max(geometry.top, Math.min(geometry.plotBottom, crosshair.y))
      context.save()
      context.setLineDash([3, 4])
      context.strokeStyle = "#71838c"
      context.lineWidth = 1
      context.beginPath()
      context.moveTo(x, geometry.top)
      context.lineTo(x, geometry.volumeBottom)
      context.moveTo(geometry.left, y)
      context.lineTo(geometry.right, y)
      context.stroke()
      context.setLineDash([])
      const value = Number(geometry.mode === "intraday" ? item.price : item.close)
      const reference = Number(geometry.referenceOf ? geometry.referenceOf(item, index) : NaN)
      const change = value - reference
      const percent = Number.isFinite(reference) && reference > 0 ? change / reference * 100 : NaN
      const label = geometry.mode === "intraday" ? String(item.trade_date || "") + " " + String(item.time || "") : String(item.date || "--")
      context.font = '11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      context.textAlign = "center"
      const labelWidth = Math.max(58, context.measureText(label).width + 14)
      const labelX = Math.max(geometry.left, Math.min(geometry.right - labelWidth, x - labelWidth / 2))
      context.fillStyle = "#344249"
      context.fillRect(labelX, height - 41, labelWidth, 18)
      context.fillStyle = "#edf3f5"
      context.fillText(label, labelX + labelWidth / 2, height - 28)
      const rows = geometry.mode === "intraday"
        ? [["现价", this.globalNumber(item.price)], ["涨跌", Number.isFinite(percent) ? (change > 0 ? "+" : "") + this.globalNumber(change) + "  " + (percent > 0 ? "+" : "") + this.globalNumber(percent) + "%" : "--"], ["均价", this.globalNumber(item.average)], ["分钟量", this.globalCompact(item.volume)]]
        : [["开", this.globalNumber(item.open)], ["高", this.globalNumber(item.high)], ["低", this.globalNumber(item.low)], ["收", this.globalNumber(item.close)], ["成交量", this.globalCompact(item.volume)]]
      const boxWidth = 174
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
      context.fillText(label, boxX + 10, boxY + 19)
      context.font = '11px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      rows.forEach((row, rowIndex) => {
        const rowY = boxY + 39 + rowIndex * 19
        context.fillStyle = "#91a0a7"
        context.textAlign = "left"
        context.fillText(row[0], boxX + 10, rowY)
        context.fillStyle = row[0] === "涨跌" ? (change > 0 ? "#ef6b6b" : change < 0 ? "#48c5a0" : "#edf3f5") : "#edf3f5"
        context.textAlign = "right"
        context.fillText(row[1], boxX + boxWidth - 10, rowY)
      })
      context.restore()
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
        context.fillStyle = "#edf3f5"
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
        ? [{ label: this.intradayWhiteLabel(), value: this.number(item.price) }, { label: "涨跌", value: changeText, color: changeColor }, { label: this.intradayYellowLabel(), value: this.number(indexChart ? item.leading : item.average) }, { label: "分钟量", value: this.compact(item.volume) }]
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
      const indexChart = this.isMarketIndex(this.data.symbol)
      const left = indexChart ? 72 : 54
      const right = 16
      const top = 18
      const bottom = 52
      const volumeHeight = 74
      const plotBottom = height - bottom - volumeHeight
      const plotHeight = Math.max(20, plotBottom - top)
      const plotWidth = Math.max(20, width - left - right)
      const points = this.minutes.map(point => ({ ...point, slot: this.minuteSlot(point.time) })).filter(point => point.slot != null && Number.isFinite(Number(point.price)) && Number(point.price) > 0)
      if (!points.length) {
        context.fillStyle = "#91a0a7"
        context.font = '12px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
        context.fillText("暂无有效分时数据", 18, 28)
        return
      }
      if (indexChart && points.length < 2) {
        context.fillStyle = "#91a0a7"
        context.font = '12px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
        context.fillText("等待开盘形成有效指数走势", 18, 28)
        return
      }
      const lunchGap = Math.min(52, Math.max(24, plotWidth * 0.06))
      const sessionWidth = (plotWidth - lunchGap) / 2
      const x = slot => {
        if (slot < morningSlots) return left + slot / (morningSlots - 1) * sessionWidth
        return left + sessionWidth + lunchGap + (slot - morningSlots) / (afternoonSlots - 1) * sessionWidth
      }
      const priceValues = points.map(point => Number(point.price)).filter(value => Number.isFinite(value) && value > 0)
      const leadingValues = points.map(point => Number(point.leading)).filter(value => Number.isFinite(value) && value > 0)
      const values = indexChart
        ? [...priceValues, ...leadingValues, Number(this.quote.high), Number(this.quote.low)].filter(value => Number.isFinite(value) && value > 0)
        : points.flatMap(point => [Number(point.price), Number(point.average)]).filter(value => Number.isFinite(value) && value > 0)
      const previousClose = Number(this.quote.previous_close)
      if (!indexChart && Number.isFinite(previousClose) && previousClose > 0) values.push(previousClose)
      let minimum = Math.min(...values)
      let maximum = Math.max(...values)
      const padding = indexChart ? (maximum === minimum ? Math.max(0.1, maximum * 0.001) : 0) : ((maximum - minimum) * 0.08 || Math.max(0.1, maximum * 0.002))
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
          if (!Number.isFinite(value) || value <= 0) return
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
      drawLine("price", "#edf3f5", 1.8)
      if (indexChart) {
        if (leadingValues.length) drawLine("leading", "#f0b768", 1.3)
      } else {
        drawLine("average", "#f0b768", 1.3)
      }
      if (Number.isFinite(previousClose) && previousClose >= minimum && previousClose <= maximum) {
		const referenceY = y(previousClose)
        context.setLineDash([5, 4])
        context.strokeStyle = "#91a0a7"
        context.lineWidth = 1
        context.beginPath()
		context.moveTo(left, referenceY)
		context.lineTo(width - right, referenceY)
        context.stroke()
        context.setLineDash([])
		context.font = '700 10px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'
		context.textAlign = "right"
		const label = "0%"
		const labelWidth = context.measureText(label).width + 8
		const labelX = width - right
		const labelY = Math.max(top + 7, Math.min(plotBottom - 7, referenceY))
		context.fillStyle = "#171d21"
		context.fillRect(labelX - labelWidth, labelY - 7, labelWidth, 14)
		context.strokeStyle = "#66757d"
		context.strokeRect(labelX - labelWidth, labelY - 7, labelWidth, 14)
		context.fillStyle = "#cbd5d9"
		context.fillText(label, labelX - 4, labelY + 3)
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
      const indexChart = this.isMarketIndex(this.data.symbol)
      const left = indexChart ? 72 : 48
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
        const lowValue = Number(bar.low)
        const highValue = Number(bar.high)
        if (Number.isFinite(lowValue) && lowValue > 0) values.push(lowValue)
        if (Number.isFinite(highValue) && highValue > 0) values.push(highValue)
        if (!indexChart) {
          ;[5, 20, 60].forEach(length => {
            const value = this.movingAverage(allBars, firstSourceIndex + index, length)
            if (Number.isFinite(value) && value > 0) values.push(value)
          })
        }
      })
      if (!values.length) {
        context.fillStyle = "#91a0a7"
        context.fillText("暂无有效日 K 数据", 18, 28)
        return
      }
      const minimum = Math.min(...values)
      const maximum = Math.max(...values)
      const padding = indexChart ? (maximum === minimum ? Math.max(0.1, maximum * 0.001) : 0) : ((maximum - minimum) * 0.08 || 1)
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
          if (!Number.isFinite(value) || value <= 0 || (indexChart && (value < low || value > high))) return
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
    this.loadSentiment()
    this.loadBoards()
    this.loadMarketRankings()
    this.loadGlobalChart(this.globalSelectedSymbol, true)
    this.loadWatchlist()
    this.loadAIConfig()
    this.timer = window.setInterval(() => {
      if (this.workspaceMode === "market") this.load(this.requestedSymbol)
      this.loadIndices()
      this.loadSentiment()
      this.loadBoards()
      this.loadMarketRankings()
      if (this.workspaceMode === "global") this.loadGlobalChart(this.globalSelectedSymbol)
      this.loadWatchlist()
      this.pollRealtimeSession()
      if (this.assistantOpen) {
        this.loadAssistantAlerts()
        if (!this.assistantLoading && Date.now() - this.assistantContextCheckedAt >= 30000) this.loadAssistantContext(this.assistantSymbol)
        if (this.assistantJobID && !this.assistantJobTimer) this.pollAssistantJob()
      }
    }, 10000)
    window.addEventListener("resize", this.handleResize)
  },
  beforeUnmount() {
    window.clearInterval(this.timer)
    if (this.assistantJobTimer != null) window.clearTimeout(this.assistantJobTimer)
    if (this.crosshairFrame != null) window.cancelAnimationFrame(this.crosshairFrame)
    this.setAssistantBodyLock(false)
    window.removeEventListener("resize", this.handleResize)
  },
}).mount("#app")
