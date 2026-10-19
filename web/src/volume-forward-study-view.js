import { RefreshCw, Download, LockKeyhole } from 'lucide-vue-next'
import { finiteNumber } from './dashboard.mjs'

export const VolumeForwardStudyView = {
  components:{RefreshCw,Download,LockKeyhole},
  emits:['status'],
  data(){return {report:null,configured:null,loading:false,error:'',tab:'progress',controller:null,timer:null}},
  computed:{
    plan(){return this.report?.plan},
    parameters(){return this.plan?.candidate?.technical || {}},
    policy(){return this.plan?.policy || {}},
    candidate(){return this.report?.groups?.find(row=>row.id==='candidate') || {}},
    phase(){if(!this.report)return '';if(this.report.sample_ready)return '样本达标 · 待复核';return this.report.sealed_days?'前瞻观察中':'已冻结 · 等待首个观察日'},
    dayProgress(){return Math.min(100,(this.report?.sealed_days || 0)/Math.max(1,this.policy.minimum_trading_days || 60)*100)},
    tradeProgress(){return Math.min(100,(this.candidate.trades || 0)/Math.max(1,this.policy.minimum_trades || 10)*100)},
    stockNames(){return (this.plan?.candidate?.tickers || []).map(symbol=>({symbol,name:this.plan.candidate.names?.[symbol] || symbol}))},
  },
  methods:{
    percent(value){const n=finiteNumber(value);return n===null?'--':n.toFixed(2)+'%'},
    ratio(value){const n=finiteNumber(value);return n===null?'--':(n*100).toFixed(0)+'%'},
    money(value){const n=finiteNumber(value);return n===null?'--':n.toLocaleString('zh-CN',{maximumFractionDigits:2})},
    time(value){if(!value||value.startsWith('0001'))return '--';return new Date(value).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false})},
    date(value){return value?.slice(0,10) || '--'},
    async load(){
      this.controller?.abort();const controller=new AbortController();this.controller=controller;this.loading=true
      try{
        const response=await fetch('/api/strategy/volume-forward',{cache:'no-store',signal:controller.signal});const data=await response.json()
        if(!response.ok){const error=Error(data.error || '前瞻研究读取失败');error.status=response.status;throw error}
        this.configured=data.configured;this.report=data.configured?data:null;this.error=''
        this.$emit('status',data.configured?(data.status?.status==='error'?'P008前瞻采集异常':data.sealed_days?'P008前瞻观察中':'P008参数已冻结'):'')
      }catch(error){if(error.name!=='AbortError'){this.configured=true;this.error=error.message;if(error.status!==409)this.report=null;this.$emit('status',error.status===409?'P008前瞻采集中':'前瞻归档待核验')}}
      finally{if(this.controller===controller)this.loading=false}
    },
  },
  mounted(){this.load();this.timer=setInterval(()=>this.load(),30000)},
  beforeUnmount(){clearInterval(this.timer);this.controller?.abort()},
  template:`<section class="volume-forward-study" v-if="configured!==false" aria-label="已冻结的P008前瞻实验" :aria-busy="loading">
    <header class="vf-heading"><div><lock-keyhole :size="18"/><h2>P008 量比前瞻</h2><span v-if="report" class="vf-phase">{{ phase }}</span></div><div class="vf-actions"><a v-if="report" class="icon-button" href="/api/strategy/volume-forward?download=1" download title="导出前瞻摘要" aria-label="导出前瞻摘要"><download :size="16"/></a><button class="icon-button" type="button" title="刷新前瞻状态" aria-label="刷新前瞻状态" :disabled="loading" @click="load"><refresh-cw :size="16"/></button></div></header>
    <p v-if="error" role="alert" class="vf-error">{{ error }}</p><p class="vf-empty" v-if="loading && !report">正在核验冻结配置与封存记录</p>
    <template v-if="report && plan"><div class="vf-origin"><span>冻结 {{ time(plan.created_at) }}</span><span>源研究 {{ plan.source_id }} / {{ plan.candidate_id }}</span><span>{{ stockNames.length }}只股票</span><span>独立前瞻 · 未晋级</span></div>
      <div class="vf-parameters"><div><span>单股上限</span><b>{{ ratio(parameters.max_position) }}</b></div><div><span>追加信号量比</span><b>≥ {{ plan.candidate.entry_volume_floor }} 倍</b></div><div><span>趋势均线</span><b>MA{{ parameters.fast_ma }} / {{ parameters.slow_ma }}</b></div><div><span>原量能基线</span><b>{{ parameters.volume_ratio_min }} 倍</b></div><div><span>止损 / 止盈</span><b>{{ ratio(parameters.stop_loss) }} / {{ ratio(parameters.take_profit) }}</b></div><div><span>最长持有</span><b>{{ parameters.max_holding_days }}个交易日</b></div></div>
      <div class="vf-progress"><div><span>已封存观察日</span><strong>{{ report.sealed_days }} / {{ policy.minimum_trading_days }}</strong><progress :value="report.sealed_days" :max="policy.minimum_trading_days" aria-label="前瞻观察日进度"></progress><small>最早开始 {{ date(plan.candidate.start) }}</small></div><div><span>候选已平仓</span><strong>{{ candidate.trades || 0 }} / {{ policy.minimum_trades }}</strong><progress :value="candidate.trades || 0" :max="policy.minimum_trades" aria-label="候选平仓进度"></progress><small>{{ report.last_date?'封存截至 '+report.last_date:'尚无封存日档' }}</small></div><div class="vf-runtime"><span>采集状态</span><strong :class="{'vf-error':report.status.status==='error'}">{{ report.status.detail || '尚无调度记录' }}</strong><small>检查 {{ time(report.status.checked_at) }}</small><small v-if="report.status_stale" class="vf-warning">调度状态待更新</small></div></div>
      <div class="dashboard-segment vf-tabs" role="tablist" aria-label="前瞻研究视图"><button type="button" role="tab" :aria-selected="tab==='progress'" :aria-pressed="tab==='progress'" @click="tab='progress'">前瞻进度</button><button type="button" role="tab" :aria-selected="tab==='evidence'" :aria-pressed="tab==='evidence'" @click="tab='evidence'">离线证据</button><button type="button" role="tab" :aria-selected="tab==='config'" :aria-pressed="tab==='config'" @click="tab='config'">冻结配置</button></div>
      <div v-if="tab==='progress'"><p class="vf-empty" v-if="!report.sealed_days">等待冻结起点之后的新交易日，暂无前瞻收益数据</p><div class="vf-table-wrap"><table class="vf-table"><thead><tr><th>独立资金路径</th><th>收益</th><th>最大回撤</th><th>现金</th><th>平仓 / 持仓</th><th>待执行信号</th><th>费用</th></tr></thead><tbody><tr v-for="row in report.groups" :key="row.id" :data-forward-group="row.id"><td>{{ row.name }}</td><td>{{ percent(row.return_percent) }}</td><td>{{ percent(row.drawdown_percent) }}</td><td>{{ money(row.cash) }}</td><td>{{ row.trades }} / {{ row.positions }}</td><td>{{ row.pending }}</td><td>{{ money(row.fees) }}</td></tr></tbody></table></div><div v-if="report.assessment" class="vf-assessment"><h3>{{ report.assessment.verdict }}</h3><div v-for="check in report.assessment.checks" :key="check.key"><span :class="check.passed?'up':'vf-warning'">{{ check.passed?'达标':'待满足' }}</span><strong>{{ check.name }}</strong><span>{{ check.detail }}</span></div></div></div>
      <div v-else-if="tab==='evidence'"><h3>原研究的滚动验证窗口</h3><div class="vf-table-wrap"><table class="vf-table"><thead><tr><th>规则</th><th>成本</th><th>平均窗口收益</th><th>最差回撤</th><th>正收益窗口</th><th>交易</th><th>离线检查</th></tr></thead><tbody><template v-for="row in report.evidence" :key="row.entry_volume_floor"><tr v-for="cost in ['normal','double_cost']" :key="row.entry_volume_floor+cost"><td>{{ row.name }}</td><td>{{ cost==='normal'?'常规':'双倍' }}</td><td>{{ percent(row[cost].average_return_percent) }}</td><td>{{ percent(row[cost].worst_drawdown_percent) }}</td><td>{{ ratio(row[cost].positive_fold_ratio) }}</td><td>{{ row[cost].trades }}</td><td :class="row[cost].passed?'up':'vf-warning'">{{ row[cost].passed?'通过':'未通过' }}</td></tr></template></tbody></table></div><details class="vf-boundaries"><summary>研究边界</summary><ul><li v-for="warning in report.warnings" :key="warning">{{ warning }}</li></ul></details></div>
      <div v-else><dl class="vf-config"><div><dt>入场模式</dt><dd>趋势重返 · {{ parameters.entry_mode }}</dd></div><div><dt>各组初始资金</dt><dd>{{ money(plan.candidate.initial_cash) }} 元</dd></div><div><dt>成交口径</dt><dd>日线模拟 · T+1 · 不复权 · 不强制期末清仓</dd></div><div><dt>参考基准</dt><dd>{{ plan.candidate.benchmark }}</dd></div><div><dt>复核门槛</dt><dd>{{ policy.minimum_trading_days }}日 / {{ policy.minimum_trades }}笔，收益与超额为正，回撤≤{{ policy.maximum_drawdown_percent }}%</dd></div><div><dt>配置指纹</dt><dd>{{ plan.fingerprint }}</dd></div><div><dt>程序指纹</dt><dd>{{ plan.executable_sha256 }}</dd></div><div><dt>源研究指纹</dt><dd>{{ plan.source_sha256 }}</dd></div></dl><div class="vf-stock-list"><span v-for="stock in stockNames" :key="stock.symbol">{{ stock.name }} <small>{{ stock.symbol }}</small></span></div></div>
    </template>
  </section>`,
}
