import { RefreshCw, Download, ChevronLeft, ChevronRight } from 'lucide-vue-next'
import { validationPatterns, validationPercent, validationPoints, validationState, validationOutcome, validationActionState, validationTone } from './pattern-validation.mjs'

export const PatternForwardResultsView = {
  components:{RefreshCw,Download,ChevronLeft,ChevronRight}, props:['task'],
  data(){return {view:null,error:'',loading:false,horizon:5,pattern:'',bias:'',state:'',cutoff:'',offset:0,patterns:validationPatterns,controller:null,sequence:0,histories:{},historyErrors:{},historyControllers:new Map(),closed:false}},
  computed:{
    query(){return {id:this.task.task.id,tracking:'1',horizon:String(this.horizon),pattern:this.pattern,bias:this.bias,state:this.state,date:this.cutoff,offset:String(this.offset)}},
    filterKey(){return [this.task.task.id,this.horizon,this.pattern,this.bias,this.state,this.cutoff].join('|')},
    requestKey(){return this.filterKey+'|'+this.offset+'|'+(this.task.status.tracking_through || '')},
    exportURL(){return '/api/pattern-forwards?'+new URLSearchParams({id:this.task.task.id,tracking:'1',download:'1',date:this.cutoff})},
    totals(){return this.view?.totals || {}},
  },
  watch:{filterKey(){this.offset=0},requestKey(){this.load()}},
  methods:{
    percent:validationPercent,points:validationPoints,status:validationState,action:validationActionState,tone:validationTone,
    outcome(record){return validationOutcome(record.sample,this.horizon)},
    stockName(symbol){return this.task.task.pool.members.find(m=>m.symbol===symbol)?.name || symbol},
    async api(query,signal){const response=await fetch('/api/pattern-forwards?'+new URLSearchParams(query),{cache:'no-store',signal});const data=await response.json();if(!response.ok)throw new Error(data.error || '读取前向跟踪失败');return data},
    async load(){
      this.controller?.abort();this.controller=new AbortController();const sequence=++this.sequence
      for(const controller of this.historyControllers.values())controller.abort()
      this.historyControllers.clear();this.histories={};this.historyErrors={};this.loading=true;this.error='';this.view=null
      try{const view=await this.api(this.query,this.controller.signal);if(sequence===this.sequence && !this.closed)this.view=view}
      catch(e){if(e.name!=='AbortError' && sequence===this.sequence)this.error=e.message}
      finally{if(sequence===this.sequence)this.loading=false}
    },
    async history(id,event){
      if(!event.target.open || this.histories[id] || this.historyControllers.has(id))return
      const controller=new AbortController(),sequence=this.sequence;this.historyControllers.set(id,controller);delete this.historyErrors[id]
      try{const data=await this.api({id:this.task.task.id,tracking:'1',sample:id,date:this.cutoff},controller.signal);if(sequence===this.sequence && !this.closed)this.histories[id]=data.history}
      catch(e){if(e.name!=='AbortError' && sequence===this.sequence)this.historyErrors[id]=e.message}
      finally{if(this.historyControllers.get(id)===controller)this.historyControllers.delete(id)}
    },
  },
  mounted(){this.load()},
  beforeUnmount(){this.closed=true;this.sequence++;this.controller?.abort();for(const controller of this.historyControllers.values())controller.abort()},
  template:`<section class="pv-forward-results" aria-label="前向结果跟踪" :aria-busy="loading">
    <div class="pv-section-title"><h3>前向结果跟踪</h3><div class="pv-job-actions"><button type="button" class="icon-button" title="刷新前向结果" aria-label="刷新前向结果" :disabled="loading" @click="load"><refresh-cw :size="15"/></button><a :href="exportURL" class="icon-button" title="导出前向跟踪日志" aria-label="导出前向跟踪日志"><download :size="15"/></a></div></div>
    <p v-if="task.status.tracking_error" role="alert" class="pv-action-warning">{{ task.status.tracking_error }}</p><p v-if="error" role="alert" class="pv-action-warning">{{ error }}</p>
    <div class="pv-filters"><div class="dashboard-segment" role="group" aria-label="前向结果窗口"><button v-for="days in [5,10,20]" :key="days" type="button" :aria-pressed="horizon===days" :class="{active:horizon===days}" @click="horizon=days">{{ days }}日</button></div><select v-model="pattern" aria-label="前向形态筛选"><option value="">全部形态</option><option v-for="item in patterns" :key="item[0]" :value="item[0]">{{ item[1] }}</option></select><select v-model="bias" aria-label="前向方向筛选"><option value="">全部方向</option><option value="bullish">看涨</option><option value="bearish">看跌</option></select><select v-model="state" aria-label="前向状态筛选"><option value="">全部状态</option><option value="watching">待确认</option><option value="forming">突破待确认</option><option value="confirmed">已确认</option><option value="invalidated">已失效</option><option value="expired">观察到期</option><option value="unavailable">数据缺失</option></select><select v-model="cutoff" aria-label="前向结果截止日"><option value="">最新结果</option><option v-for="day in task.days.filter(d=>d.sealed_at && !d.sealed_at.startsWith('0001'))" :key="day.date" :value="day.date">截至 {{ day.date }}</option></select></div>
    <template v-if="view"><p class="pv-study-basis">跟踪截至 {{ view.through || '--' }} · 沪深300交易日窗口 · 确认日收盘起算</p><div class="pv-totals"><div><span>样本 / 已确认</span><strong>{{ totals.samples }} / {{ totals.confirmed }}</strong></div><div><span>纳入统计 / 待成熟</span><strong>{{ totals.mature }} / {{ totals.pending }}</strong></div><div><span>数据缺失</span><strong>{{ totals.unavailable }}</strong></div><div><span>事件隔离 / 未核验</span><strong>{{ totals.action_affected }} / {{ totals.action_unverified }}</strong></div><div><span>平均价格涨跌</span><strong :class="tone(totals.average_price_return)">{{ percent(totals.average_price_return) }}</strong></div><div><span>配对样本</span><strong>{{ totals.benchmark_paired }}</strong></div><div><span>平均沪深300涨跌</span><strong :class="tone(totals.average_benchmark_return)">{{ percent(totals.average_benchmark_return) }}</strong></div><div><span>平均价格超额</span><strong :class="tone(totals.average_excess_return)">{{ totals.average_excess_return==null ? '--' : Number(totals.average_excess_return).toFixed(2)+' pp' }}</strong></div></div>
      <details v-for="record in view.records" :key="record.sample.id" class="pv-forward-result" :data-result-state="record.sample.state" :data-result-outcome="outcome(record).state" @toggle="history(record.sample.id,$event)"><summary><span><strong>{{ stockName(record.sample.symbol) }} · {{ record.sample.name }}</strong><small>{{ record.sample.symbol }} · 观察 {{ record.sample.observed_on }} · {{ status(record.sample.state) }}</small></span><span><strong :class="tone(outcome(record).price_return)">{{ percent(outcome(record).price_return) }}</strong><small>{{ horizon }}日 · {{ status(outcome(record).state) }} · {{ outcome(record).observed_days || 0 }}/{{ horizon }}</small></span></summary>
        <div class="pv-forward-result-facts"><p>确认 {{ record.sample.confirmed_on || '--' }} · 失效 {{ record.sample.invalidated_on || '--' }} · 到期 {{ record.sample.expired_on || '--' }}</p><p>窗口末日 {{ outcome(record).through || '--' }} · {{ action(outcome(record).action_check,task.task.config.price_basis==='hfq') }}</p><p>价格涨跌 {{ percent(outcome(record).price_return) }} · 方向表现 {{ percent(outcome(record).direction_return) }}</p><p>沪深300 {{ percent(outcome(record).benchmark?.return) }} · 价格超额 {{ points(outcome(record).benchmark?.excess_return) }}</p><p v-if="outcome(record).detail" class="pv-action-warning">{{ outcome(record).detail }}</p><p v-if="outcome(record).benchmark?.detail" class="pv-action-warning">{{ outcome(record).benchmark.detail }}</p><p v-if="record.detail" class="pv-action-warning">{{ record.detail }}</p><p v-if="record.missing_dates.length">缺采或口径不连续 {{ record.missing_dates.join('、') }}</p><p class="pv-hash">原始观察 {{ record.observation_hash }}</p></div>
        <h4>逐日记录</h4><p v-if="historyErrors[record.sample.id]" role="alert">{{ historyErrors[record.sample.id] }}</p><p v-else-if="!histories[record.sample.id]">正在读取记录</p><ol v-else class="pv-forward-history"><li v-for="entry in histories[record.sample.id]" :key="entry.as_of"><time>{{ entry.as_of }}</time><span>{{ status(entry.sample.state) }} · {{ status(outcome(entry).state) }}<small v-if="entry.detail">{{ entry.detail }}</small></span></li></ol>
      </details><p v-if="!view.records.length" class="pv-empty">暂无符合条件的跟踪样本</p>
      <div class="pv-pagination"><span>{{ view.total }}个样本 · 第 {{ Math.floor(view.filter.offset/50)+1 }} / {{ Math.max(1,Math.ceil(view.total/50)) }} 页</span><button type="button" class="icon-button" title="上一页" aria-label="前向结果上一页" :disabled="!offset || loading" @click="offset=Math.max(0,offset-50)"><chevron-left :size="16"/></button><button type="button" class="icon-button" title="下一页" aria-label="前向结果下一页" :disabled="offset+50>=view.total || loading" @click="offset+=50"><chevron-right :size="16"/></button></div>
      <p class="pv-study-basis">价格表现未计交易费用与成交约束；看跌方向表现不代表可做空收益。超额为个股与沪深300价格指数的涨跌差。</p><p class="pv-hash">日志 {{ view.hash || '--' }}</p>
    </template><p v-else-if="loading" class="pv-empty">正在读取前向结果</p>
  </section>`,
}
