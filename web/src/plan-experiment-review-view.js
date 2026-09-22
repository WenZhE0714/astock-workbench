import { ArrowUpRight, Download } from "lucide-vue-next"
import { finiteNumber } from "./dashboard.mjs"
import { experimentReviewPlans, experimentReviewStatusLabel, experimentTrialLabel } from "./plan-experiment.mjs"
import "./plan-experiment-review.css"

export const PlanExperimentReviewView = {
  components: { ArrowUpRight, Download },
  props: { review: Object },
  emits: ["open-plan"],
  data() { return { filter: "all", query: "", shown: 40 } },
  computed: {
    plans() { return experimentReviewPlans(this.review, this.filter, this.query) },
    filters() {
      return [
        { key: "all", label: "全部" }, { key: "paired_closed", label: "成对平仓" },
        { key: "single_sided", label: "单边成交" }, { key: "open", label: "持仓中" },
        { key: "waiting", label: "未成交" }, { key: "unavailable", label: "待核对" },
      ]
    },
  },
  methods: {
    experimentReviewStatusLabel,
    experimentTrialLabel,
    number(value, digits = 2) { const amount = finiteNumber(value); return amount == null ? "--" : amount.toFixed(digits) },
    money(value) { const amount = finiteNumber(value); return amount == null ? "--" : amount.toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 }) },
    r(value) { const amount = finiteNumber(value); return amount == null ? "--" : `${amount > 0 ? "+" : ""}${amount.toFixed(2)}R` },
    difference(value) { const amount = finiteNumber(value); return amount == null ? "--" : `${amount > 0 ? "+" : ""}${amount.toFixed(2)} 个百分点` },
    tone(value) { return value > 0 ? "up" : value < 0 ? "down" : "flat" },
    time(value) {
      if (!value || String(value).startsWith("0001")) return "--"
      const date = new Date(String(value).includes("T") ? value : String(value).replace(" ", "T") + "+08:00")
      return Number.isNaN(date.getTime()) ? "--" : date.toLocaleString("zh-CN", { timeZone: "Asia/Shanghai", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })
    },
    selectFilter(filter) { this.filter = filter; this.shown = 40 },
    fills(plan) { return [plan.range, plan.confirmed].filter(Boolean) },
    armName(id) { return id === "range" ? "区间条件组" : "仅确认对照组" },
  },
  template: `<section class="experiment-review" aria-label="影子实验自动复盘">
    <header class="experiment-review-head"><div><h2>自动复盘</h2><p>同计划成对平仓 · 各自完整持有期 · 扣费后</p></div><a class="icon-button" href="/api/plan-experiment?view=review&download=1" download="plan-experiment-review.json" title="导出自动复盘" aria-label="导出自动复盘"><download :size="16" /></a></header>
    <template v-if="review">
      <div class="experiment-review-kpis">
        <div><span>成对平仓 / 全部计划</span><strong>{{ review.paired_completed }} / {{ review.total_plans }}</strong><small>两组均已完成退出</small></div>
        <div><span>平均净收益率差</span><strong :class="tone(review.average_return_difference)">{{ difference(review.average_return_difference) }}</strong><small>区间组 - 对照组</small></div>
        <div><span>平均净 R 差</span><strong :class="tone(review.average_net_r_difference)">{{ r(review.average_net_r_difference) }}</strong><small>{{ review.net_r_samples }} 组成对风险样本</small></div>
        <div><span>成对已实现盈亏差</span><strong :class="tone(review.profit_difference)">{{ money(review.profit_difference) }}</strong><small>元 · 含仓位数量差异</small></div>
      </div>
      <div class="experiment-review-coverage"><span>区间较高 {{ review.range_higher }}</span><span>对照较高 {{ review.confirmed_higher }}</span><span>持平 {{ review.equal_return }}</span><span>单边 {{ review.single_sided }}</span><span>未平仓 {{ review.open_pairs }}</span><span>未成交 {{ review.waiting }}</span><span v-if="review.unavailable">待核对 {{ review.unavailable }}</span><time>账本 {{ time(review.as_of) }}</time></div>
      <div class="experiment-review-arms"><section v-for="arm in (review.arms || [])" :key="arm.id" :data-arm="arm.id"><h3>{{ arm.name }}</h3><dl><div><dt>已平仓净盈亏</dt><dd :class="tone(arm.net_profit)">{{ money(arm.net_profit) }}<small v-if="arm.net_profit != null"> 元</small></dd></div><div><dt>完成 / 在持</dt><dd>{{ arm.completed }} / {{ arm.open }}</dd></div><div><dt>平均净 R</dt><dd>{{ r(arm.average_net_r) }}<small> · {{ arm.net_r_samples }} 笔</small></dd></div><div><dt>成交费用（含在持）</dt><dd>{{ money(arm.fees) }}</dd></div><div><dt>盈利占比</dt><dd>{{ arm.win_rate == null ? '--' : number(arm.win_rate, 1) + '%' }}</dd></div><div><dt>拒单次数</dt><dd>{{ arm.rejections }}</dd></div></dl><p class="chart-plan-error" v-if="arm.unavailable">{{ arm.unavailable }} 条记录待核对</p></section></div>
      <div class="experiment-review-toolbar"><label><span>计划状态</span><select :value="filter" @change="selectFilter($event.target.value)" aria-label="自动复盘状态"><option v-for="item in filters" :key="item.key" :value="item.key">{{ item.label }}</option></select></label><input v-model.trim="query" @input="shown = 40" placeholder="股票、形态或计划编号" aria-label="筛选自动复盘"><span>{{ plans.length }} 条计划</span></div>
      <div class="experiment-review-plans">
        <details class="experiment-review-plan" v-for="plan in plans.slice(0, shown)" :key="plan.plan_id" :data-plan-id="plan.plan_id">
          <summary><span class="experiment-review-stock"><strong>{{ plan.name || plan.symbol.slice(2) }}</strong><small>{{ plan.symbol.slice(2) }} · {{ plan.structure_name }}</small><small>{{ experimentReviewStatusLabel(plan.status) }}{{ plan.selected ? '' : ' · 已移出实验' }}</small></span><span><small>区间净 R</small><strong :class="tone(plan.range.net_r)">{{ r(plan.range.net_r) }}</strong><small>{{ experimentTrialLabel(plan.range.status) }}</small></span><span><small>对照净 R</small><strong :class="tone(plan.confirmed.net_r)">{{ r(plan.confirmed.net_r) }}</strong><small>{{ experimentTrialLabel(plan.confirmed.status) }}</small></span><span class="experiment-review-delta"><small>区间 - 对照</small><strong :class="tone(plan.net_r_difference)">{{ r(plan.net_r_difference) }}</strong><small>{{ difference(plan.return_difference) }}</small></span></summary>
          <div class="experiment-review-plan-meta"><span>分析 {{ plan.analysis_date }} · 有效至 {{ plan.expires_on }}</span><button type="button" @click="$emit('open-plan', plan)" class="plan-monitor-stock" :aria-label="'查看原计划 ' + plan.plan_id.slice(0, 8)">原计划<arrow-up-right :size="15" /></button></div>
          <div class="experiment-review-fills"><section v-for="fill in fills(plan)" :key="fill.arm_id"><h4>{{ armName(fill.arm_id) }}<small>{{ experimentTrialLabel(fill.status) }}</small></h4><dl><div><dt>条件观察时间</dt><dd>{{ time(fill.trigger_at) }}</dd></div><div><dt>模拟入场时间</dt><dd>{{ time(fill.entry_at) }}</dd></div><div><dt>入场价 / 数量</dt><dd>{{ number(fill.entry_price) }} / {{ fill.quantity || '--' }}</dd></div><div><dt>模拟退出时间</dt><dd>{{ time(fill.exit_at) }}</dd></div><div><dt>退出价</dt><dd>{{ number(fill.exit_price) }}</dd></div><div><dt>成交费用</dt><dd>{{ money(fill.fees) }}</dd></div><div><dt>已实现净盈亏</dt><dd :class="tone(fill.net_profit)">{{ money(fill.net_profit) }}</dd></div><div><dt>初始风险金额</dt><dd :title="'（实际入场价 - 冻结失效位）× 数量'">{{ money(fill.initial_risk) }}</dd></div><div><dt>扣费后 R</dt><dd :class="tone(fill.net_r)">{{ r(fill.net_r) }}</dd></div><div><dt>拒单次数</dt><dd>{{ fill.rejections }}</dd></div></dl><p class="experiment-review-exit">{{ fill.exit_reason || fill.reason }}</p><p v-if="fill.last_rejection" class="experiment-review-rejection">最近拒单：{{ fill.last_rejection }}</p><p v-for="warning in (fill.warnings || [])" :key="warning" class="chart-plan-error">{{ warning }}</p><small v-if="fill.entry_order_id" class="experiment-review-order">入场订单 {{ fill.entry_order_id }}</small><small v-if="fill.exit_order_id" class="experiment-review-order">退出订单 {{ fill.exit_order_id }}</small></section></div>
        </details>
      </div>
      <p class="chart-analysis-empty" v-if="!plans.length">{{ review.total_plans ? '当前筛选没有计划' : '尚无实验计划' }}</p>
      <button type="button" v-if="shown < plans.length" class="experiment-review-more" @click="shown += 40">加载更多</button>
      <p v-for="warning in (review.warnings || [])" :key="warning" role="status" class="chart-plan-error">{{ warning }}</p>
    </template>
    <p v-else class="chart-analysis-empty">等待实验复盘</p>
  </section>`,
}
