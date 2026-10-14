import { Check, ShieldCheck, ArrowUpRight } from 'lucide-vue-next'

export const PatternForwardHealthView = {
  components:{Check,ShieldCheck,ArrowUpRight},props:['record','runtime'],emits:['open-day','refresh'],
  data(){return {unreadOnly:false,error:'',busy:0,auditing:false,audit:null,controller:null}},
  computed:{
    health(){return this.record.health || {days:[],issues:[]}},
    events(){return [...(this.record.operations?.events || [])].reverse().filter(e=>!this.unreadOnly || !e.acknowledged_at)},
    verification(){const saved=this.record.operations?.audit;return this.audit && (!saved?.checked_at || new Date(this.audit.checked_at)>=new Date(saved.checked_at)) ? this.audit : saved},
    unread(){return this.record.operations?.events?.filter(e=>!e.acknowledged_at).length || 0},
  },
  methods:{
    time(value){return !value || value.startsWith('0001') ? '--' : new Date(value).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false})},
    phase(value){return {waiting:'等待采集',paused:'已暂停',blocked:'规则不兼容',complete:'当日完成',incomplete:'当日缺采',tracking:'等待结果日志','calendar-pending':'交易日待核验',closed:'非交易日',due:'等待调度',retrying:'等待重试',collecting:'采集中'}[value] || '--'},
    dayState(value){return {captured:'已留存',open:'采集中',partial:'部分缺采',missed:'缺采'}[value] || value},
    auditState(value){return {verified:'完整性校验通过',waiting:'等待首日采集',collecting:'当日尚未封存',partial:'含缺采记录','tracking-pending':'等待结果日志',invalid:'完整性校验失败'}[value] || '尚未验收'},
    async acknowledge(event){
      this.busy=event.sequence;this.error=''
      try{const response=await fetch('/api/pattern-forwards',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'ack-alert',id:this.record.task.id,sequence:event.sequence})});const data=await response.json();if(!response.ok)throw new Error(data.error || '确认记录失败');this.$emit('refresh')}
      catch(e){this.error=e.message}finally{this.busy=0}
    },
    async verify(){
      this.controller?.abort();const controller=new AbortController();this.controller=controller;this.auditing=true;this.error=''
      try{const response=await fetch('/api/pattern-forwards?'+new URLSearchParams({id:this.record.task.id,audit:'1'}),{cache:'no-store',signal:controller.signal});const data=await response.json();if(!response.ok)throw new Error(data.error || '归档验收失败');this.audit=data}
      catch(e){if(e.name!=='AbortError')this.error=e.message}finally{this.auditing=false}
    },
  },
  beforeUnmount(){this.controller?.abort()},
  template:`<section class="pv-forward-health" aria-label="前向运行监控">
    <div class="pv-section-title"><h3>采集健康</h3><strong :data-health-phase="health.phase">{{ phase(health.phase) }}</strong></div>
    <p class="pv-study-basis">{{ health.date }} · {{ health.detail }}</p><p v-if="error" role="alert" class="pv-action-warning">{{ error }}</p><p v-if="runtime?.error" role="alert" class="pv-action-warning">{{ runtime.error }}</p><p v-if="runtime && !runtime.running" class="pv-action-warning">后台调度未运行</p>
    <div class="pv-totals"><div><span>当日留存 / 股票池</span><strong>{{ health.captured }} / {{ health.expected }}</strong></div><div><span>历史缺采日</span><strong>{{ health.missing_days }}</strong></div><div><span>结果待衔接日</span><strong>{{ health.tracking_lag }}</strong></div><div><span>未确认记录</span><strong>{{ unread }}</strong></div></div>
    <dl class="pv-health-timing"><div><dt>下次计划采集</dt><dd>{{ time(health.next_attempt_at) }}</dd></div><div><dt>最近完整采集</dt><dd>{{ time(health.last_success_at) }}</dd></div><div><dt>最近调度结束</dt><dd>{{ time(runtime?.last_cycle_at) }}</dd></div><div><dt>结果日志截至</dt><dd>{{ record.status.tracking_through || '--' }}</dd></div></dl>
    <ul v-if="health.issues.length" class="pv-health-issues"><li v-for="issue in health.issues" :key="issue.key" :data-issue-severity="issue.severity"><strong>{{ issue.message }}</strong><small v-if="issue.symbols?.length">{{ issue.symbols.join('、') }}</small></li></ul>
    <div class="pv-section-title"><h3>归档验收</h3><button type="button" class="icon-button" title="校验归档完整性" aria-label="校验前向归档" :disabled="auditing" @click="verify"><shield-check :size="16"/></button></div><p :data-audit-state="verification?.state || 'pending'">{{ auditing ? '正在校验' : auditState(verification?.state) }}</p><p class="pv-study-basis">核验 {{ time(verification?.checked_at) }} · {{ verification?.snapshots || 0 }}份股票快照 · 截至 {{ verification?.through || '--' }}</p><p v-if="verification?.error" role="alert" class="pv-action-warning">{{ verification.error }}</p>
    <div class="pv-section-title"><h3>每日完成情况</h3></div><div v-for="day in health.days" :key="day.date" class="pv-health-day"><time>{{ day.date }}</time><span>{{ dayState(day.state) }}</span><div><progress :value="day.captured" :max="day.expected || 1" :aria-label="day.date+'采集进度'"></progress><small>{{ day.captured }} / {{ day.expected }}只 · {{ day.tracking_done ? '日志已衔接' : '日志待衔接' }}</small></div><button type="button" class="icon-button" :title="'查看'+day.date+'快照'" :aria-label="'查看'+day.date+'快照'" @click="$emit('open-day',day.date)"><arrow-up-right :size="15"/></button><p v-if="day.missing.length" class="pv-health-missing">缺采或待完成 {{ day.missing.join('、') }}</p></div><p v-if="!health.days.length" class="pv-empty">尚无采集日</p>
    <div class="pv-section-title"><h3>异常记录</h3><label class="pv-health-unread"><input type="checkbox" v-model="unreadOnly">仅未确认</label></div><div v-for="event in events" :key="event.sequence" class="pv-health-event" :data-alert-kind="event.kind"><div><strong>{{ event.kind==='resolved' ? '已恢复' : '发现异常' }} · {{ event.issue.message }}</strong><small>{{ time(event.at) }}<template v-if="event.acknowledged_at"> · 已确认 {{ time(event.acknowledged_at) }}</template></small></div><button v-if="!event.acknowledged_at" type="button" class="icon-button" title="确认异常记录" :aria-label="'确认记录'+event.sequence" :disabled="!!busy" @click="acknowledge(event)"><check :size="15"/></button></div><p v-if="!events.length" class="pv-empty">暂无异常记录</p>
  </section>`,
}
