import { Play, Pause, RefreshCw, Download, FolderOpen } from 'lucide-vue-next'
import { validationPriceBasis, validationActionState } from './pattern-validation.mjs'
import { PatternForwardResultsView } from './pattern-forward-results-view.js'
import { PatternForwardHealthView } from './pattern-forward-health-view.js'

export const PatternForwardView = {
  components:{Play,Pause,RefreshCw,Download,FolderOpen,PatternForwardResultsView,PatternForwardHealthView}, emits:['manage-pools'],
  data(){return {pools:[],items:[],poolVersion:'',taskID:'',name:'',priceBasis:'hfq',date:'',detail:null,error:'',busy:false,closed:false,timer:null,controller:null,detailController:null,detailSequence:0,loadedKey:'',dataView:'observations',runtime:null}},
  computed:{
    selected(){return this.items.find(v=>v.task.id===this.taskID)},
    day(){return this.selected?.days.find(d=>d.date===this.date)},
    captured(){return this.day?.items.filter(i=>i.state==='captured').length || 0},
    sampleCount(){return this.day?.items.reduce((n,i)=>n+(i.sample_ids?.length || 0),0) || 0},
    exportURL(){return this.taskID && this.date ? '/api/pattern-forwards?'+new URLSearchParams({id:this.taskID,date:this.date,download:'1'}) : ''},
  },
  methods:{
    basis:validationPriceBasis,actionStatus:validationActionState,
    state(value){return {captured:'已留存',open:'等待完成',partial:'部分缺采',missed:'缺采',pending:'待采集',missing:'缺采'}[value] || value},
    time(value){return !value || value.startsWith('0001') ? '--' : new Date(value).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false})},
    number(value){return value==null ? '--' : Number(value).toFixed(2)},
    clock(minutes){return String(Math.floor(minutes/60)).padStart(2,'0')+':'+String(minutes%60).padStart(2,'0')},
    stockName(symbol){return this.selected?.task.pool.members.find(m=>m.symbol===symbol)?.name || ''},
    snapshot(symbol){return this.detail?.stocks.find(s=>s.symbol===symbol)},
    async api(url,options={}){const r=await fetch(url,{cache:'no-store',...options}),p=await r.json();if(!r.ok)throw new Error(p.error || '前向研究请求失败');return p},
    async refresh(){
      clearTimeout(this.timer);this.controller?.abort();this.controller=new AbortController()
      try{const p=await this.api('/api/pattern-forwards',{signal:this.controller.signal});if(this.closed)return;this.items=p.items || [];this.runtime=p.runtime;if(!this.taskID)this.taskID=this.items[0]?.task.id || '';if(!this.selected?.days.some(d=>d.date===this.date))this.date=this.selected?.days[0]?.date || '';await this.loadDay()}
      catch(e){if(e.name!=='AbortError')this.error=e.message}
      finally{if(!this.closed)this.timer=setTimeout(()=>this.refresh(),5000)}
    },
    async loadDay(){
      const key=this.taskID+'|'+this.date+'|'+JSON.stringify(this.day || null)
      if(key===this.loadedKey)return
      this.detailController?.abort();const sequence=++this.detailSequence;this.detail=null
      if(!this.taskID || !this.date){this.loadedKey=key;return}
      const controller=new AbortController();this.detailController=controller
      try{const p=await this.api('/api/pattern-forwards?'+new URLSearchParams({id:this.taskID,date:this.date}),{signal:controller.signal});if(sequence===this.detailSequence){this.detail=p;this.loadedKey=key}}
      catch(e){if(e.name!=='AbortError' && sequence===this.detailSequence)this.error=e.message}
    },
    selectTask(){this.date=this.selected?.days[0]?.date || '';this.loadedKey='';this.loadDay()},
    async command(action){
      this.busy=true;this.error=''
      const body=action==='create'?{action,pool_version:this.poolVersion,name:this.name,price_basis:this.priceBasis}:{action,id:this.taskID}
      try{const p=await this.api('/api/pattern-forwards',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});if(p.task){this.taskID=p.task.id;this.date='';this.loadedKey=''};await this.refresh()}
      catch(e){this.error=e.message}finally{this.busy=false}
    },
  },
  async mounted(){try{const p=await this.api('/api/pattern-pools');this.pools=p.items || [];this.poolVersion=this.pools[0]?.version || '';await this.refresh()}catch(e){this.error=e.message}},
  beforeUnmount(){this.closed=true;clearTimeout(this.timer);this.controller?.abort();this.detailController?.abort()},
  template:`<section class="pv-forward" aria-label="前向观察">
    <div class="pv-section-title"><h3>前向研究任务</h3><div class="pv-job-actions"><button type="button" :disabled="busy" @click="$emit('manage-pools')"><folder-open :size="14"/>管理股票池</button><button type="button" class="icon-button" aria-label="刷新前向任务" title="刷新前向任务" @click="refresh"><refresh-cw :size="15"/></button></div></div>
    <form class="pv-form" @submit.prevent="command('create')"><label class="pv-symbols">股票池版本<select v-model="poolVersion" aria-label="前向股票池版本" required :disabled="busy"><option value="">选择冻结股票池</option><option v-for="p in pools" :key="p.version" :value="p.version">{{ p.name }} · {{ p.members.length }}只 · {{ p.version.slice(0,8) }}</option></select></label><label>任务名称<input v-model="name" aria-label="前向任务名称" maxlength="60" placeholder="前向研究" :disabled="busy"></label><label>价格口径<select v-model="priceBasis" aria-label="前向价格口径" :disabled="busy"><option value="hfq">后复权研究</option><option value="raw">未复权</option></select></label><button type="submit" :disabled="busy || !poolVersion"><play :size="14"/>创建前向任务</button></form>
    <p v-if="error" role="alert" class="pv-action-warning">{{ error }}</p>
    <label class="pv-job-select">研究任务<select v-model="taskID" aria-label="前向研究任务" @change="selectTask"><option value="">暂无任务</option><option v-for="v in items" :key="v.task.id" :value="v.task.id">{{ v.task.name }} · {{ time(v.task.created_at) }} · {{ v.control.enabled ? '已启用' : '已暂停' }}</option></select></label>
    <template v-if="selected"><div class="pv-forward-status" :data-forward-enabled="selected.control.enabled"><strong>{{ selected.control.enabled ? '已启用' : '已暂停' }} · {{ selected.task.pool.members.length }}只 · {{ basis(selected.task.config.price_basis) }}</strong><p>冻结 {{ time(selected.task.created_at) }} · 最早采集日 {{ selected.task.config.start_on }}</p><p>上海时间 {{ clock(selected.task.config.open_minute) }} 至 {{ clock(selected.task.config.close_minute) }} · 重试间隔 {{ selected.task.config.retry_minutes }}分钟</p><p v-if="selected.status.calendar_start">{{ selected.status.calendar_source }} · {{ selected.status.calendar_start }} 至 {{ selected.status.calendar_end }}</p><p v-if="selected.status.error" class="pv-action-warning">{{ selected.status.error }}</p><div class="pv-job-actions"><button type="button" :disabled="busy" @click="command(selected.control.enabled ? 'pause' : 'enable')"><pause v-if="selected.control.enabled" :size="14"/><play v-else :size="14"/>{{ selected.control.enabled ? '暂停前向任务' : '启用前向任务' }}</button><button type="button" :disabled="busy || !selected.can_collect" @click="command('collect')"><refresh-cw :size="14"/>检查收盘采集</button></div><details class="pv-method"><summary>冻结配置</summary><p>{{ selected.task.pool.name }} · {{ selected.task.pool.source }}</p><p>{{ selected.task.config.version }} · {{ selected.task.config.engine_version }} · {{ selected.task.config.patterns.length }}种形态 · {{ selected.task.config.pattern_version }}</p><p>固定历史起点 {{ selected.task.config.history_start }} · 预热 {{ selected.task.config.warmup_days }}天 · 回看 {{ selected.task.config.lookback_bars }}根 · 确认期限 {{ selected.task.config.confirmation_bars }}根</p><p>{{ selected.task.config.price_method }} · {{ selected.task.config.volume_method }} · {{ selected.task.config.quality_method }}</p><p class="pv-hash">股票池 {{ selected.task.pool.version }}</p><p class="pv-hash">配置 {{ selected.task.config_hash }}</p></details></div>
    <div class="pv-run-modes" role="group" aria-label="前向数据视图"><button type="button" :aria-pressed="dataView==='observations'" @click="dataView='observations'">观察快照</button><button type="button" :aria-pressed="dataView==='results'" @click="dataView='results'">结果跟踪</button><button type="button" :aria-pressed="dataView==='health'" @click="dataView='health'">运行监控</button></div>
    <pattern-forward-results-view v-if="dataView==='results'" :key="taskID" :task="selected"/>
    <pattern-forward-health-view v-else-if="dataView==='health'" :key="taskID" :record="selected" :runtime="runtime" @refresh="refresh" @open-day="date=$event; dataView='observations'; loadDay()"/>
    <template v-else><div class="pv-section-title"><h3>收盘观察快照</h3><a v-if="exportURL" :href="exportURL" class="icon-button" title="导出当日快照" aria-label="导出当日快照"><download :size="15"/></a></div>
    <label class="pv-job-select">采集日<select v-model="date" aria-label="前向采集日" @change="loadDay"><option value="">暂无采集记录</option><option v-for="d in selected.days" :key="d.date" :value="d.date">{{ d.date }} · {{ state(d.state) }}</option></select></label>
    <div v-if="day" class="pv-forward-day" :data-forward-day-state="day.state"><div class="pv-totals"><div><span>已留存 / 股票池</span><strong>{{ captured }} / {{ day.items.length }}</strong></div><div><span>首次观察样本</span><strong>{{ sampleCount }}</strong></div><div><span>缺采或待完成</span><strong>{{ day.items.length-captured }}</strong></div><div><span>当日状态</span><strong>{{ state(day.state) }}</strong></div></div><p class="pv-study-basis">{{ day.calendar_source }} · 最近尝试 {{ time(day.last_attempt_at) }} · 封存 {{ time(day.sealed_at) }}</p><p v-if="day.error" class="pv-action-warning">{{ day.error }}</p>
      <details v-for="item in day.items" :key="item.symbol" class="pv-observation" :data-observation-state="item.state"><summary><strong>{{ item.symbol }} {{ stockName(item.symbol) }}</strong><span>{{ state(item.state) }} · {{ item.sample_ids.length }}个新样本</span></summary><p v-if="item.error" class="pv-action-warning">{{ item.error }}</p><template v-if="snapshot(item.symbol)"><p>留存 {{ time(snapshot(item.symbol).collected_at) }} · {{ snapshot(item.symbol).coverage.source }}</p><p v-if="!snapshot(item.symbol).samples.length">当日未识别到新形态</p><div v-for="sample in snapshot(item.symbol).samples" :key="sample.id" class="pv-forward-sample"><strong>{{ sample.name }} · {{ sample.observed_on }}</strong><p>{{ actionStatus(sample.action_check, !!snapshot(item.symbol).adjustment?.quality_method) }}</p><p>{{ basis(selected.task.config.price_basis) }}点位 · 确认 {{ number(sample.snapshot.pattern.trigger_price) }} · 失效 {{ number(sample.snapshot.pattern.invalidation_price) }}</p><p v-for="evidence in sample.snapshot.evidence" :key="evidence">{{ evidence }}</p><p class="pv-hash">样本 {{ sample.id }}</p></div><p class="pv-hash">输入 {{ snapshot(item.symbol).input_hash }}</p><p class="pv-hash">快照 {{ item.snapshot_hash }}</p></template></details>
    </div><p v-else class="pv-empty">尚无当日观察记录</p></template></template>
  </section>`,
}
