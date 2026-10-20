import { ClipboardCheck, X } from 'lucide-vue-next'

export const StrategyPreflightView = {
  components: { ClipboardCheck, X },
  props: { request: { type: Object, required: true }, disabled: Boolean },
  emits: ['busy', 'exclude'],
  data() { return { report: null, reportKey: '', checkedAt: 0, error: '', loading: false, controller: null, sequence: 0 } },
  computed: {
    requestKey() { return JSON.stringify(this.request) },
    failures() { return (this.report?.items || []).filter(item => !item.ready) },
    displayItems() { return [...(this.report?.items || [])].sort((a, b) => Number(a.ready) - Number(b.ready) || a.symbol.localeCompare(b.symbol)) },
  },
  watch: { requestKey() { this.invalidate() } },
  methods: {
    invalidate() {
      this.sequence++; this.controller?.abort(); this.controller = null
      this.report = null; this.reportKey = ''; this.error = ''; this.loading = false
      this.$emit('busy', false)
    },
    cancel() { this.invalidate(); this.error = '预检已取消' },
    async inspect(force = false) {
      const key = this.requestKey
      if (!force && this.reportKey === key && this.report?.ready && Date.now() - this.checkedAt < 60000) return true
      this.controller?.abort()
      const controller = new AbortController(), sequence = ++this.sequence
      this.controller = controller; this.loading = true; this.error = ''; this.report = null
      this.$emit('busy', true)
      try {
        const response = await fetch('/api/strategy/preflight', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: key, signal: controller.signal })
        const data = await response.json()
        if (sequence !== this.sequence || key !== this.requestKey) return false
        if (!response.ok) throw Error(data.error || '数据预检失败')
        this.report = data; this.reportKey = key; this.checkedAt = Date.now()
        return data.ready === true
      } catch (error) {
        if (sequence === this.sequence && error.name !== 'AbortError') this.error = error.message
        return false
      } finally {
        if (sequence === this.sequence) { this.loading = false; this.controller = null; this.$emit('busy', false) }
      }
    },
    time(value) { return new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false }) },
  },
  beforeUnmount() { this.invalidate() },
  template: `<section class="strategy-preflight" aria-label="回测数据预检" :aria-busy="loading">
    <div class="sp-head"><strong>数据预检</strong><div><button type="button" class="icon-button" title="检查回测数据" aria-label="检查回测数据" :disabled="disabled || loading" @click="inspect(true)"><clipboard-check :size="16"/></button><button v-if="loading" type="button" class="icon-button" title="取消数据预检" aria-label="取消数据预检" @click="cancel"><x :size="16"/></button></div></div>
    <p v-if="loading" role="status">正在检查 {{ request.symbols.length }} 只股票{{ request.benchmark?'与基准':'' }}</p>
    <p v-if="error" class="sp-error" role="alert">{{ error }}</p>
    <template v-if="report"><p class="sp-summary" :class="report.ready?'sp-ready':'sp-error'" role="status">股票可用 {{ report.available }} / {{ report.items.length }}<span v-if="report.unavailable"> · {{ report.unavailable }} 只不可用</span></p><small>检查 {{ time(report.checked_at) }}</small>
      <p v-if="report.benchmark && !report.benchmark.ready" class="sp-warning">基准不可用，收益对照受限</p>
      <button v-if="failures.length && report.available" type="button" class="text-action sp-exclude" :disabled="disabled" @click="$emit('exclude',failures.map(item=>item.symbol))">本次排除不可用股票</button>
      <details :open="!report.ready" class="sp-details"><summary>数据明细</summary><div class="sp-items"><div v-for="item in displayItems" :key="item.symbol" class="sp-item" :data-preflight-symbol="item.symbol"><div><b>{{ item.name }}</b><span :class="item.ready?'sp-ready':'sp-error'">{{ item.ready?'可用':'不可用' }}</span></div><small>{{ item.symbol }} · {{ item.bars || '--' }} 根 / 至少 {{ item.minimum_bars }} 根</small><small v-if="item.first_date">{{ item.first_date }} 至 {{ item.last_date }}</small><small>含预热 {{ item.required_start }} 至 {{ report.end }}</small><details v-if="item.detail"><summary>原因</summary><p>{{ item.detail }}</p></details></div>
      <div v-if="report.benchmark" class="sp-item"><div><b>沪深300基准</b><span :class="report.benchmark.ready?'sp-ready':'sp-warning'">{{ report.benchmark.ready?'可用':'不可用' }}</span></div><small v-if="report.benchmark.first_date">{{ report.benchmark.first_date }} 至 {{ report.benchmark.last_date }}</small><p v-if="report.benchmark.detail">{{ report.benchmark.detail }}</p></div></div></details>
    </template>
  </section>`,
}
