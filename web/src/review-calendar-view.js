import { CalendarDays, ChevronLeft, ChevronRight, RefreshCw, ArrowUpRight } from 'lucide-vue-next'
import { shanghaiToday, validCalendarMonth, shiftCalendarMonth, calendarCells, calendarValue, compactCalendarValue } from './review-calendar.mjs'

const eventNames = { plan: '创建计划', entry: '入场', exit: '退出', review: '复盘更新' }

export const ReviewCalendarView = {
  components: { CalendarDays, ChevronLeft, ChevronRight, RefreshCw, ArrowUpRight },
  props: ['initialMonth', 'refreshKey', 'selection'],
  emits: ['open-plan', 'open-shadow', 'selection'],
  data() {
    return { month: validCalendarMonth(this.selection?.month) ? this.selection.month : validCalendarMonth(this.initialMonth) ? this.initialMonth : shanghaiToday().slice(0, 7), source: this.selection?.source || 'manual', profile: this.selection?.profile || 'balanced', profiles: [], report: null,
      selected: this.selection?.selected || '', kind: 'all', query: '', shown: 50, undatedShown: 20, loading: false, error: '', controller: null, requestID: 0 }
  },
  computed: {
    requestKey() { return `${this.month}|${this.source}|${this.profile}|${this.refreshKey}` },
    selectionKey() { return `${this.month}|${this.source}|${this.profile}|${this.selected}` },
    cells() { return calendarCells(this.month) },
    dayMap() { return Object.fromEntries((this.report?.days || []).map(day => [day.date, day])) },
    selectedDay() { return this.dayMap[this.selected] || { events: [], totals: {} } },
    selectedEvents() {
      const query = this.query.trim().toLowerCase()
      return this.selectedDay.events.filter(item => (this.kind === 'all' || item.kind === this.kind) && (!query || `${item.symbol} ${item.name} ${item.label} ${(item.tags || []).join(' ')}`.toLowerCase().includes(query)))
    },
    totals() { return this.report?.totals || {} },
    unit() { return this.source === 'manual' ? '费用前平均 R' : '已平仓净盈亏（元）' },
    monthlyValue() { return calendarValue(this.totals, this.source) },
  },
  watch: {
    requestKey: { immediate: true, handler() { this.load() } },
    selectionKey() { this.$emit('selection', {month:this.month, source:this.source, profile:this.profile, selected:this.selected}) },
    selected() { this.shown = 50 },
    kind() { this.shown = 50 },
    query() { this.shown = 50 },
  },
  methods: {
    compactCalendarValue,
    eventName(kind) { return eventNames[kind] || kind },
    value(date) { return calendarValue(this.dayMap[date]?.totals, this.source) },
    tone(value) { return value == null ? '' : value > 0 ? 'gain' : value < 0 ? 'loss' : 'flat' },
    number(value) { return value == null ? '--' : Number(value).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) },
    time(item) { return item.at ? new Date(item.at).toLocaleTimeString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false }) : '仅记录日期' },
    status(item) { return { followed: '按计划执行', deviated: '偏离计划', watching: '观察中', skipped: '主动放弃', not_traded: '未交易' }[item.execution_status] || '' },
    discipline(item) { return { followed: '纪律遵守', partial: '部分遵守', deviated: '纪律偏差', not_applicable: '不适用' }[item.discipline] || '' },
    label(cell) { return `${cell.date}，${this.dayMap[cell.date]?.events.length || 0}条记录，${this.unit} ${this.number(this.value(cell.date))}` },
    navigate(delta) { this.month = shiftCalendarMonth(this.month, delta) },
    currentMonth() { this.month = shanghaiToday().slice(0, 7); this.selected = shanghaiToday() },
    chooseMonth(event) { if (validCalendarMonth(event.target.value)) this.month = event.target.value; else event.target.value = this.month },
    open(item) { this.$emit(this.source === 'manual' ? 'open-plan' : 'open-shadow', { ...item, profile: this.profile }) },
    async key(event) {
      if (!event.target.dataset.date) return
      const movement = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }[event.key]
      if (!movement) return
      event.preventDefault()
      const date = new Date(event.target.dataset.date + 'T12:00:00Z')
      date.setUTCDate(date.getUTCDate() + movement)
      const value = date.toISOString().slice(0, 10)
      if (!validCalendarMonth(value.slice(0, 7))) return
      this.selected = value
      this.month = value.slice(0, 7)
      await this.$nextTick()
      this.$refs.grid?.querySelector(`[data-date="${value}"]`)?.focus()
    },
    async load() {
      this.controller?.abort()
      const controller = new AbortController()
      this.controller = controller
      const id = ++this.requestID
      this.loading = true; this.error = ''; this.report = null; this.undatedShown = 20
      try {
        const params = new URLSearchParams({ month: this.month, source: this.source })
        if (this.source === 'shadow') params.set('profile', this.profile)
        const response = await fetch(`/api/review-calendar?${params}`, { cache: 'no-store', signal: controller.signal })
        const payload = await response.json()
        if (id !== this.requestID) return
        if (!response.ok) throw new Error(payload.error || '复盘日历读取失败')
        this.report = payload.report
        this.profiles = payload.profiles || []
        if (!this.selected.startsWith(this.month)) {
          const today = shanghaiToday()
          this.selected = today.startsWith(this.month) ? today : (this.report.days.find(day => day.events.length)?.date || this.month + '-01')
        }
      } catch (error) {
        if (id === this.requestID && error.name !== 'AbortError') this.error = error.message || String(error)
      } finally { if (id === this.requestID) this.loading = false }
    },
  },
  beforeUnmount() { this.requestID++; this.controller?.abort() },
  template: `<section class="review-calendar" aria-label="复盘日历" :aria-busy="loading">
    <div class="calendar-toolbar"><div class="calendar-title"><calendar-days :size="19"/><h2>复盘日历</h2></div><div class="calendar-navigation"><button type="button" class="icon-button" title="上个月" aria-label="上个月" :disabled="month === '1900-01'" @click="navigate(-1)"><chevron-left :size="16"/></button><input type="month" :value="month" min="1900-01" max="2200-12" @change="chooseMonth" aria-label="复盘月份"><button type="button" class="icon-button" title="下个月" aria-label="下个月" :disabled="month === '2200-12'" @click="navigate(1)"><chevron-right :size="16"/></button><button type="button" class="icon-button" title="回到本月" aria-label="回到本月" @click="currentMonth"><calendar-days :size="16"/></button><button type="button" class="icon-button" title="刷新日历" aria-label="刷新日历" :disabled="loading" @click="load"><refresh-cw :size="16"/></button></div></div>
    <div class="calendar-source"><div class="dashboard-segment" role="group" aria-label="日历数据来源"><button type="button" :aria-pressed="source === 'manual'" @click="source = 'manual'; kind = 'all'">人工复盘</button><button type="button" :aria-pressed="source === 'shadow'" @click="source = 'shadow'; kind = 'all'">影子账户</button></div><select v-if="source === 'shadow'" v-model="profile" aria-label="日历影子账户"><option v-for="item in profiles" :key="item.id" :value="item.id">{{ item.name }}</option></select><span>{{ source === 'manual' ? '按实际退出日归属 · 费用前 R' : '按平仓日归属 · 含已记录费用' }}</span><small v-if="report?.as_of">账本截至 {{ report.as_of }}</small></div>
    <p class="chart-plan-error" role="alert" v-if="error">{{ error }}</p>
    <div class="calendar-kpis"><div><span>{{ source === 'manual' ? '本月新建计划' : '本月买入委托' }}</span><strong>{{ report ? (source === 'manual' ? totals.plans : totals.entries) : '--' }}</strong></div><div><span>{{ source === 'manual' ? '本月完成交易' : '本月平仓批次' }}</span><strong>{{ report ? totals.exits : '--' }}</strong></div><div><span>{{ unit }}</span><strong :class="tone(monthlyValue)">{{ number(monthlyValue) }}{{ source === 'manual' && monthlyValue != null ? 'R' : '' }}</strong></div><div><span>{{ source === 'manual' ? '本月复盘 / 偏差' : '档案交易日' }}</span><strong>{{ source === 'manual' ? (report ? totals.reviews + ' / ' + totals.deviations : '--') : (report?.as_of || '--') }}</strong></div></div>
    <div class="calendar-layout"><div class="calendar-month"><div class="calendar-weekdays"><span v-for="day in ['一','二','三','四','五','六','日']" :key="day">周{{ day }}</span></div><div ref="grid" class="calendar-grid" role="group" aria-label="月度复盘日期" @keydown="key"><template v-for="cell in cells" :key="cell.key"><button v-if="cell.date" type="button" :data-date="cell.date" :aria-label="label(cell)" :aria-pressed="selected === cell.date" :class="[tone(value(cell.date)), {selected:selected === cell.date, populated:dayMap[cell.date]?.events.length}]" @click="selected = cell.date"><time :datetime="cell.date">{{ cell.day }}</time><strong>{{ dayMap[cell.date]?.events.length ? compactCalendarValue(value(cell.date)) : '' }}</strong><small>{{ dayMap[cell.date]?.events.length ? dayMap[cell.date].events.length + '项' : '' }}</small></button><span v-else class="calendar-padding" aria-hidden="true"></span></template></div><div class="calendar-legend"><span>{{ unit }}</span><span>{{ loading ? '正在读取归档' : '上海时区' }}</span></div></div>
    <section class="calendar-day-detail" aria-label="当日复盘明细"><div class="calendar-day-heading"><h3>{{ selected || month }}</h3><span>{{ selectedDay.events.length }} 条记录</span></div><div class="calendar-day-filters"><select v-model="kind" aria-label="当日记录类型"><option value="all">全部记录</option><option v-if="source === 'manual'" value="plan">创建计划</option><option value="entry">入场</option><option value="exit">退出</option><option v-if="source === 'manual'" value="review">复盘更新</option></select><input v-model.trim="query" type="search" aria-label="筛选当日股票" placeholder="股票、形态、标签"></div><p v-if="!selectedEvents.length" class="calendar-empty">{{ loading ? '正在读取' : '当天没有符合条件的记录' }}</p><ol class="calendar-events"><li v-for="item in selectedEvents.slice(0, shown)" :key="item.id"><div class="calendar-event-title"><span>{{ eventName(item.kind) }}</span><time>{{ time(item) }}</time><button type="button" class="icon-button" :title="source === 'manual' ? '打开原计划与复盘' : '打开影子账本'" :aria-label="'打开记录 ' + item.id" @click="open(item)"><arrow-up-right :size="15"/></button></div><strong>{{ item.name || item.symbol }} <small>{{ item.symbol }}</small></strong><p>{{ item.label }}<span v-if="item.quantity"> · {{ item.quantity }}股</span><span v-if="item.price != null"> · {{ number(item.price) }}元</span></p><b v-if="item.r != null" :class="tone(item.r)">{{ number(item.r) }}R</b><b v-if="item.net_profit != null" :class="tone(item.net_profit)">{{ number(item.net_profit) }}元</b><small v-if="status(item) || discipline(item)">{{ status(item) }} · {{ discipline(item) }}</small><p v-if="item.note">{{ item.note }}</p><div v-if="item.tags?.length" class="calendar-tags"><span v-for="tag in item.tags" :key="tag">{{ tag }}</span></div></li></ol><button v-if="selectedEvents.length > shown" type="button" class="calendar-more" @click="shown += 50">加载更多</button></section></div>
    <details class="calendar-undated" v-if="report?.undated?.length"><summary>待补日期 · {{ report.undated.length }} 条（全部月份）</summary><ul><li v-for="item in report.undated.slice(0, undatedShown)" :key="item.id"><span>{{ item.name || item.symbol }} · {{ eventName(item.kind) }} · {{ item.note || '日期未记录' }}</span><button type="button" class="icon-button" title="打开原计划补充复盘" :aria-label="'补充日期 ' + item.id" @click="open(item)"><arrow-up-right :size="15"/></button></li></ul><button v-if="report.undated.length > undatedShown" type="button" class="calendar-more" @click="undatedShown += 20">加载更多</button></details>
    <div class="calendar-warnings" v-if="report?.warnings?.length"><p v-for="warning in report.warnings" :key="warning">{{ warning }}</p></div>
  </section>`,
}
