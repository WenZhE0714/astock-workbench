import { Bell, Check, CheckCheck, ArrowUpRight, Trash2 } from "lucide-vue-next"
import { alertStorageKey, emptyAlertState, parseAlertState, collectLocalAlerts, deliveryCandidates } from "./local-alerts.mjs"
import { planMonitorEventLabel } from "./plan-monitor.mjs"

export const LocalAlertCenter = {
  components: { Bell, Check, CheckCheck, ArrowUpRight, Trash2 },
  props: ["monitors", "ready", "visible"],
  emits: ["enabled", "unread", "open"],
  data() { return { state: emptyAlertState(), filter: "unread", permission: typeof Notification === "undefined" ? "unsupported" : Notification.permission, error: "", busy: false, notifications: [] } },
  computed: {
    unread() { return this.state.records.filter(item => !item.read).length },
    items() { return this.state.records.filter(item => this.filter === "all" || (this.filter === "unread" ? !item.read : !item.handled)) },
    permissionLabel() { return { unsupported: "当前浏览器不支持桌面通知", denied: "通知权限已被阻止", default: "尚未授权", granted: this.state.enabled ? "页面在线提醒已开启" : "桌面提醒已关闭" }[this.permission] },
  },
  watch: {
    monitors() { this.ingest() },
    ready() { this.ingest() },
    unread(value) { this.$emit("unread", value) },
  },
  methods: {
    planMonitorEventLabel,
    time(value) { return new Date(value).toLocaleString("zh-CN", { timeZone: "Asia/Shanghai", hour12: false }) },
    deliveryLabel(value) { return { history: "历史记录", pending: "站内记录", shown: "已交给系统通知", cooldown: "冷却期内合并", blocked: "通知未送达" }[value] || "站内记录" },
    readStored() { return parseAlertState(localStorage.getItem(alertStorageKey)) },
    persist(state) {
      localStorage.setItem(alertStorageKey, JSON.stringify(state))
      this.state = state
      this.$emit("enabled", state.enabled)
    },
    async transaction(action) {
      const run = async () => {
        try { await action(this.readStored()); this.error = "" }
        catch (error) { this.error = "本地提醒记录读写失败：" + (error.message || String(error)) }
      }
      if (navigator.locks) await navigator.locks.request(alertStorageKey, run)
      else await run()
    },
    async ingest() {
      if (!this.ready) return
      await this.transaction(async previous => {
        const now = Date.now()
        const { state, incoming } = collectLocalAlerts(previous, this.monitors, now)
        this.permission = typeof Notification === "undefined" ? "unsupported" : Notification.permission
        const send = state.enabled && this.permission === "granted" ? deliveryCandidates(state, incoming, now) : []
        this.persist(state)
        for (const record of send) {
          try {
            const notification = new Notification(`${record.symbol} · ${record.name || '计划'} · ${planMonitorEventLabel(record.kind)}`, { body: record.message, tag: record.id })
            notification.onclick = () => { window.focus(); this.open(record); notification.close() }
            this.notifications.push(notification)
            if (this.notifications.length > 20) this.notifications.shift().close()
          } catch {
            record.delivery = "blocked"
            this.persist(state)
          }
        }
      })
    },
    async toggle(event) {
      const enabled = event.target.checked
      this.busy = true
      try {
        if (enabled && this.permission !== "unsupported") this.permission = await Notification.requestPermission()
        await this.transaction(state => {
          state.enabled = enabled && this.permission === "granted"
          if (state.enabled) state.enabledAt = Date.now()
          this.persist(state)
        })
      } catch (error) { this.error = error.message || "通知授权失败" }
      finally { this.busy = false; event.target.checked = this.state.enabled }
    },
    async open(record) {
      await this.transaction(state => { const item = state.records.find(item => item.id === record.id); if (item) item.read = true; this.persist(state) })
      this.$emit("open", { symbol: record.symbol, plan_id: record.planID })
    },
    async handle(record) {
      await this.transaction(state => { const item = state.records.find(item => item.id === record.id); if (item) { item.read = true; item.handled = true }; this.persist(state) })
    },
    async markAllRead() { await this.transaction(state => { state.records.forEach(item => { item.read = true }); this.persist(state) }) },
    async clearHandled() { await this.transaction(state => { state.records = state.records.filter(item => !item.handled); this.persist(state) }) },
    sync(event) {
      if (event.key !== alertStorageKey) return
      try { this.state = this.readStored(); this.$emit("enabled", this.state.enabled) } catch (error) { this.error = error.message }
    },
  },
  mounted() {
    try { this.state = this.readStored(); this.$emit("enabled", this.state.enabled) } catch (error) { this.error = error.message }
    window.addEventListener("storage", this.sync)
    this.ingest()
  },
  beforeUnmount() { window.removeEventListener("storage", this.sync); this.notifications.forEach(item => item.close()) },
  template: `<section v-show="visible" class="local-alert-center" aria-label="本地提醒记录">
    <header class="local-alert-heading"><div><bell :size="18"/><h2>提醒记录</h2><span>{{ unread }} 条未读</span></div><label><input type="checkbox" :checked="state.enabled" :disabled="busy || permission === 'unsupported'" @change="toggle" aria-label="桌面提醒"><span>桌面提醒</span></label></header>
    <div class="local-alert-toolbar"><div class="dashboard-segment" role="group" aria-label="提醒筛选"><button type="button" v-for="option in [{key:'unread',label:'未读'},{key:'pending',label:'未处理'},{key:'all',label:'全部'}]" :key="option.key" :aria-pressed="filter === option.key" @click="filter = option.key">{{ option.label }}</button></div><span>{{ permissionLabel }}</span><button class="icon-button" type="button" title="全部标为已读" aria-label="全部标为已读" :disabled="!unread" @click="markAllRead"><check-check :size="16"/></button><button class="icon-button" type="button" title="清除已处理记录" aria-label="清除已处理记录" :disabled="!state.records.some(item => item.handled)" @click="clearHandled"><trash-2 :size="16"/></button></div>
    <p v-if="error" class="chart-plan-error" role="alert">{{ error }}</p>
    <p class="dashboard-empty" v-if="!items.length">{{ ready ? '暂无符合条件的提醒' : '正在读取监控事件' }}</p>
    <ol class="local-alert-list"><li v-for="item in items" :key="item.id" :class="{unread:!item.read}"><div class="local-alert-copy"><button type="button" @click="open(item)"><strong>{{ item.symbol }}</strong><span>{{ item.name }} · {{ planMonitorEventLabel(item.kind) }}</span><arrow-up-right :size="14"/></button><p>{{ item.message }}</p><small>{{ time(item.observedAt) }} · {{ deliveryLabel(item.delivery) }} · {{ item.handled ? '已处理' : '待处理' }}</small></div><button type="button" class="icon-button" :title="item.handled ? '已处理' : '标为已处理'" :aria-label="'处理提醒 ' + item.id" :disabled="item.handled" @click="handle(item)"><check :size="16"/></button></li></ol>
  </section>`,
}
