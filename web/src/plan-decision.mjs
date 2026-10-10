import { Calculator, ShieldCheck, Bell, BookmarkPlus } from "lucide-vue-next"

export function decisionSummary({ analysis, structure, quote = {}, historical, historyError, quoteError, session, today, now = Date.now() }) {
  const result = { state: "waiting", label: "等待结构数据", detail: "", blocksMonitoring: true }
  if (!analysis || !structure) return result
  if (historical) return { ...result, state: "history", label: "历史回看", detail: "仅复核所选日期，不使用当前报价确认历史计划。" }
  const price = Number(quote.current)
  const quoteDate = String(quote.quote_time || "").slice(0, 10)
  const timestamp = Date.parse(String(quote.quote_time || "").replace(" ", "T") + "+08:00")
  const stale = historyError || quoteError || /缓存/.test(analysis.source || "") || !quoteDate ||
    !Number.isFinite(price) || price <= 0 || !Number.isFinite(timestamp) ||
    (session === "trading" && (quoteDate !== today || now - timestamp > 10 * 60000 || timestamp - now > 60000))
  if (stale) return { ...result, state: "stale", label: "数据待更新", detail: "报价或日K暂不可用于最新确认，风险试算仅作历史情景参考。" }
  if (analysis.data_date < quoteDate) return { ...result, state: "stale", label: "日K落后于报价", detail: "等待日K更新后再启用新计划监控。" }
  if (structure.state === "invalidated") return { ...result, state: "invalidated", label: "结构已失效", detail: "原有条件不再成立，等待重新形成可观察结构。" }
  const plan = structure.plan
  if (!plan) return { ...result, state: "risk", label: "风险观察", detail: "该结构不生成做多计划。" }
  if (price <= plan.invalidation) return { ...result, state: "invalidated", label: "报价已触及失效位", detail: "当前报价越过原计划风险边界，等待后台校验或重新分析。" }
  const normal = { ...result, blocksMonitoring: false }
  if (price > plan.entry_high) return { ...normal, label: "已超出入场区间", detail: "不按旧区间追认入场；可保留计划观察回落。" }
  if (structure.state !== "confirmed") return { ...normal, label: analysis.complete ? "等待结构确认" : "等待完整日K确认", detail: plan.confirmation }
  if (price < plan.entry_low) return { ...normal, label: "等待进入观察区间", detail: "日线结构已确认，当前报价尚未进入冻结区间。" }
  return { ...normal, state: "in_zone", label: session === "trading" ? "区间内观察" : "收盘快照位于区间", detail: "区间位置不等于成交或买入信号，触发状态以后台监控校验为准。" }
}

