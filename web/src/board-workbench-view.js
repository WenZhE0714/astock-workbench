import { LayoutGrid, List, RefreshCw, Play, Pause, Radio, ChevronLeft, ChevronRight, Save, SlidersHorizontal, ArrowUp, ArrowDown, ArrowUpRight, X, Star } from "lucide-vue-next"
import { boardModules, boardPresets, boardLayoutKey, normalizeBoardLayout, boardBubbles, boardDailyStats, linkedBoardRows, selectedSnapshot, shanghaiDate, boardDailySession } from "./board-workbench.mjs"

export const BoardWorkbench = {
  components: { LayoutGrid, List, RefreshCw, Play, Pause, Radio, ChevronLeft, ChevronRight, Save, SlidersHorizontal, ArrowUp, ArrowDown, ArrowUpRight, X, Star },
  props: { initialCode: String },
  emits: ["open", "board"],
  data() { return { boardModules, boardPresets, layout: normalizeBoardLayout(), preset: "intraday", saved: [], saveName: "", editing: false, error: "", notice: "", loading: false,
    snapshots: [], dates: [], date: shanghaiDate(), live: true, index: 0, playing: false, code: this.initialCode || "", detailsCode: "", query: "", sort: "flow", direction: "all",
    current: { items: [], signals: [], holdings: [] }, daily: [], dailySource: "", dailyError: "", dailyLoading: false, dailyFetchedAt: 0, dailySession: "", selectedSymbol: "", stock: null, stockError: "", stockLoading: false,
    controller: null, dailyController: null, stockController: null, timer: null, playTimer: null, mountedAlive: true, members: [], membersError: "" } },
  computed: {
    today() { return shanghaiDate() },
    snapshot() { return this.live ? this.current : selectedSnapshot(this.snapshots, this.index, false) },
    boards() { return this.snapshot?.items || [] },
    selected() { return this.boards.find(item => item.code === this.code) || null },
    visibleBoards() { return [...this.boards].filter(item => (!this.query || `${item.name} ${item.code}`.includes(this.query)) && (this.direction === "all" || (this.direction === "in" ? item.main_net_yuan > 0 : item.main_net_yuan < 0))).sort((a, b) => this.sort === "name" ? a.name.localeCompare(b.name, "zh") : Number(b[this.sort === "flow" ? "main_net_yuan" : "percent"] ?? -Infinity) - Number(a[this.sort === "flow" ? "main_net_yuan" : "percent"] ?? -Infinity)) },
    bubble() { return boardBubbles(this.visibleBoards) },
    signals() { return linkedBoardRows(this.snapshot?.signals, this.selected) },
    holdings() { return linkedBoardRows(this.snapshot?.holdings, this.selected) },
    stockSignals() { return this.signals.filter(item => item.symbol === this.selectedSymbol) },
    stockHoldings() { return this.holdings.filter(item => item.symbol === this.selectedSymbol) },
    dailyStats() { return boardDailyStats(this.daily) },
    dailyBars() { const rows = this.daily.slice(-20), max = Math.max(1, ...rows.map(r => Math.abs(r.main_net_yuan))); return rows.map((r, i) => ({ ...r, x: 30 + i * (740 / Math.max(1, rows.length)), width: Math.max(4, 600 / Math.max(1, rows.length)), y: r.main_net_yuan >= 0 ? 112 - r.main_net_yuan / max * 80 : 112, height: Math.max(1, Math.abs(r.main_net_yuan) / max * 80) })) },
    intradayPoints() { return this.snapshots.slice(0, this.live ? undefined : this.index + 1).map(s => ({ at: s.captured_at, item: s.items.find(b => b.code === this.code) })).filter(p => p.item && p.item.main_net_yuan != null) },
    intradayPath() { const rows = this.intradayPoints; const max = Math.max(1, ...rows.map(r => Math.abs(r.item.main_net_yuan))); return rows.map((r, i) => `${i ? 'L' : 'M'}${30 + i / Math.max(1, rows.length - 1) * 740},${95 - r.item.main_net_yuan / max * 65}`).join(" ") },
    selectedStock() { return this.stockSignals[0] || this.stockHoldings[0] || this.members.find(m => m.symbol === this.selectedSymbol) || null },
  },
  watch: {
    initialCode(value) { if (value && value !== this.code) this.chooseBoard(value) },
    code() { this.selectedSymbol = ""; this.stockController?.abort(); this.stock = null; this.loadBoardDetails() },
  },
  methods: {
    amount(value) { if (value == null || !Number.isFinite(Number(value))) return "--"; const n = Number(value); return `${n > 0 ? '+' : n < 0 ? '-' : ''}${Math.abs(n) >= 1e8 ? (Math.abs(n) / 1e8).toFixed(2) + '亿' : (Math.abs(n) / 1e4).toFixed(0) + '万'}` },
    percent(value) { return value == null ? "--" : `${Number(value) > 0 ? '+' : ''}${Number(value).toFixed(2)}%` },
    time(value) { if (!value) return "--"; return new Date(value).toLocaleString('zh-CN', {timeZone:'Asia/Shanghai', hour12:false}) },
    shortTime(value) { return value ? new Date(value).toLocaleTimeString('zh-CN', {timeZone:'Asia/Shanghai', hour12:false}) : "--" },
    tone(value) { return value > 0 ? 'up' : value < 0 ? 'down' : '' },
    stateName(value) { return {triggered:'触发',watching:'观察',weak:'偏弱',invalid:'失效',insufficient:'数据不足'}[value] || value },
    moduleName(id) { return boardModules.find(m => m.id === id)?.name || id },
    async request(url, signal) { const response = await fetch(url, { signal, cache: 'no-store' }); const data = await response.json(); if (!response.ok) throw Error(data.error || data.warning || '读取失败'); return data },
    async load() {
      this.controller?.abort(); const controller = new AbortController(); this.controller = controller; this.loading = true; this.error = ''
      try {
        const after = this.live && this.snapshots[0]?.date === this.date ? this.snapshots.at(-1)?.id : ''
        const archive = await this.request(`/api/board-snapshots?date=${this.date}${after ? '&after='+after : ''}`, controller.signal)
        if (controller.signal.aborted) return
        const oldID = this.snapshots[this.index]?.id
        this.snapshots = after ? [...this.snapshots, ...(archive.snapshots || [])] : archive.snapshots || []; this.dates = archive.dates || []; this.notice = archive.warning || ''
        if (!this.live) { const found = this.snapshots.findIndex(s => s.id === oldID); this.index = found >= 0 ? found : Math.min(this.index, Math.max(0, this.snapshots.length - 1)) }
        if (this.live) {
          let latest = this.snapshots.at(-1), board = null
          try { board = await this.request('/api/boards?sort=flow', controller.signal) }
          catch(error) {
            if(error.name==='AbortError') throw error
            this.notice = /403|Forbidden/.test(error.message) ? '板块数据源暂时拒绝访问（403）' : '板块数据源暂时不可用'
            if (!latest && this.dates.length && this.dates[0] !== this.date) { const previous = await this.request(`/api/board-snapshots?date=${this.dates[0]}`,controller.signal); latest = previous.snapshots?.at(-1) }
            if (latest) this.current = {...latest, retained:true}
            else this.current = {...this.current, retained:true}
            this.notice += this.current.items.length ? '，已保留最近有效快照' : '，尚无可用归档快照'
          }
          if (board) this.current = {items: board.items || [], signals: latest?.signals || [], holdings: latest?.holdings || [], captured_at: board.fetched_at, warnings: latest?.warnings || []}
          if (board?.warning) this.notice = board.warning
          if (board && !latest) {
            const result = await this.request('/api/strategy/realtime', controller.signal)
            this.current.signals = result.result?.signals || []
            const accounts = await this.request('/api/strategy/shadow?profile=balanced', controller.signal)
            this.current.holdings = (accounts.report?.positions || []).map(p => ({...p, available:p.available_quantity, profile:'balanced', profile_name:'均衡型', as_of:accounts.report.generated_at}))
          }
        }
        if (!this.code || !this.boards.some(b => b.code === this.code)) this.code = this.boards[0]?.code || ''
        else if (this.detailsCode !== this.code) this.loadBoardDetails()
        else {
          if (!this.dailyLoading && (this.dailyError || Date.now() - this.dailyFetchedAt >= 600000 || this.dailySession !== boardDailySession())) this.loadDaily(true)
          if (this.membersError) this.loadMembers()
        }
      } catch (error) { if (error.name !== 'AbortError') this.error = error.message }
      finally { if (this.controller === controller) this.loading = false }
    },
    chooseBoard(code) { this.code = code; this.$emit('board', code) },
    loadBoardDetails() { this.detailsCode = this.code; this.loadDaily(); this.loadMembers() },
    async loadDaily(preserve = false) {
      this.dailyController?.abort(); const controller = new AbortController(); this.dailyController = controller
      if (!preserve) this.daily = []
      this.dailyError = ''; this.dailyLoading = false; if (!this.code) return; this.dailyLoading = true
      const session = boardDailySession()
      const through = this.live ? shanghaiDate() : shanghaiDate(new Date(`${this.date}T00:00:00+08:00`).getTime() - 86400000)
      try { const result = await this.request(`/api/board-daily-flows?code=${this.code}&through=${through}`, controller.signal); this.daily = result.items || []; this.dailySource = result.source || ''; this.dailyFetchedAt = Date.now(); this.dailySession = session }
      catch (error) { if (error.name !== 'AbortError') this.dailyError = error.message }
      finally { if (this.dailyController === controller) this.dailyLoading = false }
    },
    async loadMembers() {
      this.members = []; this.membersError = ''; if (!this.live || !this.code) return
      const code = this.code
      try { const result = await this.request(`/api/stock?symbol=${encodeURIComponent(code)}&limit=60`); if (code === this.code && this.live && this.mountedAlive) this.members = result.board?.leaders || [] }
      catch (error) { if (code === this.code && this.live) this.membersError = error.message }
    },
    async chooseStock(symbol) {
      this.selectedSymbol = symbol; this.stock = null; this.stockError = ''; this.stockController?.abort()
      if (!this.live) return
      const controller = new AbortController(); this.stockController = controller; this.stockLoading = true
      try { this.stock = await this.request(`/api/stock?symbol=${encodeURIComponent(symbol)}&limit=60`, controller.signal) }
      catch (error) { if (error.name !== 'AbortError') this.stockError = error.message }
      finally { if (this.stockController === controller) this.stockLoading = false }
    },
    async chooseDate() { this.stop(); this.live = false; this.index = 0; this.snapshots = []; this.current = {items:[],signals:[],holdings:[]}; this.members = []; this.stockController?.abort(); this.stock = null; await this.load(); this.loadDaily() },
    seek(value) { this.stop(); this.live = false; this.index = Number(value); this.stockController?.abort(); this.stock = null; this.members = []; this.loadDaily() },
    async goLive() { this.stop(); this.live = true; this.date = shanghaiDate(); await this.load(); this.loadDaily(); this.loadMembers(); if (this.selectedSymbol) this.chooseStock(this.selectedSymbol) },
    play() { if (this.playing) return this.stop(); if (this.snapshots.length < 2) return; this.live = false; this.stock = null; this.members = []; this.stockController?.abort(); if (this.index >= this.snapshots.length - 1) this.index = 0; this.playing = true; this.loadDaily(); this.playTimer = setInterval(() => { if (this.index < this.snapshots.length - 1) this.index++; else this.stop() }, 900) },
    stop() { clearInterval(this.playTimer); this.playing = false },
    applyPreset(id) { const preset = boardPresets.find(p => p.id === id) || this.saved.find(p => p.id === id); if (!preset) return; this.preset = id; this.layout = normalizeBoardLayout(preset); this.persist() },
    persist() { try { localStorage.setItem(boardLayoutKey, JSON.stringify({version:1, layout:this.layout, preset:this.preset, saved:this.saved})) } catch { this.error = '工作区设置保存失败' } },
    savePreset() { const name = this.saveName.trim().slice(0, 24); if (!name) { this.error = '请输入预设名称'; return }; const existing = this.saved.find(p => p.name === name); const id = existing?.id || `custom-${Date.now()}`; this.saved = [...this.saved.filter(p => p.id !== id), {id,name,...normalizeBoardLayout(this.layout)}].slice(-12); this.preset = id; this.saveName = ''; this.persist() },
    removePreset() { this.saved = this.saved.filter(p => p.id !== this.preset); this.applyPreset('intraday') },
    move(id, delta) { const rows = [...this.layout.modules], index = rows.indexOf(id), target = index + delta; if (target < 0 || target >= rows.length) return; [rows[index],rows[target]] = [rows[target],rows[index]]; this.layout.modules = rows; this.persist() },
    toggle(id, value) { if (value) this.layout.modules.push(id); else if (this.layout.modules.length > 1) this.layout.modules = this.layout.modules.filter(x => x !== id); this.persist() },
    wide(id, value) { this.layout.wide = value ? [...new Set([...this.layout.wide,id])] : this.layout.wide.filter(x => x !== id); this.persist() },
    async follow(symbol) { try { const response = await fetch('/api/native-notifications', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({action:'follow',symbol})}); const data = await response.json(); if (!response.ok) throw Error(data.error); this.notice = '已加入重点跟踪' } catch(error) { this.error = error.message } },
  },
  mounted() {
    try { const saved = JSON.parse(localStorage.getItem(boardLayoutKey) || 'null'); if (saved?.version === 1) { this.layout = normalizeBoardLayout(saved.layout); this.saved = Array.isArray(saved.saved) ? saved.saved.slice(0,12).filter(p => p && typeof p.id === 'string' && typeof p.name === 'string').map(p => ({id:p.id,name:p.name,...normalizeBoardLayout(p)})) : []; this.preset = saved.preset || 'intraday' } } catch { this.error = '工作区设置损坏，已使用默认布局' }
    this.load(); this.timer = setInterval(() => { if (this.live) { this.date = shanghaiDate(); this.load() } }, 30000)
  },
  beforeUnmount() { this.mountedAlive = false; this.controller?.abort(); this.dailyController?.abort(); this.stockController?.abort(); clearInterval(this.timer); this.stop() },
  template: `<section class="board-workbench" aria-label="板块资金工作台" :aria-busy="loading">
    <header class="bw-heading"><div><h1>板块资金</h1><span>{{ live ? (current.retained ? '保留快照' : '实时快照') : '历史回放' }} · {{ time(snapshot?.captured_at) }}</span></div><div class="bw-actions"><select v-model="preset" @change="applyPreset(preset)" aria-label="工作区预设"><option v-for="item in [...boardPresets,...saved]" :key="item.id" :value="item.id">{{ item.name }}</option></select><button class="icon-button" title="调整布局" aria-label="调整布局" :aria-pressed="editing" @click="editing=!editing"><sliders-horizontal :size="17"/></button><button class="icon-button" title="刷新板块" aria-label="刷新板块" :disabled="loading" @click="load"><refresh-cw :size="17"/></button></div></header>
    <div v-if="editing" class="bw-editor"><div class="bw-editor-row" v-for="item in boardModules" :key="item.id"><label><input type="checkbox" :checked="layout.modules.includes(item.id)" @change="toggle(item.id,$event.target.checked)">{{ item.name }}</label><button class="icon-button" :aria-label="'前移'+item.name" :title="'前移'+item.name" :disabled="layout.modules.indexOf(item.id)<=0" @click="move(item.id,-1)"><arrow-up :size="14"/></button><button class="icon-button" :aria-label="'后移'+item.name" :title="'后移'+item.name" :disabled="!layout.modules.includes(item.id) || layout.modules.indexOf(item.id)===layout.modules.length-1" @click="move(item.id,1)"><arrow-down :size="14"/></button><label><input type="checkbox" :checked="layout.wide.includes(item.id)" @change="wide(item.id,$event.target.checked)">全宽</label></div><div class="bw-save"><input v-model="saveName" placeholder="预设名称" aria-label="预设名称" maxlength="24"><button class="icon-button" title="保存预设" aria-label="保存预设" @click="savePreset"><save :size="17"/></button><button v-if="preset.startsWith('custom-')" class="icon-button" title="删除当前预设" aria-label="删除当前预设" @click="removePreset"><x :size="17"/></button></div></div>
    <p v-if="error" class="bw-error" role="alert">{{ error }}</p><p v-if="notice" class="bw-notice">{{ notice }}</p>
    <div class="bw-timeline"><input type="date" v-model="date" :max="today" aria-label="回放日期" @change="chooseDate"><button class="icon-button" :title="playing?'暂停回放':'播放回放'" :aria-label="playing?'暂停回放':'播放回放'" :disabled="snapshots.length<2" @click="play"><pause v-if="playing" :size="16"/><play v-else :size="16"/></button><input type="range" min="0" :max="Math.max(0,snapshots.length-1)" :value="live ? snapshots.length-1 : index" :disabled="!snapshots.length" aria-label="盘中回放时间" @input="seek($event.target.value)"><span>{{ snapshots.length ? shortTime((live?snapshots.at(-1):snapshots[index])?.captured_at) : '当日未采集' }}</span><button class="bw-live" :aria-pressed="live" @click="goLive"><radio :size="15"/>实时</button></div>
    <div v-if="!live && !snapshots.length" class="bw-empty">所选日期没有已保存的盘中快照</div>
    <div class="bw-selection"><strong>{{ selected?.name || '选择板块' }}</strong><span :class="tone(selected?.percent)">{{ percent(selected?.percent) }}</span><span :class="tone(selected?.main_net_yuan)">{{ amount(selected?.main_net_yuan) }}</span><span>{{ selected?.source || '供应商未标注来源' }}</span><button v-if="selected" class="icon-button" title="打开板块行情" aria-label="打开板块行情" @click="$emit('open',{symbol:code})"><arrow-up-right :size="16"/></button></div>
    <div class="bw-grid"><section v-for="id in layout.modules" :key="id" class="bw-module" :class="{'bw-wide':layout.wide.includes(id)}" :aria-label="moduleName(id)">
      <header class="bw-module-head"><h2>{{ moduleName(id) }}</h2><div v-if="id==='market'" class="bw-segment"><button :aria-pressed="layout.mode==='bubble'" title="资金气泡" aria-label="资金气泡" @click="layout.mode='bubble';persist()"><layout-grid :size="16"/></button><button :aria-pressed="layout.mode==='table'" title="资金榜单" aria-label="资金榜单" @click="layout.mode='table';persist()"><list :size="16"/></button></div></header>
      <template v-if="id==='market'"><div class="bw-filters"><input v-model.trim="query" placeholder="板块名称或代码" aria-label="筛选板块"><select v-model="direction" aria-label="资金方向"><option value="all">全部方向</option><option value="in">净流入</option><option value="out">净流出</option></select><select v-model="sort" aria-label="板块排序"><option value="flow">主力净额</option><option value="percent">涨跌幅</option><option value="name">名称</option></select></div>
        <svg v-if="layout.mode==='bubble'" class="bw-bubbles" viewBox="0 0 940 430" role="img" aria-label="板块涨跌与资金四象限"><rect x="70" y="42" width="400" height="170" fill="#192a27"/><rect x="470" y="42" width="400" height="170" fill="#302326"/><rect x="70" y="212" width="400" height="170" fill="#202831"/><rect x="470" y="212" width="400" height="170" fill="#302b21"/><path d="M70 212H870M470 42V382" stroke="#a5adb8" stroke-dasharray="4 4"/><text x="70" y="25">主力净流入（亿）</text><text x="810" y="415">涨跌幅（%）</text><text x="25" y="55">{{ (bubble.maxY/1e8).toFixed(1) }}</text><text x="20" y="380">-{{ (bubble.maxY/1e8).toFixed(1) }}</text><text x="70" y="405">-{{ bubble.maxX.toFixed(1) }}</text><text x="465" y="405">0</text><text x="855" y="405">{{ bubble.maxX.toFixed(1) }}</text><circle v-for="item in bubble.points" :key="item.code" :cx="item.x" :cy="item.y" :r="item.radius" :fill="item.main_net_yuan>=0?'#d94b52':'#16826b'" :stroke="item.code===code?'#253442':'white'" :stroke-width="item.code===code?3:1" opacity=".78" tabindex="0" role="button" :aria-label="item.name+' '+percent(item.percent)+' 主力'+amount(item.main_net_yuan)" @click="chooseBoard(item.code)" @keydown.enter="chooseBoard(item.code)"><title>{{ item.name }} · {{ percent(item.percent) }} · {{ amount(item.main_net_yuan) }}</title></circle></svg>
        <div v-else class="bw-table-wrap"><table class="bw-table"><thead><tr><th>板块</th><th>涨跌幅</th><th>主力净额</th><th>净占比</th><th>涨 / 跌</th></tr></thead><tbody><tr v-for="item in visibleBoards" :key="item.code" :class="{selected:item.code===code}"><td><button @click="chooseBoard(item.code)">{{ item.name }}<small>{{ item.code }}</small></button></td><td :class="tone(item.percent)">{{ percent(item.percent) }}</td><td :class="tone(item.main_net_yuan)">{{ amount(item.main_net_yuan) }}</td><td>{{ percent(item.main_ratio_percent) }}</td><td>{{ item.rise_count }} / {{ item.fall_count }}</td></tr></tbody></table></div><p v-if="!visibleBoards.length" class="bw-empty">暂无匹配板块</p><div v-if="layout.mode===\'bubble\'" class="bw-quadrants"><span>左上：下跌流入</span><span>右上：上涨流入</span><span>左下：下跌流出</span><span>右下：上涨流出</span></div><small class="bw-source">{{ visibleBoards.length }} 个行业板块 · 本地观察时间 {{ time(snapshot?.captured_at) }}</small></template>
      <template v-else-if="id==='trend'"><h3>{{ selected?.name || '--' }} · 盘中累计主力净额</h3><svg v-if="intradayPoints.length" class="bw-chart" viewBox="0 0 800 180" role="img" aria-label="板块盘中资金曲线"><path d="M30 95H770" stroke="#c9d0d9" stroke-dasharray="4 4"/><path :d="intradayPath" fill="none" stroke="#16826b" stroke-width="2.5"/><text x="30" y="173">{{ shortTime(intradayPoints[0]?.at) }}</text><text x="685" y="173">{{ shortTime(intradayPoints.at(-1)?.at) }}</text></svg><p v-else class="bw-empty">尚无该板块盘中采样</p><h3>完整日线资金</h3><div class="bw-metrics"><div><span>连续流入</span><b>{{ dailyStats.count ? dailyStats.streak+'个有数据交易日' : '--' }}</b></div><div><span>近3个有数据交易日</span><b :class="tone(dailyStats.sum3)">{{ amount(dailyStats.sum3) }}</b></div><div><span>近5个有数据交易日</span><b :class="tone(dailyStats.sum5)">{{ amount(dailyStats.sum5) }}</b></div></div><p v-if="dailyError" class="bw-error">{{ dailyError }}</p><p v-if="dailyLoading" class="bw-empty">正在读取资金日线</p><svg v-if="dailyBars.length" class="bw-chart" viewBox="0 0 800 230" role="img" aria-label="板块每日主力净流入"><path d="M25 112H780" stroke="#a5adb8"/><rect v-for="bar in dailyBars" :key="bar.date" :x="bar.x" :y="bar.y" :width="bar.width" :height="bar.height" :fill="bar.main_net_yuan>=0?'#d94b52':'#16826b'"><title>{{ bar.date }} · {{ amount(bar.main_net_yuan) }}</title></rect><text x="30" y="222">{{ dailyBars[0]?.date }}</text><text x="675" y="222">{{ dailyBars.at(-1)?.date }}</text></svg><small class="bw-source">{{ dailySource }} · {{ dailyStats.count }} 个有效日 · 未核验缺失交易日</small></template>
      <template v-else-if="id==='signals'"><p class="bw-empty" v-if="!signals.length">该板块暂无已归档候选</p><button class="bw-linked-row" v-for="item in signals" :key="item.symbol" :class="{selected:item.symbol===selectedSymbol}" @click="chooseStock(item.symbol)"><span><strong>{{ item.name }}</strong><small>{{ item.symbol }} · {{ shortTime(item.as_of) }}</small></span><span>{{ stateName(item.state) }}</span><b>{{ Number(item.score).toFixed(1) }}</b></button></template>
      <template v-else-if="id==='holdings'"><p class="bw-empty" v-if="!holdings.length">该板块暂无已记录模拟持仓</p><button class="bw-linked-row" v-for="item in holdings" :key="item.profile+item.symbol" :class="{selected:item.symbol===selectedSymbol}" @click="chooseStock(item.symbol)"><span><strong>{{ item.name }}</strong><small>{{ item.profile_name }} · {{ time(item.as_of) }}</small></span><span>{{ item.quantity }}股<small>可卖 {{ item.available }}股</small></span><b>{{ amount(item.market_value) }}</b></button></template>
      <template v-else-if="id==='stock'"><p v-if="!selectedSymbol" class="bw-empty">选择关联候选或持仓</p><template v-else><div class="bw-stock-head"><h3>{{ stock?.name || selectedStock?.name || selectedSymbol }}</h3><button class="icon-button" title="重点跟踪" aria-label="重点跟踪" @click="follow(selectedSymbol)"><star :size="16"/></button><button class="icon-button" title="打开个股行情" aria-label="打开个股行情" @click="$emit('open',{symbol:selectedSymbol})"><arrow-up-right :size="16"/></button></div><p v-if="stockError" class="bw-error">{{ stockError }}</p><p v-if="stockLoading" class="bw-empty">正在读取个股行情</p><div class="bw-stock-price"><strong>{{ stock?.quote?.current || selectedStock?.price || '--' }}</strong><span>{{ live?'最新行情':'归档信号价' }}</span></div><p v-for="item in stockSignals" :key="item.symbol">{{ stateName(item.state) }} · {{ Number(item.score).toFixed(1) }}分 · {{ item.reason || item.reasons?.[0] }}</p><p v-for="item in stockHoldings" :key="item.profile">{{ item.profile_name }} · {{ item.quantity }}股 · 可卖{{ item.available }}股</p><small class="bw-source">{{ stock?.quote?.quote_time || time(selectedStock?.as_of) }}</small></template><h3 v-if="live && members.length">同板块成份股</h3><button class="bw-linked-row" v-for="item in members.slice(0,8)" :key="item.symbol" @click="chooseStock(item.symbol)"><span>{{ item.name }}</span><b :class="tone(item.percent)">{{ percent(item.percent) }}</b></button><p v-if="membersError" class="bw-error">{{ membersError }}</p></template>
    </section></div><p v-for="warning in snapshot?.warnings || []" :key="warning" class="bw-notice">{{ warning }}</p>
  </section>`,
}
