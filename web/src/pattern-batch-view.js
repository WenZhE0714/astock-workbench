import { Play, Square, RefreshCw, Save, Pencil, ArrowUpRight } from 'lucide-vue-next'
import { validationSymbols, validationPriceBasis } from './pattern-validation.mjs'
import { shanghaiToday } from './review-calendar.mjs'

const seed = [
  ['600519','贵州茅台','食品饮料'],['000858','五粮液','食品饮料'],['600036','招商银行','银行'],['601398','工商银行','银行'],
  ['601318','中国平安','非银金融'],['600030','中信证券','非银金融'],['600276','恒瑞医药','医药生物'],['000538','云南白药','医药生物'],
  ['300750','宁德时代','电力设备'],['601012','隆基绿能','电力设备'],['002594','比亚迪','汽车'],['600104','上汽集团','汽车'],
  ['000333','美的集团','家用电器'],['000651','格力电器','家用电器'],['600900','长江电力','公用事业'],['601088','中国神华','煤炭'],
  ['601857','中国石油','石油石化'],['600019','宝钢股份','钢铁'],['002415','海康威视','电子'],['002080','中材科技','建筑材料'],
]

export const PatternBatchView = {
  components:{Play,Square,RefreshCw,Save,Pencil,ArrowUpRight}, props:['initial'], emits:['open-run'],
  data() {
    const today=shanghaiToday(),start=new Date(today+'T12:00:00Z'); start.setUTCFullYear(start.getUTCFullYear()-1)
    return {pools:[],jobs:[],poolVersion:'',jobID:'',editing:false,codes:'',name:'',source:'',members:[],error:'',busy:false,timer:null,controller:null,closed:false,
      start:this.initial?.start || start.toISOString().slice(0,10),end:this.initial?.end || today,priceBasis:this.initial?.price_basis || 'hfq',validationStart:this.initial?.validation_start || '',split:!!this.initial?.validation_start,today}
  },
  computed:{
    pool(){return this.pools.find(p=>p.version===this.poolVersion)},
    job(){return this.jobs.find(j=>j.id===this.jobID)},
    active(){return this.jobs.some(j=>['running','cancelling'].includes(j.status))},
    finished(){return this.job?.items.filter(i=>['ready','failed'].includes(i.state)).length || 0},
    failed(){return this.job?.items.filter(i=>i.state==='failed').length || 0},
  },
  methods:{
    basis:validationPriceBasis,
    status(value){return {running:'运行中',cancelling:'停止中',cancelled:'已取消',paused:'等待续跑',completed:'已完成',partial:'部分失败',failed:'失败',pending:'待处理',ready:'已保存'}[value] || value},
    phase(value){return {benchmark:'冻结基准',fetching:'采集检查点',validating:'计算验证结果',complete:'归档完成'}[value] || '待启动'},
    time(value){return new Date(value).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false})},
    async api(url,options={}){const r=await fetch(url,{cache:'no-store',...options}),p=await r.json();if(!r.ok)throw new Error(p.error || '请求失败');return p},
    async refresh(){
      clearTimeout(this.timer);this.controller?.abort();this.controller=new AbortController()
      try {const p=await this.api('/api/pattern-batches',{signal:this.controller.signal});if(this.closed)return;this.jobs=p.items || [];if(!this.jobID)this.jobID=this.jobs[0]?.id || ''}
      catch(e){if(e.name!=='AbortError')this.error=e.message}
      finally{if(!this.closed && this.active)this.timer=setTimeout(()=>this.refresh(),1500)}
    },
    async loadPools(){const p=await this.api('/api/pattern-pools');this.pools=p.items || [];if(!this.poolVersion)this.poolVersion=this.pools[0]?.version || '';if(!this.pools.length)this.editing=true},
    syncMembers(){const old=new Map(this.members.map(m=>[m.symbol,m]));this.members=validationSymbols(this.codes).map(symbol=>old.get(symbol) || {symbol,name:'',industry:''})},
    preset(){this.name='跨行业研究示例20只';this.source='内置人工示例；行业标签为研究分组，非官方分类；当前静态名单，存在选择与存续偏差';this.members=seed.map(([symbol,name,industry])=>({symbol,name,industry}));this.codes=this.members.map(m=>m.symbol).join(', ');this.editing=true},
    edit(){this.name=this.pool?.name || '';this.source=this.pool?.source || '';this.members=(this.pool?.members || []).map(m=>({...m}));this.codes=this.members.map(m=>m.symbol).join(', ');this.editing=true},
    async savePool(){
      this.syncMembers();this.busy=true;this.error=''
      try{const pool=await this.api('/api/pattern-pools',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:this.name,source:this.source,members:this.members})});this.poolVersion=pool.version;await this.loadPools();this.editing=false}
      catch(e){this.error=e.message}finally{this.busy=false}
    },
    async command(action,id){
      this.busy=true;this.error=''
      const body=action==='create'?{action,pool_version:this.poolVersion,request:{start:this.start,end:this.end,price_basis:this.priceBasis,validation_start:this.split?this.validationStart:undefined}}:{action,id}
      try{const result=await this.api('/api/pattern-batches',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});if(result.id)this.jobID=result.id;await this.refresh()}
      catch(e){this.error=e.message}finally{this.busy=false}
    },
  },
  async mounted(){try{await this.loadPools();await this.refresh()}catch(e){this.error=e.message}},
  beforeUnmount(){this.closed=true;clearTimeout(this.timer);this.controller?.abort()},
  template:`<section class="pv-batch" aria-label="股票池与批量验证">
    <div class="pv-section-title"><h3>冻结股票池</h3><button type="button" class="icon-button" aria-label="刷新批量任务" title="刷新批量任务" @click="refresh"><refresh-cw :size="15"/></button></div>
    <div class="pv-pool-select"><select v-model="poolVersion" aria-label="冻结股票池版本"><option value="">选择股票池</option><option v-for="p in pools" :key="p.version" :value="p.version">{{ p.name }} · {{ p.members.length }}只 · {{ p.version.slice(0,8) }}</option></select><button type="button" @click="edit" :disabled="busy"><pencil :size="14"/>{{ pool ? '编辑为新版本' : '新建股票池' }}</button><button type="button" @click="preset" :disabled="busy">跨行业20只示例</button></div>
    <p v-if="pool" class="pv-study-basis">{{ pool.source }} · 冻结 {{ time(pool.created_at) }}</p>
    <form v-if="editing" class="pv-pool-editor" @submit.prevent="savePool">
      <label>名称<input v-model="name" aria-label="股票池名称" maxlength="80" required></label><label>名单与分组来源<input v-model="source" aria-label="股票池来源" maxlength="600" required></label>
      <label class="pv-pool-codes">股票代码<textarea v-model="codes" @change="syncMembers" aria-label="股票池代码" placeholder="600519, 000858" required rows="3"></textarea></label>
      <details v-if="members.length" class="pv-pool-labels"><summary>名称与行业标签 · {{ members.length }}只</summary><div v-for="m in members" :key="m.symbol"><span>{{ m.symbol }}</span><input v-model="m.name" :aria-label="m.symbol+' 名称'" placeholder="名称" maxlength="80"><input v-model="m.industry" :aria-label="m.symbol+' 行业标签'" placeholder="行业标签" maxlength="80"></div></details>
      <button type="submit" :disabled="busy"><save :size="14"/>保存股票池版本</button><button type="button" @click="editing=false" :disabled="busy">收起</button>
    </form>
    <form class="pv-form" @submit.prevent="command('create')"><label>开始日期<input type="date" v-model="start" aria-label="批量开始日期" :max="end" required></label><label>结束日期<input type="date" v-model="end" aria-label="批量结束日期" :min="start" :max="today" required></label><label>价格口径<select v-model="priceBasis" aria-label="批量价格口径"><option value="raw">未复权</option><option value="hfq">后复权研究</option></select></label><label class="pv-study-toggle"><input type="checkbox" v-model="split" aria-label="批量分期评估">分期评估</label><label v-if="split">验证期起点<input type="date" v-model="validationStart" aria-label="批量验证期起点" :min="start" :max="end" required></label><button type="submit" :disabled="busy || active || !poolVersion"><play :size="14"/>启动批量验证</button></form>
    <p v-if="error" role="alert" class="pv-action-warning">{{ error }}</p>
    <label class="pv-job-select">批量任务<select v-model="jobID" aria-label="批量任务"><option value="">暂无任务</option><option v-for="j in jobs" :key="j.id" :value="j.id">{{ time(j.created_at) }} · {{ j.pool.name }} · {{ status(j.status) }}</option></select></label>
    <div v-if="job" class="pv-job" :data-job-status="job.status"><div class="pv-section-title"><strong>{{ status(job.status) }} · {{ phase(job.phase) }}</strong><span>{{ job.current_symbol || '' }}</span></div><progress :value="finished" :max="job.items.length" aria-label="批量数据检查点进度"></progress><p class="pv-study-basis">检查点 {{ finished }} / {{ job.items.length }} · 失败 {{ failed }} · {{ job.request.start }} 至 {{ job.request.end }} · {{ basis(job.request.price_basis) }}</p><p v-if="job.request.validation_start" class="pv-study-basis">验证期起点 {{ job.request.validation_start }}</p><p v-if="job.error" class="pv-action-warning">{{ job.error }}</p>
      <div class="pv-job-actions"><button v-if="job.status==='running'" type="button" :disabled="busy" @click="command('cancel',job.id)"><square :size="14"/>取消任务</button><button v-if="['paused','cancelled','failed'].includes(job.status)" type="button" :disabled="busy || active" @click="command('resume',job.id)"><play :size="14"/>继续任务</button><button v-if="failed && ['partial','failed','cancelled','paused'].includes(job.status)" type="button" :disabled="busy || active" @click="command('retry-failed',job.id)"><refresh-cw :size="14"/>重试失败项</button><button v-for="(id,i) in job.run_ids" :key="id" type="button" :aria-label="'查看批量归档 '+(i+1)" @click="$emit('open-run',id)"><arrow-up-right :size="14"/>验证归档 {{ i+1 }}</button></div>
      <details open class="pv-job-items"><summary>逐股票状态</summary><div v-for="item in job.items" :key="item.symbol"><strong>{{ item.symbol }}</strong><span>{{ status(item.state) }} · 尝试 {{ item.attempts }}</span><small v-if="item.error" class="pv-action-warning">{{ item.error }}</small></div></details><p class="pv-hash">股票池 {{ job.pool.version }} · 配置 {{ job.config_hash }}</p>
    </div>
  </section>`,
}
