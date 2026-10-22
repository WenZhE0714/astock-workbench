import { RefreshCw, Download } from 'lucide-vue-next'

export const MarketChatReview = {
  components: { RefreshCw, Download },
  props: { assessment: { type: Object, required: true } },
  data() { return { review: null, loading: false, error: '' } },
  computed: {
    evidenceURL() { return `/api/assistant/market-review?id=${encodeURIComponent(this.assessment.id)}&evidence=1` },
  },
  methods: {
    direction(value) { return { bullish: '偏多', bearish: '偏空', range: '震荡', unknown: '证据不足' }[value] || '未记录' },
    status(days) {
      if (!this.assessment.evaluable) return '不计分'
      const item = this.review?.outcomes?.find(row => row.trading_days === days)
      if (!item) return '待复核'
      if (item.status === 'waiting') return `待到期 ${item.observed_days}/${days}日`
      if (item.status === 'ineligible') return '不计分'
      const change = Number(item.return_percent).toFixed(2)
      const matched = item.matched === true ? '方向一致' : item.matched === false ? '方向不符' : '未给方向'
      return `${item.end_date} · ${change}% · ${matched}`
    },
    async check() {
      if (this.loading) return
      this.loading = true; this.error = ''
      try {
        const response = await fetch(`/api/assistant/market-review?id=${encodeURIComponent(this.assessment.id)}`, { cache: 'no-store' })
        const data = await response.json()
        if (!response.ok) throw new Error(data.error || '复核失败')
        this.review = data
      } catch (error) { this.error = error.message || String(error) }
      finally { this.loading = false }
    },
  },
  template: `<section class="market-chat-review" aria-label="大盘判断复核">
    <header><strong>判断复核</strong><div><a class="icon-button" :href="evidenceURL" title="下载原始证据" aria-label="下载原始证据"><download :size="15"/></a><button class="icon-button" :disabled="loading" @click="check" title="复核后续走势" aria-label="复核后续走势"><refresh-cw :size="15"/></button></div></header>
    <p>上证指数 · 基准 {{ assessment.base_date }} 收盘 {{ Number(assessment.base_close).toFixed(2) }}</p>
    <dl><template v-for="item in assessment.predictions" :key="item.trading_days"><dt>{{ item.trading_days }}交易日 · {{ direction(item.direction) }}</dt><dd>{{ status(item.trading_days) }}</dd></template></dl>
    <small>方向核对中性区间 ±{{ assessment.neutral_band_percent }}%，未含交易成本</small>
    <p v-if="assessment.limit_reason">{{ assessment.limit_reason }}</p><p v-if="error" role="alert">{{ error }}</p>
  </section>`,
}
