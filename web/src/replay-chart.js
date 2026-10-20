import { markRaw } from 'vue'
import { createChart, CandlestickSeries, HistogramSeries, LineSeries, createSeriesMarkers, CrosshairMode } from 'lightweight-charts'
import { movingAverage } from './frozen-replay.mjs'

export const ReplayChart = {
  props: { bars: { default: () => [] }, lines: { default: () => [] }, markers: { default: () => [] }, ma: Boolean, volume: Boolean, focus: Object, cursor: String, range: Object, level: Number },
  emits: ['cursor', 'range', 'pick'],
  data() { return { engine: null } },
  watch: {
    bars: { handler() { this.render() } }, lines: { handler() { this.render() }, deep: true }, markers: { handler() { this.renderMarkers() }, deep: true },
    ma() { this.render() }, focus() { this.focusTrade() }, cursor() { this.syncCursor() }, range() { this.syncRange() }, level() { this.renderLevel() },
  },
  methods: {
    render() {
      if (!this.engine) return
      const e = this.engine
      const oldRange = e.chart.timeScale().getVisibleLogicalRange()
      e.primary.setData(this.lines.length ? this.lines[0].data : this.bars)
      e.secondary.setData(this.lines[1]?.data || [])
      e.volume.setData(this.volume ? this.bars.map(b => ({ time: b.time, value: b.volume, color: b.close >= b.open ? '#ef6b6b66' : '#48c5a066' })) : [])
      e.averages.forEach((s, i) => s.setData(this.ma ? movingAverage(this.bars, [20, 60][i]) : []))
      this.renderMarkers(); this.renderLevel()
      const last = this.bars.at(-1)?.time || this.lines[0]?.data.at(-1)?.time
      if (!e.initialized || e.lastTime !== last) { e.chart.timeScale().fitContent(); e.initialized = true }
      else if (oldRange) e.chart.timeScale().setVisibleLogicalRange(oldRange)
      e.lastTime = last
      this.focusTrade()
    },
    renderMarkers() { this.engine?.markers.setMarkers(this.markers.map(({ tradeKey, ...m }) => m)) },
    renderLevel() {
      if (!this.engine) return
      if (this.engine.level) this.engine.primary.removePriceLine(this.engine.level)
      this.engine.level = Number.isFinite(this.level) && this.level > 0 ? this.engine.primary.createPriceLine({ price: this.level, color: '#efb755', lineWidth: 1, lineStyle: 2, axisLabelVisible: true, title: '提醒草案' }) : null
    },
    focusTrade() {
      if (!this.engine || !this.focus) return
      const first = this.bars.findIndex(b => b.time >= this.focus.from)
      const last = this.bars.findIndex(b => b.time >= this.focus.to)
      if (first >= 0) this.engine.chart.timeScale().setVisibleLogicalRange({ from: Math.max(0, first - 12), to: Math.min(this.bars.length + 2, (last < 0 ? this.bars.length - 1 : last) + 12) })
    },
    syncCursor() {
      const e = this.engine
      if (!e || e.syncing) return
      e.syncing = true
      const data = this.lines.length ? this.lines[0].data : this.bars
      const bar = this.cursor ? data.filter(b => b.time <= this.cursor).at(-1) : null
      if (bar) e.chart.setCrosshairPosition(bar.close ?? bar.value, bar.time, e.primary)
      else e.chart.clearCrosshairPosition()
      e.syncing = false
    },
    syncRange() {
      const e = this.engine
      if (!e || !this.range || e.syncing || !e.initialized) return
      e.syncing = true
      try { e.chart.timeScale().setVisibleRange(this.range) } catch { /* No shared completed weekly range yet. */ }
      e.syncing = false
    },
    reset() { this.engine?.chart.timeScale().fitContent() },
  },
  mounted() {
    const chart = createChart(this.$refs.host, { autoSize: true, layout: { background: { color: '#14191d' }, textColor: '#a6b2b7', fontFamily: '-apple-system, BlinkMacSystemFont, sans-serif', fontSize: 11, attributionLogo: true }, grid: { vertLines: { color: '#242c31' }, horzLines: { color: '#242c31' } }, rightPriceScale: { borderColor: '#354048', scaleMargins: { top: .12, bottom: this.volume ? .26 : .12 } }, timeScale: { borderColor: '#354048', rightOffset: 3, timeVisible: false }, crosshair: { mode: CrosshairMode.Normal }, localization: { locale: 'zh-CN' }, handleScroll: { vertTouchDrag: false } })
    const primary = chart.addSeries(this.lines.length ? LineSeries : CandlestickSeries, this.lines.length ? { color: '#58b9d7', lineWidth: 2, priceFormat: { type: 'percent' } } : { upColor: '#ef6b6b', downColor: '#48c5a0', wickUpColor: '#ef6b6b', wickDownColor: '#48c5a0', borderVisible: false })
    const secondary = chart.addSeries(LineSeries, { color: '#efb755', lineWidth: 2, priceFormat: { type: 'percent' } })
    const volume = chart.addSeries(HistogramSeries, { priceScaleId: 'volume', priceFormat: { type: 'volume' }, lastValueVisible: false, priceLineVisible: false })
    volume.priceScale().applyOptions({ scaleMargins: { top: .82, bottom: 0 }, visible: false })
    const averages = ['#58b9d7', '#b39bd7'].map(color => chart.addSeries(LineSeries, { color, lineWidth: 1, lastValueVisible: false, priceLineVisible: false, crosshairMarkerVisible: false }))
    this.engine = markRaw({ chart, primary, secondary, volume, averages, markers: createSeriesMarkers(primary, []), syncing: false, initialized: false })
    chart.subscribeCrosshairMove(p => { if (!this.engine.syncing) this.$emit('cursor', typeof p.time === 'string' ? p.time : '') })
    chart.timeScale().subscribeVisibleTimeRangeChange(range => { if (!this.engine.syncing && range) this.$emit('range', range) })
    chart.subscribeClick(p => { if (p.point && p.time) this.$emit('pick', { date: p.time, price: primary.coordinateToPrice(p.point.y), tradeKey: this.markers.find(m => m.time === p.time)?.tradeKey }) })
    this.render()
  },
  beforeUnmount() { this.engine?.chart.remove(); this.engine = null },
  template: `<div class="replay-chart-canvas" ref="host" role="img" aria-label="冻结行情图表"></div>`,
}
