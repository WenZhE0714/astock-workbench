import { BellPlus, Pause, Play, Copy, Trash2, RefreshCw, X, ArrowUpRight, Check } from 'lucide-vue-next'
import { legacyPriceAlertKey, phaseLabel, deliveryLabel, ruleFromLegacy, pendingLegacyAlerts } from './price-alerts.mjs'
import './price-alerts.css'

export const PriceAlertsView = {
  components:{BellPlus,Pause,Play,Copy,Trash2,RefreshCw,X,ArrowUpRight,Check},
  props:['symbol','defaultName','defaultPrice','source'],
  data(){return {items:[],runtime:{},nativeEnabled:false,loading:false,busy:false,available:false,error:'',notice:'',warning:'',filter:'all',query:'',controller:null,mutationController:null,timer:null,legacy:[],selectedID:new URLSearchParams(location.search).get('alert')||'',focusedID:'',dialogOpen:false,form:{},clientID:'',formError:'',legacyID:''}},
  computed:{
    visibleItems(){return this.items.filter(a=>(!this.symbol||a.rule.symbol===this.symbol)&&(this.filter==='all'||a.phase===this.filter)&&`${a.rule.name} ${a.rule.symbol}`.toLowerCase().includes(this.query.trim().toLowerCase()))},
    runtimeLabel(){if(!this.available)return '服务待连接';if(!this.runtime.running)return this.runtime.error||'后台尚未运行';return this.runtime.session==='trading'?'后台检查中':'等待交易时段'},
    localLegacy(){return this.legacy.filter(a=>!this.symbol||a.symbol===this.symbol)},
  },
  methods:{
    phaseLabel,deliveryLabel,
    money(v){return Number.isFinite(v)?v.toLocaleString('zh-CN',{minimumFractionDigits:2,maximumFractionDigits:3}):'--'},
    time(v){return v&&!v.startsWith('0001')?new Date(v).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false}):'--'},
    sourceLabel(rule){return rule.source?`${rule.source.id==='forward'?'前瞻封存':'历史演练'} ${rule.source.date}`:'手动价位'},
    async load(){
      this.controller?.abort();const controller=new AbortController();this.controller=controller;this.loading=true
      try{const r=await fetch('/api/price-alerts',{cache:'no-store',signal:controller.signal});const data=await r.json();if(!r.ok)throw Error(data.error||'后台提醒读取失败');this.items=data.items||[];this.runtime=data.runtime||{};this.nativeEnabled=!!data.native_enabled;this.warning=data.warning||'';this.available=true;this.error='';if(this.selectedID&&this.focusedID!==this.selectedID){this.filter='all';await this.$nextTick();this.$el.querySelector('[data-selected="true"]')?.scrollIntoView({block:'center'});this.focusedID=this.selectedID}}
      catch(e){if(e.name!=='AbortError'){this.error=e.message;this.available=false}}
      finally{if(this.controller===controller)this.loading=false}
    },
    async post(body){
      const controller=new AbortController();this.mutationController=controller;const timeout=setTimeout(()=>controller.abort(),20000)
      try{const r=await fetch('/api/price-alerts',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body),signal:controller.signal});const data=await r.json();if(!r.ok)throw Error(data.error||'提醒操作失败');return data}
      finally{clearTimeout(timeout)}
    },
    open(seed=null,legacyID=''){
      if(!this.available||this.busy)return
      this.notice=''
      const expires=new Date(Date.now()+8*3600000+7*86400000).toISOString().slice(0,10)
      this.form=seed?{...seed,source:seed.source?{...seed.source}:undefined}:{symbol:this.symbol||'',name:this.defaultName||'',direction:'above',price:this.defaultPrice||'',expires_on:expires,source:this.source?{...this.source}:undefined}
      if(this.form.expires_on<new Date(Date.now()+8*3600000).toISOString().slice(0,10))this.form.expires_on=expires
      this.clientID=legacyID?`legacy-${legacyID}`:crypto.randomUUID();this.legacyID=legacyID;this.formError='';this.dialogOpen=true;this.$nextTick(()=>this.$refs.dialog.showModal())
    },
    close(){if(this.busy)return;this.$refs.dialog?.close();this.dialogOpen=false},
    migrate(item){this.open(ruleFromLegacy(item),item.id)},
    async confirm(){
      if(this.busy)return
      const price=Number(this.form.price)
      if(!this.form.symbol.trim()||!Number.isFinite(price)||price<=0||price>1e8||!this.form.expires_on){this.formError='请填写股票、有效价位和有效期';return}
      this.busy=true;this.formError=''
      try{
        const data=await this.post({action:'create',client_id:this.clientID,confirmed:true,rule:{...this.form,price}})
        if(this.legacyID){
          try{const old=JSON.parse(localStorage.getItem(legacyPriceAlertKey)||'[]');localStorage.setItem(legacyPriceAlertKey,JSON.stringify(old.filter(a=>a.id!==this.legacyID)));this.legacy=this.legacy.filter(a=>a.id!==this.legacyID)}catch{this.notice='后台规则已保存；旧浏览器记录清理失败，重复确认不会重复建规则'}
        }
        this.selectedID=data.alert.id;this.busy=false;this.close();this.notice=this.notice||(data.alert.event?'该提醒已触发，事件已保留':data.alert.enabled?'后台提醒已启用':'后台规则已保存，当前未启用');await this.load()
      }catch(e){this.formError=e.name==='AbortError'?'请求超时，可重试确认；同一请求不会重复建规则':e.message}finally{this.busy=false}
    },
    async control(item,action){
      if(this.busy)return;this.busy=true;this.error=''
      try{await this.post({action,id:item.id,revision:item.revision,...(action==='enable'?{enabled:!item.enabled}:{})});this.notice=action==='delete'?'提醒已删除':'提醒状态已更新';await this.load()}
      catch(e){this.error=e.name==='AbortError'?'请求超时，请刷新确认状态':e.message}finally{this.busy=false}
    },
    duplicate(item){const expires=new Date(Date.now()+8*3600000+7*86400000).toISOString().slice(0,10);this.open({...item.rule,expires_on:expires})},
  },
  mounted(){try{this.legacy=pendingLegacyAlerts(JSON.parse(localStorage.getItem(legacyPriceAlertKey)||'[]'))}catch{this.notice='旧浏览器提醒读取失败，原数据未修改'}this.load();this.timer=setInterval(()=>this.load(),10000)},
  beforeUnmount(){clearInterval(this.timer);this.controller?.abort();this.mutationController?.abort()},
  template:`<section class="price-alerts" aria-label="后台价位提醒" :aria-busy="loading">
    <div class="pa-heading"><div><h2>价位提醒</h2><span>{{ runtimeLabel }}</span><small>{{ nativeEnabled?'系统通知已开启':'系统通知未开启 · 保留触发记录' }}</small></div><div class="pa-actions"><button class="icon-button" title="刷新后台提醒" aria-label="刷新后台提醒" :disabled="loading" @click="load"><refresh-cw :size="16"/></button><button type="button" class="pa-create" :disabled="!available||busy" @click="open()"><bell-plus :size="16"/>新建提醒</button></div></div>
    <p v-if="error" class="pa-error" role="alert">{{ error }}</p><p v-if="notice" class="pa-notice" role="status">{{ notice }}</p><p v-if="warning" class="pa-notice">{{ warning }}</p>
    <div class="pa-runtime"><span>最近检查 {{ time(runtime.last_cycle_at) }}</span><span>每 {{ runtime.interval_seconds||30 }} 秒</span><a v-if="symbol" href="/?view=monitor&monitor=prices">全部后台提醒 <arrow-up-right :size="13"/></a></div>
    <p class="pa-error" v-if="runtime.error">{{ runtime.error }}</p>
    <div class="pa-toolbar"><select v-model="filter" aria-label="后台提醒筛选"><option value="all">全部状态</option><option value="active">监控中</option><option value="paused">已暂停</option><option value="triggered">已触发</option><option value="expired">已到期</option></select><input v-model="query" placeholder="名称 / 股票代码" aria-label="搜索后台提醒"><span>{{ visibleItems.length }}条</span></div>
    <div class="pa-legacy" v-if="localLegacy.length"><h3>旧本页提醒 · 待迁移</h3><div v-for="a in localLegacy" :key="a.id"><span>{{ a.name||a.symbol }} {{ a.direction==='above'?'≥':'≤' }} {{ money(a.price) }}</span><button type="button" :disabled="!available||busy" @click="migrate(a)">确认迁移</button></div></div>
    <p class="pa-empty" v-if="!visibleItems.length">{{ loading?'正在读取后台规则':'暂无符合条件的后台提醒' }}</p>
    <article class="pa-item" v-for="a in visibleItems" :key="a.id" :data-alert-id="a.id" :data-selected="a.id===selectedID"><div class="pa-item-heading"><div><h3>{{ a.rule.name }} <small>{{ a.rule.symbol }}</small></h3><strong>{{ a.rule.direction==='above'?'≥':'≤' }} {{ money(a.rule.price) }}</strong><span class="pa-phase" :class="a.phase">{{ phaseLabel(a.phase) }}</span></div><div class="pa-actions"><button class="icon-button" :title="a.enabled?'暂停提醒':'启用提醒'" :aria-label="a.enabled?'暂停提醒':'启用提醒'" :disabled="busy||!available||['triggered','expired'].includes(a.phase)" @click="control(a,'enable')"><pause v-if="a.enabled" :size="15"/><play v-else :size="15"/></button><button class="icon-button" title="复制为新提醒" aria-label="复制为新提醒" :disabled="busy||!available" @click="duplicate(a)"><copy :size="15"/></button><button class="icon-button" title="删除后台提醒" aria-label="删除后台提醒" :disabled="busy||!available" @click="control(a,'delete')"><trash-2 :size="15"/></button></div></div>
      <div class="pa-facts"><div><span>最近报价</span><b>{{ money(a.quote_price) }}</b><small>{{ time(a.quote_at) }}</small></div><div><span>检查结果</span><b>{{ a.check_detail||'等待检查' }}</b><small>{{ time(a.last_checked_at) }}</small></div><div><span>有效期 / 来源</span><b>{{ a.rule.expires_on }}</b><small>{{ sourceLabel(a.rule) }}</small></div><div><span>系统通知</span><b>{{ deliveryLabel(a) }}</b><small v-if="a.notification?.last_error" class="pa-error">{{ a.notification.last_error }}</small></div></div>
      <div class="pa-trigger" v-if="a.event"><check :size="15"/><span>{{ time(a.event.quote_at) }} · {{ money(a.event.price) }}</span><a :href="'/?view=monitor&monitor=native&event='+encodeURIComponent(a.event.id)">通知记录 <arrow-up-right :size="13"/></a></div>
    </article>
    <dialog class="pa-dialog" v-if="dialogOpen" ref="dialog" aria-labelledby="pa-dialog-title" @cancel.prevent="close"><div class="pa-heading"><h3 id="pa-dialog-title">后台价位提醒草案</h3><button class="icon-button" aria-label="关闭后台提醒草案" title="关闭" :disabled="busy" @click="close"><x :size="16"/></button></div>
      <label>股票<input v-model="form.symbol" :readonly="!!form.source" aria-label="提醒股票" placeholder="代码或名称" maxlength="40"></label><label>名称<input v-model="form.name" aria-label="提醒名称" maxlength="80"></label><label>条件<select v-model="form.direction" aria-label="后台提醒条件"><option value="above">达到或高于</option><option value="below">达到或低于</option></select></label><label>价位<input type="number" min="0.01" max="100000000" step="0.01" v-model="form.price" aria-label="后台提醒价位"></label><label>有效期<input type="date" v-model="form.expires_on" aria-label="后台提醒有效期"></label>
      <p class="pa-muted">{{ sourceLabel(form) }} · 单次触发 · 当前报价</p><p class="pa-muted">由工作台服务检查，关闭页面后继续；Mac 需保持登录、唤醒和联网。</p><p class="pa-notice" v-if="!nativeEnabled">系统通知未开启，触发事件仍保存到工作台。</p><p v-if="formError" class="pa-error" role="alert">{{ formError }}</p>
      <div class="pa-dialog-actions"><button type="button" :disabled="busy" @click="close">取消</button><button type="button" :disabled="busy" @click="confirm"><bell-plus :size="15"/>{{ busy?'正在保存…':'确认并启用后台提醒' }}</button></div>
    </dialog>
  </section>`,
}
