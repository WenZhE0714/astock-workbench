import { Play, Square, RefreshCw, Download, ArrowUpRight, ChevronLeft, ChevronRight } from 'lucide-vue-next'
import { validationPatterns, validationPercent, validationState, validationOutcome, validationSymbols, validationTone, validationCoverageStatus, validationActionState, validationActionKinds, validationSourceURL, validationPriceBasis, validationQualityState, validationPoints, validationBenchmarkState, validationPeriod } from './pattern-validation.mjs'
import { shanghaiToday } from './review-calendar.mjs'
import { PatternBatchView } from './pattern-batch-view.js'

export const PatternValidationView = {
  components: { Play, Square, RefreshCw, Download, ArrowUpRight, ChevronLeft, ChevronRight, PatternBatchView },
  props: ['selection', 'defaultSymbol'],
  emits: ['selection', 'open-sample'],
  data() {
    const today = shanghaiToday(), start = new Date(today+'T12:00:00Z')
    start.setUTCFullYear(start.getUTCFullYear()-1)
    return { symbols:this.selection?.symbols || this.defaultSymbol || '002080', start:this.selection?.start || start.toISOString().slice(0,10), end:this.selection?.end || today,
      priceBasis:this.selection?.priceBasis || 'raw',
      batchMode:!!this.selection?.batchMode,
      splitEnabled:!!this.selection?.validationStart, validationStart:this.selection?.validationStart || '', period:this.selection?.period || '',
      runID:this.selection?.runID || '', pattern:this.selection?.pattern || '', bias:this.selection?.bias || '', regime:this.selection?.regime || '', state:this.selection?.state || '', action:this.selection?.action || '', horizon:this.selection?.horizon || 5, offset:this.selection?.offset || 0,
      view:null, runs:[], running:false, loading:false, error:'', controller:null, historyController:null, requestID:0, loadedKey:'', adoptArchive:false, patterns:validationPatterns, today }
  },
  computed: {
    selectionValue() { return {symbols:this.symbols,start:this.start,end:this.end,priceBasis:this.priceBasis,batchMode:this.batchMode,validationStart:this.splitEnabled ? this.validationStart : '',period:this.period,runID:this.runID,pattern:this.pattern,bias:this.bias,regime:this.regime,state:this.state,action:this.action,horizon:this.horizon,offset:this.offset} },
    filterKey() { return [this.pattern,this.bias,this.regime,this.state,this.action,this.horizon,this.period].join('|') },
    requestKey() { return `${this.runID}|${this.filterKey}|${this.offset}` },
    report() { return this.view?.report },
    adjusted() { return this.report?.request.price_basis === 'hfq' },
    numericQuality() { return this.report?.adjustments?.some(item=>item.quality_method) || false },
    resultPriceLabel() { return this.adjusted ? '复权' : '原始' },
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
    percent:validationPercent, status:validationState, outcome:validationOutcome, tone:validationTone, coverageStatus:validationCoverageStatus,
    points:validationPoints, benchmarkStatus:validationBenchmarkState,
    periodLabel:validationPeriod,
    actionStatus(check) { return validationActionState(check,this.numericQuality) },
    actionKinds:validationActionKinds, sourceURL:validationSourceURL, qualityStatus:validationQualityState,
    basisLabel:validationPriceBasis,
    biasLabel(value) { return value === 'bullish' ? '看涨' : '看跌' },
    time(value) { return new Date(value).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false}) },
    number(value) { return value == null ? '--' : Number(value).toFixed(2) },
    qualityNumber(value) { return value == null ? '--' : Number(value).toFixed(4) },
    cancel() { this.controller?.abort() },
    async openBatchRun(id) { this.pattern='';this.bias='';this.regime='';this.state='';this.action='';this.period='';this.horizon=5;this.offset=0;this.adoptArchive=true;this.loadedKey='';this.runID=id;await this.$nextTick();if(!this.loading && this.loadedKey!==this.requestKey)await this.load();this.history() },
    open(sample,date) { this.$emit('open-sample',{symbol:sample.symbol,date,patternID:sample.pattern_id,runID:this.runID,priceBasis:this.report.request.price_basis || 'raw'}) },
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
      const id = ++this.requestID
      this.view = null; this.error = ''; this.loading = false
      if (!this.runID) return
      const controller = new AbortController(); this.controller = controller; this.loading = true
      try {
        const query = new URLSearchParams({id:this.runID,period:this.adoptArchive ? '' : this.period,pattern:this.pattern,bias:this.bias,regime:this.regime,state:this.state,action:this.action,horizon:String(this.horizon),offset:String(this.offset),limit:'30'})
        const response = await fetch(`/api/pattern-validation?${query}`,{cache:'no-store',signal:controller.signal}), payload = await response.json()
        if (id !== this.requestID) return
        if (!response.ok) throw new Error(payload.error || '读取验证结果失败')
        if (payload.report?.run_id !== this.runID) throw new Error('归档与当前选择不一致，请重试')
        if (this.adoptArchive) {
          this.symbols = payload.report.request.symbols.join(', ')
          this.priceBasis = payload.report.request.price_basis || 'raw'
          this.validationStart = payload.report.request.validation_start || ''; this.splitEnabled = !!this.validationStart
          this.start = payload.report.request.start; this.end = payload.report.request.end; this.adoptArchive = false
        }
        this.period = payload.filter.period || ''
        this.view = payload; this.loadedKey = this.requestKey
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
        const response = await fetch('/api/pattern-validation',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({symbols,start:this.start,end:this.end,price_basis:this.priceBasis,validation_start:this.splitEnabled ? this.validationStart : undefined}),signal:controller.signal}), payload = await response.json()
        if (id !== this.requestID) return
        if (!response.ok) throw new Error(payload.error || '运行形态验证失败')
        this.pattern = ''; this.bias = ''; this.regime = ''; this.state = ''; this.action = ''; this.horizon = 5; this.offset = 0
        this.period = payload.filter.period || ''
        this.runID = payload.report.run_id; this.view = payload; this.loadedKey = this.requestKey
        await this.history()
      } catch(error) { if (id === this.requestID) this.error = error.name === 'AbortError' ? '验证已取消' : error.message || String(error) }
      finally { if (id === this.requestID) this.running = false }
    },
  },
  mounted() { this.history(); if (this.runID) this.load() },
  beforeUnmount() { this.requestID++; this.controller?.abort(); this.historyController?.abort() },
  template: `<section class="pattern-validation" aria-label="形态验证" :aria-busy="busy">
    <div class="pv-heading"><h2>形态验证</h2><span>历史重建 · {{ basisLabel(report ? report.request.price_basis : priceBasis) }} · 确认后价格表现</span></div>
    <div class="pv-run-modes" role="group" aria-label="验证方式"><button type="button" :aria-pressed="!batchMode" :disabled="running" @click="batchMode=false">单次验证</button><button type="button" :aria-pressed="batchMode" :disabled="running" @click="batchMode=true">批量验证</button></div>
    <pattern-batch-view v-if="batchMode" :initial="{start,end,price_basis:priceBasis,validation_start:splitEnabled ? validationStart : ''}" @open-run="openBatchRun"/>
    <form v-else class="pv-form" @submit.prevent="run">
      <label class="pv-symbols">股票池<input v-model="symbols" aria-label="验证股票池" placeholder="002080, 600519" required :disabled="running"></label>
      <label>开始日期<input type="date" v-model="start" aria-label="验证开始日期" required :max="end" :disabled="running"></label>
      <label>结束日期<input type="date" v-model="end" aria-label="验证结束日期" required :min="start" :max="today" :disabled="running"></label>
      <label>价格口径<select v-model="priceBasis" aria-label="验证价格口径" :disabled="running"><option value="raw">未复权</option><option value="hfq">后复权研究</option></select></label>
      <label class="pv-study-toggle"><input type="checkbox" v-model="splitEnabled" aria-label="分期评估" :disabled="running">分期评估</label>
      <label v-if="splitEnabled">验证期起点<input type="date" v-model="validationStart" aria-label="验证期起点" required :min="start" :max="end" :disabled="running"></label>
      <button type="submit" :disabled="busy"><play :size="14"/>{{ running ? '验证中' : '运行验证' }}</button>
      <button v-if="running" type="button" class="icon-button" title="取消验证" aria-label="取消验证" @click="cancel"><square :size="14"/></button>
    </form>
    <div class="pv-archive"><label>归档<select v-model="runID" @change="adoptArchive = !!runID; period = ''; offset = 0" aria-label="形态验证归档" :disabled="running"><option value="">本次验证</option><option v-for="run in runs" :key="run.run_id" :value="run.run_id">{{ time(run.generated_at) }} · {{ run.request.symbols.join(', ') }} · {{ run.samples }} 样本</option></select></label><button type="button" class="icon-button" title="刷新归档" aria-label="刷新验证归档" :disabled="running" @click="history"><refresh-cw :size="15"/></button><a v-if="runID && !busy" :href="exportURL" class="icon-button" title="导出完整验证归档" aria-label="导出完整验证归档"><download :size="15"/></a></div>
    <p v-if="error" class="chart-plan-error" role="alert">{{ error }}<button v-if="runID && !running" type="button" class="text-action" @click="load">重试读取</button></p>
    <div class="pv-filters"><div class="dashboard-segment" role="group" aria-label="形态验证窗口"><button v-for="days in [5,10,20]" :key="days" type="button" :aria-pressed="horizon === days" :class="{active:horizon === days}" :disabled="running" @click="horizon = days">{{ days }}日</button></div><select v-model="pattern" aria-label="筛选验证形态" :disabled="running"><option value="">全部形态</option><option v-for="item in patterns" :value="item[0]" :key="item[0]">{{ item[1] }}</option></select><select v-model="bias" aria-label="筛选验证方向" :disabled="running"><option value="">全部方向</option><option value="bullish">看涨</option><option value="bearish">看跌</option></select><select v-model="regime" aria-label="筛选市场阶段" :disabled="running"><option value="">全部市场阶段</option><option v-for="item in ['牛市','熊市','震荡','高波动','数据不足']" :key="item">{{ item }}</option></select><select v-model="state" aria-label="筛选样本状态" :disabled="running"><option value="">全部状态</option><option value="watching">待确认</option><option value="forming">突破待确认</option><option value="confirmed">已确认</option><option value="invalidated">已失效</option><option value="expired">观察到期</option></select></div>
    <p v-if="busy" class="pv-empty" role="status">{{ running ? '正在逐日验证并归档' : '正在读取验证结果' }}</p>
    <div class="pv-action-filter"><label>公司行动<select v-model="action" aria-label="筛选公司行动" :disabled="running"><option value="">全部核查状态</option><option value="clear">未发现支持范围内事件</option><option value="adjusted">复权已处理</option><option value="affected">事件隔离</option><option value="unverified">公司行动未核验</option><option value="legacy">旧口径未隔离</option></select></label><span v-if="view">{{ horizon }}日 · 事件隔离 {{ totals.action_affected || 0 }} · 未核验 {{ totals.action_unverified || 0 }}<template v-if="adjusted"> · 复权处理 {{ totals.action_adjusted || 0 }}</template></span></div>
    <template v-if="view">
      <p v-if="!report.action_coverage" class="pv-action-warning">旧归档未做公司行动隔离，统计沿用原口径。</p>
      <p v-if="adjusted && !numericQuality" class="pv-action-warning">旧归档未校验复权数值和送转股成交量，统计沿用原口径。</p>
      <section v-if="report.study" class="pv-study" aria-label="分期回溯评估">
        <div class="pv-section-title"><h3>分期回溯评估</h3><div class="dashboard-segment" role="group" aria-label="评估时段"><button v-for="key in ['research','validation']" :key="key" type="button" :aria-pressed="period === key" :class="{active:period === key}" :disabled="busy" @click="period = key">{{ periodLabel(key) }}</button></div></div>
        <p class="pv-study-basis">本次冻结 {{ time(report.study.frozen_at) }} · 历史回溯，非独立前向验证</p>
        <table class="pv-group-table pv-period-table"><thead><tr><th>时段</th><th>识别 / 确认</th><th>分界排除 / 缺失</th><th>配对 / 待成熟</th><th>平均超额</th><th>正超额比例</th><th>失效率</th></tr></thead><tbody><tr v-for="item in view.periods" :key="item.period" :data-study-period="item.period"><td data-label="时段"><strong>{{ periodLabel(item.period) }}</strong><small>{{ item.start }} 至 {{ item.end }}</small></td><td data-label="识别 / 确认">{{ item.totals.samples }} / {{ item.totals.confirmed }}</td><td data-label="分界排除 / 缺失">{{ item.totals.boundary_excluded }} / {{ item.totals.unavailable }}</td><td data-label="配对 / 待成熟">{{ item.totals.benchmark_paired }} / {{ item.totals.pending }}<small v-if="item.totals.benchmark_paired < 20">样本不足20</small></td><td data-label="平均超额" :class="tone(item.totals.average_excess_return)">{{ points(item.totals.average_excess_return) }}<small>方向 {{ points(item.totals.average_direction_excess_return) }}</small></td><td data-label="正超额比例">{{ percent(item.totals.positive_excess_rate) }}</td><td data-label="失效率">{{ percent(item.totals.invalidation_rate) }}</td></tr></tbody></table>
        <details class="pv-method pv-frozen-config"><summary>冻结配置</summary><p>股票池 {{ report.study.config.symbols.join('、') }} · {{ basisLabel(report.study.config.price_basis) }}</p><p>研究 {{ report.study.config.start }} 至 {{ report.study.config.research_end }} · 验证 {{ report.study.config.validation_start }} 至 {{ report.study.config.data_cutoff }}</p><p>{{ report.study.config.algorithm_version }} · {{ report.study.config.pattern_version }} · {{ report.study.config.patterns.length }} 种形态 · {{ report.study.config.horizons.join('/') }} 日窗口</p><p>预热 {{ report.study.config.warmup_days }} 天 · 识别回看 {{ report.study.config.lookback_bars }} 根 · 拐点半径 {{ report.study.config.pivot_radius }} 根 · 确认期限 {{ report.study.config.confirmation_bars }} 根</p><p>{{ report.study.config.price_method }} · {{ report.study.config.volume_method }} · {{ report.study.config.quality_method }} · {{ report.study.config.benchmark_method }}</p><p class="pv-hash">配置 {{ report.study.config_hash }}</p><p class="pv-hash">研究输入 {{ report.study.research.input_hash }}</p></details>
      </section>
      <div class="pv-basis"><span>{{ report.request.start }} 至 {{ report.request.end }} · 数据截止 {{ report.cutoff }} · {{ basisLabel(report.request.price_basis) }}</span><span>{{ report.version }} · {{ time(report.generated_at) }}</span></div>
      <div class="pv-totals"><div><span>{{ resultPriceLabel }}识别 / 确认</span><strong>{{ totals.samples }} / {{ totals.confirmed }}</strong><small>确认前失效 {{ totals.invalidated_before_confirmation }}</small></div><div><span>{{ horizon }}日纳入统计 / 待成熟</span><strong>{{ totals.mature }} / {{ totals.pending }}</strong><small>数据缺失 {{ totals.unavailable }}<template v-if="report.study"> · 分界排除 {{ totals.boundary_excluded }}</template></small></div><div><span>平均方向表现</span><strong :class="tone(totals.average_direction_return)">{{ percent(totals.average_direction_return) }}</strong><small>价格涨跌 {{ percent(totals.average_price_return) }}</small></div><div><span>窗口失效率</span><strong>{{ percent(totals.invalidation_rate) }}</strong><small>仅纳入统计的成熟窗口</small></div></div>
      <section class="pv-benchmark" aria-label="同期基准对照">
        <div class="pv-section-title"><h3>同期基准对照</h3><span>沪深300价格指数 · {{ horizon }}日窗口</span></div>
        <p v-if="!report.benchmark" class="pv-action-warning">旧归档未记录同期基准收益，未补算超额表现。</p>
        <div class="pv-totals"><div><span>配对 / 纳入统计</span><strong>{{ totals.benchmark_paired || 0 }} / {{ totals.mature }}</strong><small>未配对 {{ totals.benchmark_unpaired || 0 }}<template v-if="report.benchmark && totals.benchmark_paired < 20"> · 配对样本不足20</template></small></div><div><span>平均基准涨跌</span><strong :class="tone(totals.average_benchmark_return)">{{ percent(totals.average_benchmark_return) }}</strong><small>配对个股 {{ percent(totals.average_paired_price_return) }}</small></div><div><span>平均超额（百分点）</span><strong :class="tone(totals.average_excess_return)">{{ number(totals.average_excess_return) }}</strong><small>方向超额 {{ points(totals.average_direction_excess_return) }}</small></div><div><span>超额为正比例</span><strong>{{ percent(totals.positive_excess_rate) }}</strong><small>{{ totals.positive_excess || 0 }} / {{ totals.benchmark_paired || 0 }} · 方向为正 {{ percent(totals.positive_direction_excess_rate) }}</small></div></div>
      </section>
      <div class="pv-section-title"><h3>形态与市场阶段</h3><span>{{ view.groups.length }} 组 · {{ horizon }}日窗口</span></div>
      <table class="pv-group-table" v-if="view.groups.length"><thead><tr><th>形态 / 方向 / 市场</th><th>识别 / 确认</th><th>纳入 / 待成熟 / 缺失</th><th>价格涨跌</th><th>方向表现</th><th>有利 / 不利波动</th><th>沪深300 / 超额</th><th>失效率</th></tr></thead><tbody><tr v-for="group in view.groups" :key="group.pattern_id + group.regime"><td data-label="形态"><strong>{{ group.name }}</strong><small>{{ biasLabel(group.bias) }} · {{ group.regime }}</small></td><td data-label="识别 / 确认">{{ group.totals.samples }} / {{ group.totals.confirmed }}</td><td data-label="纳入 / 待成熟 / 缺失">{{ group.totals.mature }} / {{ group.totals.pending }} / {{ group.totals.unavailable }}<small v-if="group.totals.mature < 20">样本不足20</small></td><td data-label="价格涨跌" :class="tone(group.totals.average_price_return)">{{ percent(group.totals.average_price_return) }}</td><td data-label="方向表现" :class="tone(group.totals.average_direction_return)">{{ percent(group.totals.average_direction_return) }}</td><td data-label="有利 / 不利">{{ percent(group.totals.average_favorable) }} / {{ percent(group.totals.average_adverse) }}</td><td data-label="沪深300 / 超额"><span>{{ percent(group.totals.average_benchmark_return) }}</span><small :class="tone(group.totals.average_excess_return)">超额 {{ points(group.totals.average_excess_return) }}</small><small>配对 {{ group.totals.benchmark_paired || 0 }} · 正超额 {{ percent(group.totals.positive_excess_rate) }}</small></td><td data-label="失效率">{{ percent(group.totals.invalidation_rate) }}</td></tr></tbody></table>
      <p v-else class="pv-empty">当前条件下没有样本</p>
      <section v-if="report.pool" class="pv-pool-distribution" aria-label="股票池样本分布"><div class="pv-section-title"><h3>{{ report.pool.name }} · 样本分布</h3><span>{{ report.pool.members.length }}只 · {{ report.pool.version.slice(0,8) }}</span></div><p class="pv-study-basis">{{ report.pool.source }} · 冻结 {{ time(report.pool.created_at) }}</p><p v-if="view.stocks?.length" class="pv-study-basis">最高单股样本占比 {{ percent(view.stocks[0].sample_share) }} · {{ view.stocks[0].symbol }}</p><table class="pv-group-table"><thead><tr><th>行业标签</th><th>股票数</th><th>样本 / 占比</th><th>配对 / 平均超额</th></tr></thead><tbody><tr v-for="item in view.industries" :key="item.industry"><td data-label="行业标签">{{ item.industry }}</td><td data-label="股票数">{{ item.stocks }}</td><td data-label="样本 / 占比">{{ item.totals.samples }} / {{ percent(item.sample_share) }}</td><td data-label="配对 / 平均超额">{{ item.totals.benchmark_paired }} / {{ points(item.totals.average_excess_return) }}</td></tr></tbody></table><details><summary>逐股票分布 · {{ view.stocks?.length || 0 }}只</summary><table class="pv-group-table"><thead><tr><th>股票 / 标签</th><th>样本 / 占比</th><th>配对 / 平均超额</th><th>缺失 / 隔离</th></tr></thead><tbody><tr v-for="item in view.stocks" :key="item.symbol" :data-distribution-symbol="item.symbol"><td data-label="股票"><strong>{{ item.symbol }} {{ item.name }}</strong><small>{{ item.industry }}</small></td><td data-label="样本 / 占比">{{ item.totals.samples }} / {{ percent(item.sample_share) }}</td><td data-label="配对 / 平均超额">{{ item.totals.benchmark_paired }} / {{ points(item.totals.average_excess_return) }}</td><td data-label="缺失 / 隔离">{{ item.totals.unavailable }} / {{ item.totals.action_affected + item.totals.action_unverified }}</td></tr></tbody></table></details></section>
      <div class="pv-section-title"><h3>样本明细</h3><span>{{ view.total }} 条</span></div>
      <details class="pv-sample" v-for="sample in view.samples" :key="sample.id" :data-sample-id="sample.id"><summary><strong>{{ sample.symbol }} · {{ sample.name }}</strong><span>{{ sample.observed_on }}</span><span>{{ status(sample.state) }}</span><span>{{ status(outcome(sample,horizon).state) }} · {{ actionStatus(outcome(sample,horizon).action_check) }}</span><b :class="tone(outcome(sample,horizon).direction_return)">{{ resultPriceLabel }} {{ percent(outcome(sample,horizon).direction_return) }}</b></summary>
        <p v-if="adjusted" class="pv-action-warning">后复权研究点位 · 个股图表显示未复权价格</p>
        <ol class="pv-timeline"><li><span>首次识别</span><button type="button" :aria-label="'查看识别日 ' + sample.id" @click="open(sample,sample.observed_on)">{{ sample.observed_on }}<arrow-up-right :size="13"/></button></li><li><span>收盘确认</span><button v-if="sample.confirmed_on" type="button" :aria-label="'查看确认日 ' + sample.id" @click="open(sample,sample.confirmed_on)">{{ sample.confirmed_on }}<arrow-up-right :size="13"/></button><b v-else>--</b></li><li><span>失效 / 到期</span><b>{{ sample.invalidated_on || sample.expired_on || '--' }}</b></li><li><span>确认收盘 / 市场</span><b>{{ number(sample.confirmation_close) }} · {{ sample.regime }}</b></li></ol>
        <div class="pv-sample-levels"><span>冻结确认价 {{ number(sample.snapshot.pattern.trigger_price) }}</span><span>冻结失效位 {{ number(sample.snapshot.pattern.invalidation_price) }}</span><span>市场分类日 {{ sample.regime_date }}</span></div>
        <div v-if="sample.action_check" class="pv-action-check"><strong>识别与确认核查 · {{ actionStatus(sample.action_check) }}</strong><p>{{ sample.action_check.start }} 至 {{ sample.action_check.end }} · {{ sample.action_check.detail }}</p></div>
        <div class="pv-outcome" v-for="result in sample.outcomes" :key="result.horizon"><strong>{{ result.horizon }}日 · {{ status(result.state) }}</strong><span>{{ resultPriceLabel }}价格 {{ percent(result.price_return) }} · 方向 {{ percent(result.direction_return) }}</span><span>有利 {{ percent(result.favorable) }} · 不利 {{ percent(result.adverse) }}</span><small>{{ result.detail || (result.through ? '截至 ' + result.through + ' · ' + (result.invalidated ? '窗口内触及失效位' : '窗口内未触及失效位') : '') }}</small><p v-if="result.period_check" class="pv-period-check" :data-period-state="result.period_check.state">{{ periodLabel(sample.period) }} · {{ result.period_check.state === 'excluded' ? '分界未完成（已排除）' : '时段内' }} · 截止 {{ result.period_check.cutoff }}<br>{{ result.period_check.detail }}</p><div class="pv-benchmark-outcome" :data-benchmark-state="result.benchmark?.state || 'legacy'"><strong>沪深300 · {{ benchmarkStatus(result.benchmark) }}</strong><span>基准 {{ percent(result.benchmark?.return) }}</span><span :class="tone(result.benchmark?.excess_return)">超额 {{ points(result.benchmark?.excess_return) }}</span><span :class="tone(result.benchmark?.direction_excess_return)">方向超额 {{ points(result.benchmark?.direction_excess_return) }}</span><small v-if="result.benchmark?.start && result.benchmark?.end">{{ result.benchmark.start }} 至 {{ result.benchmark.end }}</small><small v-if="result.benchmark?.detail">{{ result.benchmark.detail }}</small></div><div v-if="result.action_check" class="pv-action-check" :data-action-state="result.action_check.state"><strong>{{ actionStatus(result.action_check) }}</strong><p>{{ result.action_check.start }} 至 {{ result.action_check.end }} · {{ result.action_check.detail }}</p><p v-for="event in result.action_check.events" :key="event.id">{{ event.ex_date }} · {{ actionKinds(event.kinds) }} · {{ event.description }}</p></div></div>
        <details class="pv-evidence"><summary>识别日冻结依据</summary><p v-for="evidence in sample.snapshot.evidence" :key="evidence">{{ evidence }}</p><p class="pv-hash">快照 {{ sample.snapshot_fingerprint }}</p></details>
      </details>
      <div class="pv-pagination" v-if="view.total > 30"><button class="icon-button" type="button" title="上一页" aria-label="上一页样本" :disabled="busy || offset === 0" @click="offset = Math.max(0,offset-30)"><chevron-left :size="16"/></button><span>{{ Math.floor(offset/30)+1 }} / {{ Math.ceil(view.total/30) }}</span><button class="icon-button" type="button" title="下一页" aria-label="下一页样本" :disabled="busy || offset+30 >= view.total" @click="offset += 30"><chevron-right :size="16"/></button></div>
      <details class="pv-coverage" open><summary>数据覆盖</summary><div v-for="item in report.coverage" :key="item.symbol" :data-coverage-symbol="item.symbol">
        <strong>{{ item.symbol === 'sh000300' ? '沪深300基准' : item.symbol }}<small>{{ coverageStatus(item) }}</small></strong>
        <span>{{ item.first_date || '--' }} 至 {{ item.last_date || '--' }} · {{ item.bars }} 根<small v-if="item.warmup_required">起始预热 {{ item.warmup_bars }} / {{ item.warmup_required }} 根 · 基准对齐 {{ item.expected_days == null ? '--' : item.available_days + ' / ' + item.expected_days }} 日</small></span>
        <small>{{ item.error || item.source }}{{ item.cached ? ' · 缓存参考' : '' }}{{ item.late_discoveries ? ' · 非首次识别日排除 ' + item.late_discoveries : '' }}</small>
        <div class="pv-coverage-detail" v-if="item.fetch_start || item.notes?.length"><p v-if="item.fetch_start">{{ item.fetch_mode === 'range' ? '区间取数' : '近期快照' }} · {{ item.fetch_start }} 至 {{ item.fetch_end }}</p><p v-if="item.reference_start">基准参照 {{ item.reference_start }} 至 {{ item.reference_end }}</p><p v-for="note in item.notes" :key="note">{{ note }}</p></div>
        <details v-if="item.missing_dates?.length" class="pv-gaps"><summary>缺失日期 {{ item.missing_dates.length }} 日 · 原因未确认</summary><p>{{ item.missing_dates.join('、') }}</p></details>
      </div></details>
      <details v-if="report.action_coverage" class="pv-actions" open><summary>分红送转记录</summary><div v-for="item in report.action_coverage" :key="item.symbol" :data-actions-symbol="item.symbol"><div class="pv-section-title"><strong>{{ item.symbol }} · {{ item.status === 'checked' ? '已查询' : '未核验' }}</strong><a v-if="sourceURL(item.source_url)" :href="sourceURL(item.source_url)" target="_blank" rel="noopener noreferrer">{{ item.source }}<arrow-up-right :size="12"/></a></div><p>{{ item.start || '--' }} 至 {{ item.end || '--' }} · {{ item.events.length }} 条记录<span v-if="item.fetched_at && !item.fetched_at.startsWith('0001')"> · 采集 {{ time(item.fetched_at) }}</span></p><p v-if="item.error" class="pv-action-warning">{{ item.error }}</p><p>供应商实施分配记录 · 仅现金分红、送股、转增 · 未逐条核验官方披露</p><ol><li v-for="event in item.events" :key="event.id"><strong>{{ event.ex_date }} · {{ actionKinds(event.kinds) }}</strong><span>{{ event.description }}</span><small v-if="event.terms">每10股：派 {{ qualityNumber(event.terms.cash_per_10) }} 元 · 送 {{ qualityNumber(event.terms.bonus_per_10) }} 股 · 转 {{ qualityNumber(event.terms.capitalization_per_10) }} 股</small><small>公告 {{ event.notice_date || '--' }} · 登记 {{ event.record_date || '--' }}</small></li></ol></div></details>
      <details v-if="report.adjustments?.length" class="pv-actions pv-adjustments" open><summary>复权因子与质量</summary><div v-for="item in report.adjustments" :key="item.symbol" :data-adjustment-symbol="item.symbol"><div class="pv-section-title"><strong>{{ item.symbol }} · {{ item.status === 'applied' ? '已应用' : '不可用' }}<template v-if="item.quality_status"> · {{ qualityStatus(item.quality_status) }}</template></strong><a v-if="sourceURL(item.source_url)" :href="sourceURL(item.source_url)" target="_blank" rel="noopener noreferrer">{{ item.source }}<arrow-up-right :size="12"/></a></div><p>{{ item.start || '--' }} 至 {{ item.end }} · {{ item.factor_count }} 个有效因子</p><p>归一基准 {{ item.anchor_date || '--' }} · 因子 {{ item.anchor_factor || '--' }}</p><p v-if="item.volume_method">成交量按校验通过的累计送转股比例折算 · 成交额和换手为原始口径</p><p v-if="item.error" class="pv-action-warning">{{ item.error }}</p><p v-if="item.fetched_at && !item.fetched_at.startsWith('0001')">采集 {{ time(item.fetched_at) }}</p><ol class="pv-quality-list"><li v-for="check in item.quality" :key="check.date" :data-quality-state="check.state"><strong>{{ check.date }} · {{ qualityStatus(check.state) }}</strong><p>{{ check.detail }}</p><p v-if="check.previous_close != null">前收 {{ check.previous_date }} · {{ qualityNumber(check.previous_close) }} 元</p><p v-if="check.expected_ex_price != null">理论除权 {{ qualityNumber(check.expected_ex_price) }} · 因子隐含 {{ qualityNumber(check.implied_ex_price) }} 元</p><p v-if="check.price_error != null">偏差 {{ qualityNumber(check.price_error) }} 元 · 容差 {{ number(check.tolerance) }} 元</p><p v-if="check.share_ratio != null">股数比例 {{ qualityNumber(check.share_ratio) }} · 因子变动 {{ qualityNumber(check.observed_ratio) }}</p></li></ol><p class="pv-hash">价格 {{ item.method }} · {{ item.factor_version || '--' }}</p><p v-if="item.volume_method" class="pv-hash">成交量 {{ item.volume_method }} · {{ item.volume_version || '--' }}</p><p v-if="item.quality_method">{{ item.quality_method }} · 供应商交叉校验，未逐条核验官方披露</p></div></details>
      <details class="pv-method"><summary>统计口径与限制</summary><p v-for="warning in report.warnings" :key="warning">{{ warning }}</p><p class="pv-hash">输入指纹 {{ report.input_hash }}</p></details>
    </template>
    <p v-else-if="!busy && !error" class="pv-empty">暂无验证结果</p>
  </section>`,
}
