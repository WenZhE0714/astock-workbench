import { Play, Square, RefreshCw, Download, ArrowUpRight, ChevronLeft, ChevronRight } from 'lucide-vue-next'
import { validationPatterns, validationPercent, validationState, validationOutcome, validationSymbols, validationTone } from './pattern-validation.mjs'
import { shanghaiToday } from './review-calendar.mjs'

export const PatternValidationView = {
  components: { Play, Square, RefreshCw, Download, ArrowUpRight, ChevronLeft, ChevronRight },
  props: ['selection', 'defaultSymbol'],
  emits: ['selection', 'open-sample'],
  data() {
    const today = shanghaiToday(), start = new Date(today+'T12:00:00Z')
    start.setUTCFullYear(start.getUTCFullYear()-1)
    return { symbols:this.selection?.symbols || this.defaultSymbol || '002080', start:this.selection?.start || start.toISOString().slice(0,10), end:this.selection?.end || today,
      runID:this.selection?.runID || '', pattern:this.selection?.pattern || '', bias:this.selection?.bias || '', regime:this.selection?.regime || '', state:this.selection?.state || '', horizon:this.selection?.horizon || 5, offset:this.selection?.offset || 0,
      view:null, runs:[], running:false, loading:false, error:'', controller:null, historyController:null, requestID:0, loadedKey:'', adoptArchive:false, patterns:validationPatterns, today }
  },
  computed: {
    selectionValue() { return {symbols:this.symbols,start:this.start,end:this.end,runID:this.runID,pattern:this.pattern,bias:this.bias,regime:this.regime,state:this.state,horizon:this.horizon,offset:this.offset} },
    filterKey() { return [this.pattern,this.bias,this.regime,this.state,this.horizon].join('|') },
    requestKey() { return `${this.runID}|${this.filterKey}|${this.offset}` },
    report() { return this.view?.report },
    totals() { return this.view?.totals || {} },
    busy() { return this.running || this.loading },
    exportURL() { return this.runID ? `/api/pattern-validation?id=${encodeURIComponent(this.runID)}&download=1` : '' },
  },
  watch: {
    selectionValue: {deep:true, handler(value) { this.$emit('selection',value) }},
    filterKey() { this.offset = 0 },
    requestKey() { if (!this.running && this.requestKey !== this.loadedKey) this.load() },
  },
  methods: {
    percent:validationPercent, status:validationState, outcome:validationOutcome, tone:validationTone,
    biasLabel(value) { return value === 'bullish' ? '看涨' : '看跌' },
    time(value) { return new Date(value).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false}) },
    number(value) { return value == null ? '--' : Number(value).toFixed(2) },
    cancel() { this.controller?.abort() },
    open(sample,date) { this.$emit('open-sample',{symbol:sample.symbol,date,patternID:sample.pattern_id,runID:this.runID}) },
    async history() {
      this.historyController?.abort()
      const controller = new AbortController(); this.historyController = controller
      try {
        const response = await fetch('/api/pattern-validation',{cache:'no-store',signal:controller.signal}), payload = await response.json()
        if (!response.ok) throw new Error(payload.error || '读取验证归档失败')
        this.runs = payload.items || []
      } catch(error) { if (error.name !== 'AbortError') this.error = error.message || String(error) }
    },
    async load() {
      this.controller?.abort()
      const id = ++this.requestID, key = this.requestKey
      this.view = null; this.error = ''; this.loading = false
      if (!this.runID) return
      const controller = new AbortController(); this.controller = controller; this.loading = true
      try {
        const query = new URLSearchParams({id:this.runID,pattern:this.pattern,bias:this.bias,regime:this.regime,state:this.state,horizon:String(this.horizon),offset:String(this.offset),limit:'30'})
        const response = await fetch(`/api/pattern-validation?${query}`,{cache:'no-store',signal:controller.signal}), payload = await response.json()
        if (id !== this.requestID) return
        if (!response.ok) throw new Error(payload.error || '读取验证结果失败')
        if (payload.report?.run_id !== this.runID) throw new Error('归档与当前选择不一致，请重试')
        if (this.adoptArchive) {
          this.symbols = payload.report.request.symbols.join(', ')
          this.start = payload.report.request.start; this.end = payload.report.request.end; this.adoptArchive = false
        }
        this.view = payload; this.loadedKey = key
      } catch(error) { if (id === this.requestID && error.name !== 'AbortError') this.error = error.message || String(error) }
      finally { if (id === this.requestID) this.loading = false }
    },
    async run() {
      const symbols = validationSymbols(this.symbols)
      if (!symbols.length || symbols.length > 10) { this.error = '股票池须包含1至10只股票'; return }
      this.controller?.abort()
      const id = ++this.requestID, controller = new AbortController()
      this.controller = controller; this.running = true; this.loading = false; this.error = ''; this.view = null
      try {
        const response = await fetch('/api/pattern-validation',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({symbols,start:this.start,end:this.end}),signal:controller.signal}), payload = await response.json()
        if (id !== this.requestID) return
        if (!response.ok) throw new Error(payload.error || '运行形态验证失败')
        this.pattern = ''; this.bias = ''; this.regime = ''; this.state = ''; this.horizon = 5; this.offset = 0
        this.runID = payload.report.run_id; this.view = payload; this.loadedKey = this.requestKey
        await this.history()
      } catch(error) { if (id === this.requestID) this.error = error.name === 'AbortError' ? '验证已取消' : error.message || String(error) }
      finally { if (id === this.requestID) this.running = false }
    },
  },
  mounted() { this.history(); if (this.runID) this.load() },
  beforeUnmount() { this.requestID++; this.controller?.abort(); this.historyController?.abort() },
  template: `<section class="pattern-validation" aria-label="形态验证" :aria-busy="busy">
    <div class="pv-heading"><h2>形态验证</h2><span>历史重建 · 未复权 · 确认后价格表现</span></div>
    <form class="pv-form" @submit.prevent="run">
      <label class="pv-symbols">股票池<input v-model="symbols" aria-label="验证股票池" placeholder="002080, 600519" required :disabled="running"></label>
      <label>开始日期<input type="date" v-model="start" aria-label="验证开始日期" required :max="end" :disabled="running"></label>
      <label>结束日期<input type="date" v-model="end" aria-label="验证结束日期" required :min="start" :max="today" :disabled="running"></label>
      <button type="submit" :disabled="busy"><play :size="14"/>{{ running ? '验证中' : '运行验证' }}</button>
      <button v-if="running" type="button" class="icon-button" title="取消验证" aria-label="取消验证" @click="cancel"><square :size="14"/></button>
    </form>
    <div class="pv-archive"><label>归档<select v-model="runID" @change="adoptArchive = !!runID" aria-label="形态验证归档" :disabled="running"><option value="">本次验证</option><option v-for="run in runs" :key="run.run_id" :value="run.run_id">{{ time(run.generated_at) }} · {{ run.request.symbols.join(', ') }} · {{ run.samples }} 样本</option></select></label><button type="button" class="icon-button" title="刷新归档" aria-label="刷新验证归档" :disabled="running" @click="history"><refresh-cw :size="15"/></button><a v-if="runID && !busy" :href="exportURL" class="icon-button" title="导出完整验证归档" aria-label="导出完整验证归档"><download :size="15"/></a></div>
    <p v-if="error" class="chart-plan-error" role="alert">{{ error }}<button v-if="runID && !running" type="button" class="text-action" @click="load">重试读取</button></p>
    <div class="pv-filters"><div class="dashboard-segment" role="group" aria-label="形态验证窗口"><button v-for="days in [5,10,20]" :key="days" type="button" :aria-pressed="horizon === days" :class="{active:horizon === days}" :disabled="running" @click="horizon = days">{{ days }}日</button></div><select v-model="pattern" aria-label="筛选验证形态" :disabled="running"><option value="">全部形态</option><option v-for="item in patterns" :value="item[0]" :key="item[0]">{{ item[1] }}</option></select><select v-model="bias" aria-label="筛选验证方向" :disabled="running"><option value="">全部方向</option><option value="bullish">看涨</option><option value="bearish">看跌</option></select><select v-model="regime" aria-label="筛选市场阶段" :disabled="running"><option value="">全部市场阶段</option><option v-for="item in ['牛市','熊市','震荡','高波动','数据不足']" :key="item">{{ item }}</option></select><select v-model="state" aria-label="筛选样本状态" :disabled="running"><option value="">全部状态</option><option value="watching">待确认</option><option value="forming">突破待确认</option><option value="confirmed">已确认</option><option value="invalidated">已失效</option><option value="expired">观察到期</option></select></div>
    <p v-if="busy" class="pv-empty" role="status">{{ running ? '正在逐日验证并归档' : '正在读取验证结果' }}</p>
    <template v-if="view">
      <div class="pv-basis"><span>{{ report.request.start }} 至 {{ report.request.end }} · 数据截止 {{ report.cutoff }}</span><span>{{ report.version }} · {{ time(report.generated_at) }}</span></div>
      <div class="pv-totals"><div><span>识别 / 确认</span><strong>{{ totals.samples }} / {{ totals.confirmed }}</strong><small>确认前失效 {{ totals.invalidated_before_confirmation }}</small></div><div><span>{{ horizon }}日成熟 / 待成熟</span><strong>{{ totals.mature }} / {{ totals.pending }}</strong><small>数据缺失 {{ totals.unavailable }}</small></div><div><span>平均方向表现</span><strong :class="tone(totals.average_direction_return)">{{ percent(totals.average_direction_return) }}</strong><small>价格涨跌 {{ percent(totals.average_price_return) }}</small></div><div><span>窗口失效率</span><strong>{{ percent(totals.invalidation_rate) }}</strong><small>仅完整成熟窗口</small></div></div>
      <div class="pv-section-title"><h3>形态与市场阶段</h3><span>{{ view.groups.length }} 组 · {{ horizon }}日窗口</span></div>
      <table class="pv-group-table" v-if="view.groups.length"><thead><tr><th>形态 / 方向 / 市场</th><th>识别 / 确认</th><th>成熟 / 待成熟 / 缺失</th><th>价格涨跌</th><th>方向表现</th><th>有利 / 不利波动</th><th>失效率</th></tr></thead><tbody><tr v-for="group in view.groups" :key="group.pattern_id + group.regime"><td data-label="形态"><strong>{{ group.name }}</strong><small>{{ biasLabel(group.bias) }} · {{ group.regime }}</small></td><td data-label="识别 / 确认">{{ group.totals.samples }} / {{ group.totals.confirmed }}</td><td data-label="成熟 / 待成熟 / 缺失">{{ group.totals.mature }} / {{ group.totals.pending }} / {{ group.totals.unavailable }}<small v-if="group.totals.mature < 20">样本不足20</small></td><td data-label="价格涨跌" :class="tone(group.totals.average_price_return)">{{ percent(group.totals.average_price_return) }}</td><td data-label="方向表现" :class="tone(group.totals.average_direction_return)">{{ percent(group.totals.average_direction_return) }}</td><td data-label="有利 / 不利">{{ percent(group.totals.average_favorable) }} / {{ percent(group.totals.average_adverse) }}</td><td data-label="失效率">{{ percent(group.totals.invalidation_rate) }}</td></tr></tbody></table>
      <p v-else class="pv-empty">当前条件下没有样本</p>
      <div class="pv-section-title"><h3>样本明细</h3><span>{{ view.total }} 条</span></div>
      <details class="pv-sample" v-for="sample in view.samples" :key="sample.id" :data-sample-id="sample.id"><summary><strong>{{ sample.symbol }} · {{ sample.name }}</strong><span>{{ sample.observed_on }}</span><span>{{ status(sample.state) }}</span><span>{{ status(outcome(sample,horizon).state) }}</span><b :class="tone(outcome(sample,horizon).direction_return)">{{ percent(outcome(sample,horizon).direction_return) }}</b></summary>
        <ol class="pv-timeline"><li><span>首次识别</span><button type="button" :aria-label="'查看识别日 ' + sample.id" @click="open(sample,sample.observed_on)">{{ sample.observed_on }}<arrow-up-right :size="13"/></button></li><li><span>收盘确认</span><button v-if="sample.confirmed_on" type="button" :aria-label="'查看确认日 ' + sample.id" @click="open(sample,sample.confirmed_on)">{{ sample.confirmed_on }}<arrow-up-right :size="13"/></button><b v-else>--</b></li><li><span>失效 / 到期</span><b>{{ sample.invalidated_on || sample.expired_on || '--' }}</b></li><li><span>确认收盘 / 市场</span><b>{{ number(sample.confirmation_close) }} · {{ sample.regime }}</b></li></ol>
        <div class="pv-sample-levels"><span>冻结确认价 {{ number(sample.snapshot.pattern.trigger_price) }}</span><span>冻结失效位 {{ number(sample.snapshot.pattern.invalidation_price) }}</span><span>市场分类日 {{ sample.regime_date }}</span></div>
        <div class="pv-outcome" v-for="result in sample.outcomes" :key="result.horizon"><strong>{{ result.horizon }}日 · {{ status(result.state) }}</strong><span>价格 {{ percent(result.price_return) }} · 方向 {{ percent(result.direction_return) }}</span><span>有利 {{ percent(result.favorable) }} · 不利 {{ percent(result.adverse) }}</span><small>{{ result.detail || (result.through ? '截至 ' + result.through + ' · ' + (result.invalidated ? '窗口内触及失效位' : '窗口内未触及失效位') : '') }}</small></div>
        <details class="pv-evidence"><summary>识别日冻结依据</summary><p v-for="evidence in sample.snapshot.evidence" :key="evidence">{{ evidence }}</p><p class="pv-hash">快照 {{ sample.snapshot_fingerprint }}</p></details>
      </details>
      <div class="pv-pagination" v-if="view.total > 30"><button class="icon-button" type="button" title="上一页" aria-label="上一页样本" :disabled="busy || offset === 0" @click="offset = Math.max(0,offset-30)"><chevron-left :size="16"/></button><span>{{ Math.floor(offset/30)+1 }} / {{ Math.ceil(view.total/30) }}</span><button class="icon-button" type="button" title="下一页" aria-label="下一页样本" :disabled="busy || offset+30 >= view.total" @click="offset += 30"><chevron-right :size="16"/></button></div>
      <details class="pv-coverage" open><summary>数据覆盖</summary><div v-for="item in report.coverage" :key="item.symbol"><strong>{{ item.symbol === 'sh000300' ? '沪深300基准' : item.symbol }}</strong><span>{{ item.first_date || '--' }} 至 {{ item.last_date || '--' }} · {{ item.bars }} 根</span><small>{{ item.error || item.source }}{{ item.cached ? ' · 缓存参考' : '' }}{{ item.late_discoveries ? ' · 非首次识别日排除 ' + item.late_discoveries : '' }}{{ item.first_date && item.first_date > report.request.start ? ' · 起始覆盖不足' : '' }}</small></div></details>
      <details class="pv-method"><summary>统计口径与限制</summary><p v-for="warning in report.warnings" :key="warning">{{ warning }}</p><p class="pv-hash">输入指纹 {{ report.input_hash }}</p></details>
    </template>
    <p v-else-if="!busy && !error" class="pv-empty">暂无验证结果</p>
  </section>`,
}