export const PlanDecision = {
  components: { Calculator, ShieldCheck, Bell, BookmarkPlus },
  props: ["analysis", "structure", "quote", "historical", "historyError", "quoteError", "session", "today", "canSave", "canMonitor", "saving", "expiresOn"],
  emits: ["save", "expiry"],
  data() {
    return {
      budget: { equity: "", cash: "", risk_percent: 1, max_position_percent: 20, existing_shares: 0, existing_cost: 0, commission_bps: 2.5, minimum_commission: 5, stamp_duty_bps: 5, transfer_bps: 0.1, slippage_bps: 5, gap_percent: 5 },
      result: null, error: "", busy: false, controller: null, requestID: 0,
    }
  },
  computed: {
    summary() { return decisionSummary(this.$props) },
    identity() { return `${this.analysis?.symbol}:${this.analysis?.fingerprint}:${this.structure?.id}` },
    hasPlan() { return this.structure?.plan && this.structure.state !== "invalidated" },
    budgetReady() { return Number(this.budget.equity) > 0 && this.budget.cash !== "" && this.hasPlan },
  },
  watch: {
    identity() { this.invalidate() },
    "analysis.symbol"() { this.budget.existing_shares = 0; this.budget.existing_cost = 0 },
    budget: { deep: true, handler() { this.invalidate() } },
  },
  methods: {
    money(value) { return value == null ? "--" : Number(value).toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 }) },
    invalidate() { this.requestID++; this.controller?.abort(); this.result = null; this.error = ""; this.busy = false },
    async calculate() {
      if (!this.budgetReady || this.busy) return
      this.invalidate()
      const id = this.requestID
      const identity = this.identity
      this.busy = true
      this.controller = new AbortController()
      try {
        const budget = Object.fromEntries(Object.entries(this.budget).map(([key, value]) => [key, Number(value)]))
        const response = await fetch("/api/position-preview", { method: "POST", headers: { "Content-Type": "application/json" }, signal: this.controller.signal,
          body: JSON.stringify({ symbol: this.analysis.symbol, through: this.analysis.data_date, fingerprint: this.analysis.fingerprint, structure_id: this.structure.id, budget }) })
        const payload = await response.json()
        if (id !== this.requestID || identity !== this.identity) return
        if (!response.ok) throw new Error(payload.error || "仓位试算失败")
        this.result = payload
      } catch (error) {
        if (id === this.requestID && error.name !== "AbortError") this.error = error.message || String(error)
      } finally { if (id === this.requestID) this.busy = false }
    },
  },
  beforeUnmount() { this.invalidate() },
  template: `<div class="plan-decision" aria-label="决策摘要与仓位试算">
    <div class="decision-heading"><shield-check :size="17"/><h3>决策摘要</h3><strong :class="summary.state">{{ summary.label }}</strong></div>
    <p class="decision-detail">{{ summary.detail }}</p>
    <dl class="decision-facts"><div><dt>图表来源</dt><dd>{{ analysis.source || '--' }} · {{ analysis.data_date }}</dd></div><div><dt>报价时间</dt><dd>{{ quote?.quote_time || '--' }}</dd></div></dl>
    <div class="decision-actions" v-if="hasPlan">
      <label><span>有效至</span><input type="date" :value="expiresOn" :min="today" @input="$emit('expiry', $event.target.value)" aria-label="计划有效期"></label>
      <button type="button" :disabled="!canSave || saving" @click="$emit('save', false)"><bookmark-plus :size="15"/>保存观察计划</button>
      <button type="button" :disabled="!canSave || !canMonitor || summary.blocksMonitoring || saving" @click="$emit('save', true)"><bell :size="15"/>保存并启用监控</button>
    </div>
    <details class="position-preview" v-if="hasPlan">
      <summary><calculator :size="16"/><strong>仓位风险试算</strong><span>手动资金 · 不下单</span></summary>
      <form @submit.prevent="calculate">
        <div class="risk-input-grid">
          <label><span>账户权益（元）</span><input type="number" v-model.number="budget.equity" min="1" max="1000000000000" step="0.01" required aria-label="试算账户权益"></label>
          <label><span>可用现金（元）</span><input type="number" v-model.number="budget.cash" min="0" :max="Number(budget.equity) || undefined" step="0.01" required aria-label="试算可用现金"></label>
          <label><span>本股风险预算（%）</span><input type="number" v-model.number="budget.risk_percent" min="0.01" max="100" step="0.01" required aria-label="本股风险预算"></label>
          <label><span>本股仓位上限（%）</span><input type="number" v-model.number="budget.max_position_percent" min="0.01" max="100" step="0.01" required aria-label="本股仓位上限"></label>
          <label><span>本股已有持仓（股）</span><input type="number" v-model.number="budget.existing_shares" min="0" max="1000000000" step="1" required aria-label="已有持仓股数"></label>
          <label><span>持仓每股成本（元）</span><input type="number" v-model.number="budget.existing_cost" :min="budget.existing_shares > 0 ? 0.01 : 0" step="0.01" :disabled="!budget.existing_shares" required aria-label="持仓每股成本"></label>
        </div>
        <details class="risk-fees"><summary>费用与压力情景</summary><div class="risk-input-grid">
          <label><span>佣金（bp）</span><input type="number" v-model.number="budget.commission_bps" min="0" max="100" step="0.1" aria-label="佣金基点"></label>
          <label><span>最低佣金（元）</span><input type="number" v-model.number="budget.minimum_commission" min="0" max="10000" step="0.01" aria-label="最低佣金"></label>
          <label><span>卖出印花税（bp）</span><input type="number" v-model.number="budget.stamp_duty_bps" min="0" max="100" step="0.1" aria-label="印花税基点"></label>
          <label><span>双边过户费（bp）</span><input type="number" v-model.number="budget.transfer_bps" min="0" max="100" step="0.01" aria-label="过户费基点"></label>
          <label><span>每边滑点（bp）</span><input type="number" v-model.number="budget.slippage_bps" min="0" max="1000" step="1" aria-label="滑点基点"></label>
          <label><span>失效位额外跳空（%）</span><input type="number" v-model.number="budget.gap_percent" min="0" max="50" step="1" aria-label="额外跳空比例"></label>
        </div></details>
        <div class="risk-submit"><button type="submit" :disabled="!budgetReady || busy"><calculator :size="15"/>{{ busy ? '计算中' : '计算仓位' }}</button><span>入场按区间上沿 {{ money(structure.plan.entry_high) }} 元</span></div>
      </form>
      <p class="chart-plan-error" role="alert" v-if="error">{{ error }}</p>
      <div v-if="result" class="risk-result" aria-live="polite">
        <div class="risk-metrics"><div><span>可新增试算股数</span><strong>{{ result.shares }} <small>股</small></strong></div><div><span>所需现金 · 含买入费用</span><strong>{{ money(result.cash_required) }}</strong></div><div><span>本股合计止损情景损失</span><strong>{{ money(result.total_risk) }}</strong></div><div><span>新增仓位净收益风险比</span><strong>{{ result.net_reward_risk == null ? '--' : result.net_reward_risk + 'R' }}</strong></div></div>
        <p class="risk-constraint">约束：{{ result.constraint || '数量上限' }} · 最低 {{ result.minimum_shares }} 股，递增 {{ result.share_step }} 股</p>
        <dl class="risk-result-details"><div><dt>本股风险预算 / 已有持仓风险</dt><dd>{{ money(result.risk_budget) }} / {{ money(result.existing_risk) }}</dd></div><div><dt>试算后本股仓位</dt><dd>{{ result.position_percent }}%</dd></div><div><dt>目标一 / 目标二净收益</dt><dd>{{ money(result.target_1_profit) }} / {{ money(result.target_2_profit) }}</dd></div><div><dt>额外跳空 {{ budget.gap_percent }}% 合计损失</dt><dd>{{ money(result.gap_loss) }}</dd></div><div><dt>买入 / 止损卖出费用</dt><dd>{{ money(result.buy_fees) }} / {{ money(result.stop_fees) }}</dd></div><div><dt>滑点后入场 / 失效价格</dt><dd>{{ money(result.entry_price) }} / {{ money(result.stop_price) }}</dd></div></dl>
        <p class="chart-plan-risk" v-for="warning in result.warnings" :key="warning">{{ warning }}</p>
      </div>
    </details>
  </div>`,
}
