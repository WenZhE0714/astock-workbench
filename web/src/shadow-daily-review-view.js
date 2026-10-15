import { CalendarDays, ChevronLeft, ChevronRight, RefreshCw, Download, ArrowUpRight, ChevronDown } from 'lucide-vue-next'

export const ShadowDailyReviewView = {
  components: { CalendarDays, ChevronLeft, ChevronRight, RefreshCw, Download, ArrowUpRight, ChevronDown },
  props: ['profiles', 'selectedProfile', 'refreshKey', 'selection'],
  emits: ['open-profile', 'selection'],
  data() { return { date: this.selection?.date || '', today: '', dates: [], report: null, loading: false, error: '', expandedID: this.selection?.expandedID || '', controller: null, requestID: 0 } },
  computed: {
    profileKey() { return (this.profiles || []).map(item => item.id).join('|') },
    requestKey() { return `${this.date}|${this.profileKey}|${this.refreshKey}` },
    rows() { const ids = new Set((this.profiles || []).map(item => item.id)); return (this.report?.rows || []).filter(row => ids.has(row.id)) },
    selected() { return this.rows.find(row => row.id === (this.expandedID || this.selectedProfile)) || this.rows[0] },
    downloadURL() { return this.report ? `/api/strategy/shadow-daily?${new URLSearchParams({date: this.report.date, download: '1'})}` : '' },
    dateIndex() { return this.dates.indexOf(this.date) },
    runtimeTask() { return this.report?.date === this.today ? this.report?.runtime?.task_states?.shadow : null },
  },
  watch: {
    requestKey: { immediate: true, handler() { this.load() } },
    selectedProfile() { this.expandedID = '' },
    profileKey() { this.expandedID = '' },
    date() { this.$emit('selection', {date: this.date, expandedID: this.expandedID}) },
    expandedID() { this.$emit('selection', {date: this.date, expandedID: this.expandedID}) },
  },
  beforeUnmount() { this.requestID++; this.controller?.abort() },
  methods: {
    number(value, decimals = 0) { return value == null || !Number.isFinite(Number(value)) ? '--' : Number(value).toLocaleString('zh-CN', {minimumFractionDigits: decimals, maximumFractionDigits: decimals}) },
    money(value) { return this.number(value, 2) },
    tone(value) { return value == null ? '' : value > 0 ? 'sd-gain' : value < 0 ? 'sd-loss' : '' },
    time(value) { return value ? new Date(value).toLocaleString('zh-CN', {timeZone: 'Asia/Shanghai', hour12: false}) : '--' },
    checkStatus(value) { return {passed: '通过', pending: '待完成', warning: '需关注', failed: '未通过'}[value] || '--' },
    count(row, key) { return this.report?.source.available ? this.number(row.scans[key]) : '--' },
    step(delta) { const next = this.dates[this.dateIndex + delta]; if (next) this.date = next },
    basis(row) { return {'archived-daily': '当日留存规则', 'current-ledger': '当前账本规则', 'current-rule-recheck': '按当前规则复核历史信号'}[row.config_basis] || '' },
    snapshotState(row) { return !row.snapshot ? '当日权益快照未留存' : row.snapshot.revision !== row.ledger_revision ? '快照早于账本校正' : row.snapshot.ledger_issues?.length ? '快照校验未通过' : !row.snapshot.fresh ? '持仓估值日期不足' : row.snapshot.sealed ? '收盘快照已封存' : '盘中快照' },
    snapshotReturn(row) { return row.snapshot?.fresh && row.snapshot.revision === row.ledger_revision && !row.snapshot.ledger_issues?.length ? this.number(row.snapshot.return_percent, 2) + '%' : '--' },
    status(row) {
      if (row.ledger_issues?.length) return '账本待核对'
      if (!row.ledger) return '账本未覆盖'
      if (row.ledger.order_count) return '已有成交'
      if (!this.report?.source.available) return '扫描档案缺失'
      if (!row.scans.scans) return '暂无盘中扫描'
      return row.scans.eligible ? '有候选未成交' : '资格未通过'
    },
    async load() {
      if (!this.profileKey) return
      this.controller?.abort()
      const controller = new AbortController()
      this.controller = controller
      const id = ++this.requestID
      const date = this.date
      this.loading = true; this.error = ''
      if (this.report?.date !== date) this.report = null
      try {
        const params = new URLSearchParams()
        if (date) params.set('date', date)
        const response = await fetch(`/api/strategy/shadow-daily?${params}`, {cache: 'no-store', signal: controller.signal})
        const data = await response.json()
        if (id !== this.requestID) return
        if (!response.ok) throw new Error(data.error || '每日复盘读取失败')
        this.report = data; this.today = data.today; this.dates = data.dates || []
        if (!this.date) this.date = data.date
      } catch (error) {
        if (id === this.requestID && error.name !== 'AbortError') this.error = error.message
      } finally { if (id === this.requestID) this.loading = false }
    },
  },
  template: `<section class="shadow-daily-comparison" aria-label="每日对照复盘" :aria-busy="loading">
    <div class="sd-toolbar"><h3><calendar-days :size="17"/>每日对照复盘</h3><div class="sd-date-controls">
      <button type="button" class="icon-button" title="上一采集日" aria-label="上一采集日" :disabled="dateIndex < 0 || dateIndex >= dates.length-1" @click="step(1)"><chevron-left :size="16"/></button>
      <input type="date" v-model="date" :max="today || undefined" aria-label="影子复盘日期">
      <button type="button" class="icon-button" title="下一采集日" aria-label="下一采集日" :disabled="dateIndex <= 0" @click="step(-1)"><chevron-right :size="16"/></button>
      <button type="button" class="icon-button" title="刷新每日复盘" aria-label="刷新每日复盘" :disabled="loading" @click="load"><refresh-cw :size="16"/></button>
      <a v-if="downloadURL" class="icon-button" :href="downloadURL" title="导出当日对照报告" aria-label="导出当日对照报告"><download :size="16"/></a>
    </div></div>
    <p v-if="error" class="sd-warning" role="alert">{{ error }}</p>
    <p v-if="loading && !report" class="sd-empty">正在读取当日档案</p>
    <template v-if="report">
      <div class="sd-source"><span>{{ report.date }} · {{ rows.length }}个账户</span><span>原始扫描记录 {{ report.source.available ? number(report.source.records) : '--' }}</span><span v-if="runtimeTask">当前调度：{{ runtimeTask.detail }}</span><span v-if="loading">更新中</span></div>
      <div v-if="report.check" class="sd-check" aria-label="每日自动检查" :data-check-status="report.check.status">
        <div class="sd-check-heading"><strong>每日检查：{{ checkStatus(report.check.status) }}</strong><span>账户验收：{{ checkStatus(report.check.account_status) }}</span><span>全部账户封存 {{ report.check.sealed_accounts }} / {{ report.check.accounts }}</span><span>有成交账户 {{ report.check.traded_accounts }}</span><span v-if="report.check.not_started">未到起点 {{ report.check.not_started }}</span><small>{{ time(report.check.checked_at) }}</small></div>
        <ul v-if="report.check.issues.length"><li v-for="(issue, index) in report.check.issues" :key="index" :data-check-code="issue.code">{{ issue.message }}</li></ul>
      </div>
      <div class="sd-warnings" v-if="report.warnings.length || report.archive_error"><p v-for="warning in report.warnings" :key="warning">{{ warning }}</p><p v-if="report.archive_error">归档异常：{{ report.archive_error }}</p></div>
      <div class="sd-table" role="table" aria-label="每日账户对照">
        <div class="sd-row sd-heading" role="row"><span>账户</span><span>扫描 / 信号观测</span><span>资格通过 / 证券</span><span>成交笔数</span><span>费用（元）</span><span>已实现净盈亏（元）</span><span>较基线已实现差额</span><span></span></div>
        <div v-for="row in rows" :key="row.id" class="sd-row" :class="{selected: selected?.id === row.id}" role="row" :data-daily-profile="row.id">
          <div class="sd-identity"><strong>{{ row.name }}</strong><small>{{ status(row) }}</small></div>
          <div><small class="sd-mobile-label">扫描 / 信号观测</small><b>{{ count(row, 'scans') }} / {{ count(row, 'observations') }}</b></div>
          <div><small class="sd-mobile-label">资格通过 / 证券</small><b>{{ count(row, 'eligible') }} / {{ count(row, 'eligible_symbols') }}</b></div>
          <div><small class="sd-mobile-label">成交笔数</small><b>{{ number(row.ledger?.order_count) }}</b></div>
          <div><small class="sd-mobile-label">费用（元）</small><b>{{ money(row.ledger?.fees) }}</b></div>
          <div><small class="sd-mobile-label">已实现净盈亏（元）</small><b :class="tone(row.ledger?.realized_profit)">{{ money(row.ledger?.realized_profit) }}</b></div>
          <div><small class="sd-mobile-label">较基线已实现差额</small><b :class="tone(row.realized_difference)">{{ money(row.realized_difference) }}</b></div>
          <button type="button" class="icon-button" :aria-label="'查看' + row.name + '每日明细'" :title="'查看' + row.name + '每日明细'" :aria-expanded="selected?.id === row.id" @click="expandedID = row.id"><chevron-down :size="15"/></button>
        </div>
      </div>
      <div v-if="selected" class="sd-detail" :data-daily-selected="selected.id">
        <div class="sd-detail-heading"><h4>{{ selected.name }} · {{ report.date }}</h4><button class="icon-button" type="button" title="打开当前账本" aria-label="打开当前账本" @click="$emit('open-profile', selected.id)"><arrow-up-right :size="15"/></button></div>
        <p class="sd-zero-reason" v-if="selected.zero_trade_reason" data-zero-trade-reason>{{ selected.zero_trade_reason }}</p>
        <p class="sd-basis">{{ basis(selected) }} · {{ selected.policy }}</p>
        <div class="sd-detail-grid"><section><h5>信号资格未通过</h5><ul class="sd-reasons"><li v-for="gate in selected.scans.gates" :key="gate.code" :data-gate-code="gate.code"><span>{{ gate.label }}<small>{{ gate.examples?.join('、') }}</small></span><b>{{ number(gate.count) }}</b></li></ul><p v-if="!selected.scans.gates.length" class="sd-empty">{{ report.source.available ? '无已记录的资格拦截' : '扫描档案缺失' }}</p></section>
          <section><h5>执行记录中的未成交原因</h5><ul class="sd-reasons"><li v-for="item in (selected.ledger?.top_blockers || [])" :key="item.reason"><span>{{ item.reason }}</span><b>{{ item.count }}</b></li></ul><p v-if="!selected.ledger?.top_blockers?.length" class="sd-empty">无当日执行拦截记录</p></section></div>
        <dl class="sd-facts"><div><dt>最高入场评分</dt><dd>{{ selected.scans.observations ? number(selected.scans.highest_entry_score, 1) : '--' }}</dd></div><div><dt>扫描覆盖证券</dt><dd>{{ count(selected, 'symbols') }}</dd></div><div><dt>扫描首末时点</dt><dd>{{ time(selected.scans.first_at) }} 至 {{ time(selected.scans.last_at) }}</dd></div><div><dt>当日成交额（元）</dt><dd>{{ money(selected.ledger?.turnover) }}</dd></div><div><dt>账本权益快照</dt><dd>{{ snapshotState(selected) }}</dd></div><div><dt>累计收益率 / 较基线</dt><dd>{{ snapshotReturn(selected) }} / {{ selected.return_difference == null ? '--' : number(selected.return_difference, 2) + '个百分点' }}</dd></div><div><dt>权益记录时点</dt><dd>{{ time(selected.snapshot?.generated_at) }}</dd></div><div><dt>排除 / 重复 / 起点前扫描</dt><dd>{{ selected.scans.excluded_scans }} / {{ selected.scans.duplicate_scans }} / {{ selected.scans.before_start }}</dd></div></dl>
      </div>
    </template>
  </section>`,
}
