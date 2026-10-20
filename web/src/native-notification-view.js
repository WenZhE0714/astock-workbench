import { Bell, BellOff, Check, CheckCheck, ArrowUpRight, Star, RefreshCw, Save } from "lucide-vue-next"

export const NativeNotificationView = {
  components: { Bell, BellOff, Check, CheckCheck, ArrowUpRight, Star, RefreshCw, Save },
  emits: ['open','account'],
  data() { return { items: [], reviews: {items:{},symbols:{}}, enabled:false, error:'', loading:false, busy:false, filter:'pending', selectedID:new URLSearchParams(location.search).get('event') || '', notes:{}, query:'', timer:null, controller:null } },
  computed: {
    visibleItems() { return this.items.filter(item => (!this.query || `${item.title} ${item.subtitle} ${item.symbol || ''}`.includes(this.query)) && (this.filter === 'all' || this.filter === 'pending' && !this.handled(item.id) || this.filter === 'unread' && !this.read(item.id) || this.filter === 'followed' && this.reviews.symbols[item.symbol]?.followed)) },
    followed() { return Object.entries(this.reviews.symbols).filter(([,s])=>s.followed).map(([symbol])=>symbol) },
    mutedSymbols() { return Object.entries(this.reviews.symbols).filter(([,s])=>new Date(s.muted_until).getTime()>Date.now()).map(([symbol])=>symbol) },
  },
  methods: {
    validTime(value) { return value && !value.startsWith('0001') },
    read(id) { return this.validTime(this.reviews.items[id]?.read_at) },
    handled(id) { return this.validTime(this.reviews.items[id]?.handled_at) },
    muted(symbol) { return new Date(this.reviews.symbols[symbol]?.muted_until).getTime()>Date.now() },
    time(value) { return new Date(value).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false}) },
    status(value) { return {submitted:'已提交给系统',pending:'等待发送',failed:'发送失败',expired:'已过期',muted:'当天静音'}[value] || value },
    async load() {
      this.controller?.abort();const controller=new AbortController();this.controller=controller;this.loading=true
      try { const response=await fetch('/api/native-notifications',{signal:controller.signal,cache:'no-store'});const result=await response.json();if(!response.ok)throw Error(result.error);this.items=result.items||[];this.reviews=result.reviews||{items:{},symbols:{}};this.enabled=result.enabled;this.error=''
        if(this.selectedID) { this.filter='all';await this.$nextTick();this.$el.querySelector('[data-selected="true"]')?.scrollIntoView({block:'center'});this.selectedID='' }
      } catch(error) { if(error.name!=='AbortError')this.error=error.message } finally { if(this.controller===controller)this.loading=false }
    },
    async action(action,id='',symbol='',note='') {
      this.busy=true
      try { const response=await fetch('/api/native-notifications',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action,id,symbol,note})});const result=await response.json();if(!response.ok)throw Error(result.error);await this.load() } catch(error){this.error=error.message} finally{this.busy=false}
    },
    open(item) { this.action('read',item.id);this.$emit('open',{symbol:item.symbol}) },
  },
  mounted(){this.load();this.timer=setInterval(()=>this.load(),30000)},
  beforeUnmount(){clearInterval(this.timer);this.controller?.abort()},
  template:`<section class="native-notifications" aria-label="Mac 通知与跟踪" :aria-busy="loading">
    <header class="nn-head"><div><bell :size="18"/><h2>Mac 通知与跟踪</h2><span>{{ enabled?'后台通知已开启':'后台通知未开启' }}</span></div><button class="icon-button" title="刷新通知" aria-label="刷新通知" :disabled="loading" @click="load"><refresh-cw :size="16"/></button></header>
    <p v-if="error" class="nn-error" role="alert">{{ error }}</p>
    <div class="nn-toolbar"><select v-model="filter" aria-label="Mac 通知筛选"><option value="pending">未处理</option><option value="unread">未读</option><option value="all">全部</option><option value="followed">重点跟踪</option></select><input v-model.trim="query" placeholder="股票或提醒内容" aria-label="搜索 Mac 通知"><span>{{ visibleItems.length }}条</span></div>
    <div class="nn-follows" v-if="followed.length"><strong>重点跟踪</strong><span v-for="symbol in followed" :key="symbol"><button @click="$emit('open',{symbol})">{{ symbol }}</button><button class="icon-button" :aria-label="'取消跟踪'+symbol" :title="'取消跟踪'+symbol" :disabled="busy" @click="action('unfollow','',symbol)"><star :size="13"/></button></span></div>
    <div class="nn-follows" v-if="mutedSymbols.length"><strong>当天静音</strong><span v-for="symbol in mutedSymbols" :key="symbol">{{ symbol }}<button class="icon-button" :aria-label="'恢复提醒'+symbol" :title="'恢复提醒'+symbol" :disabled="busy" @click="action('unmute','',symbol)"><bell :size="13"/></button></span></div>
    <p class="nn-empty" v-if="!visibleItems.length">暂无符合条件的提醒</p>
    <article class="nn-record" v-for="item in visibleItems" :key="item.id" :class="{unread:!read(item.id)}" :data-selected="item.id===selectedID">
      <div class="nn-record-head"><div><h3>{{ item.title }}</h3><strong>{{ item.subtitle }}</strong><small>{{ time(item.at) }} · {{ status(item.status) }} · {{ handled(item.id)?'已处理':'待处理' }}</small></div><div class="nn-record-actions"><button class="icon-button" title="标为已读" aria-label="标为已读" :disabled="busy || read(item.id)" @click="action('read',item.id)"><check :size="16"/></button><button class="icon-button" title="标为已处理" aria-label="标为已处理" :disabled="busy || handled(item.id)" @click="action('handle',item.id)"><check-check :size="16"/></button><button v-if="item.symbol" class="icon-button" :title="reviews.symbols[item.symbol]?.followed?'取消重点跟踪':'加入重点跟踪'" :aria-label="reviews.symbols[item.symbol]?.followed?'取消重点跟踪':'加入重点跟踪'" :aria-pressed="!!reviews.symbols[item.symbol]?.followed" :disabled="busy" @click="action(reviews.symbols[item.symbol]?.followed?'unfollow':'follow','',item.symbol)"><star :size="16"/></button><button v-if="item.symbol" class="icon-button" :title="muted(item.symbol)?'恢复提醒':'当天静音'" :aria-label="muted(item.symbol)?'恢复提醒':'当天静音'" :disabled="busy" @click="action(muted(item.symbol)?'unmute':'mute','',item.symbol)"><bell v-if="muted(item.symbol)" :size="16"/><bell-off v-else :size="16"/></button><button v-if="item.symbol" class="icon-button" title="打开个股行情" aria-label="打开个股行情" @click="open(item)"><arrow-up-right :size="16"/></button></div></div>
      <p class="nn-body">{{ item.body }}</p><p class="nn-error" v-if="item.last_error">{{ item.last_error }}</p><a v-if="item.kind?.startsWith('forward-')" class="nn-account" href="/?view=strategy&forward=acceptance">查看前瞻验收 <arrow-up-right :size="13"/></a><button v-if="item.profile" class="nn-account" @click="$emit('account',item.profile)">打开所属模拟账户 <arrow-up-right :size="13"/></button><div class="nn-note"><textarea :value="notes[item.id] ?? reviews.items[item.id]?.note ?? ''" @input="notes[item.id]=$event.target.value" rows="2" maxlength="500" :aria-label="'处理备注 '+item.id" placeholder="处理备注"></textarea><button class="icon-button" title="保存备注" aria-label="保存备注" :disabled="busy" @click="action('note',item.id,'',notes[item.id] ?? reviews.items[item.id]?.note ?? '')"><save :size="16"/></button></div>
    </article>
  </section>`,
}
