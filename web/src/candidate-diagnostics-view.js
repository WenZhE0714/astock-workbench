import { finiteNumber } from './dashboard.mjs'

export const CandidateDiagnostics = {
  props: {candidate:{type:Object,required:true},formatParameters:{type:Function,required:true}},
  data(){return {selectedID:''}},
  computed:{
    rows(){return this.candidate.diagnostics || []},
    selected(){return this.rows.find(row=>row.id===this.selectedID) || this.rows.find(row=>row.selected) || this.rows[0]},
  },
  watch:{'candidate.experiment_id'(){this.selectedID=''}},
  methods:{
    percent(value){const number=finiteNumber(value);return number===null?'--':`${number.toFixed(2)}%`},
    ratio(value){const number=finiteNumber(value);return number===null?'--':`${(number*100).toFixed(0)}%`},
  },
  template:`<section class="candidate-diagnostics" aria-label="研究候选参数与未入选原因" v-if="rows.length">
    <div class="cd-heading"><h3>候选参数与验证结果</h3><span>通过 {{ candidate.passed_count }} / {{ candidate.candidate_count }} 组</span></div>
    <div class="cd-table-wrap"><table class="cd-table"><thead><tr><th>候选</th><th>完成折数</th><th>正收益窗口</th><th>验证交易</th><th>平均验证收益</th><th>最差回撤</th><th>结论</th></tr></thead><tbody><tr v-for="row in rows" :key="row.id" :class="{selected:row.id===selected?.id}"><td><button type="button" :aria-label="'查看候选 '+row.id+' 参数'" @click="selectedID=row.id">{{ row.id }}</button></td><td>{{ row.completed_folds }} / {{ row.expected_folds }}</td><td>{{ ratio(row.positive_fold_ratio) }}</td><td>{{ row.validation_trades ?? '--' }}</td><td>{{ percent(row.average_validation_return_percent) }}</td><td>{{ percent(row.worst_validation_drawdown_percent) }}</td><td :class="row.rejected?'down':'up'">{{ row.selected?'已锁定':row.rejected?'未入选':'滚动验证通过' }}</td></tr></tbody></table></div>
    <small v-if="rows.length < candidate.candidate_count" class="cd-count">展示原归档排名前 {{ rows.length }} 组</small>
    <template v-if="selected"><div class="cd-detail-head"><h4>{{ selected.id }} · {{ selected.selected?'已锁定参数':'研究参数，未锁定' }}</h4><span>{{ selected.agent }}</span></div><p class="cd-parameters">{{ formatParameters(selected) }}</p><ul class="cd-reasons" v-if="selected.reasons.length"><li v-for="reason in selected.reasons" :key="reason">{{ reason }}</li></ul><p class="cd-pending" v-else-if="!selected.selected">该候选未被选为本轮最终参数</p>
    <div class="cd-table-wrap" v-if="selected.folds.length"><table class="cd-table"><thead><tr><th>验证折</th><th>验证区间</th><th>收益</th><th>最大回撤</th><th>交易数</th></tr></thead><tbody><tr v-for="fold in selected.folds" :key="fold.id"><td>{{ fold.id }}</td><td>{{ fold.start }} 至 {{ fold.end }}</td><td>{{ percent(fold.return_percent) }}</td><td>{{ percent(fold.drawdown_percent) }}</td><td>{{ fold.trades }}</td></tr></tbody></table></div></template>
  </section>`,
}
