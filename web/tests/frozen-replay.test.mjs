import assert from 'node:assert/strict'
import { movingAverage, replayTrades, replayMarkers, relativeSeries, filterReplaySymbols, normalizeReplayLayout, quoteTriggersAlert } from '../src/frozen-replay.mjs'

const trade = { symbol: 'sh600110', entry_signal: { date: '2026-07-22' }, entry: { date: '2026-07-23', quantity: 100 }, exit: { date: '2026-07-24' }, net_profit: 50 }
assert.equal(replayTrades({ trades: [trade] }, '2026-07-23').length, 0)
assert.equal(replayTrades({ trades: [trade] }, '2026-07-24').length, 1)
assert.ok(replayMarkers(replayTrades({ trades: [trade] }, '2026-07-24'), trade.symbol, '2026-07-23').every(m => m.time <= '2026-07-23'))
const bars = [1,2,3,4].map((close,i) => ({ time: `2026-07-${20+i}`, close }))
assert.deepEqual(movingAverage(bars,3).map(p=>p.value),[2,3])
assert.deepEqual(movingAverage(bars.slice(0,2),3),[])
const r = relativeSeries(bars,[{time:'2026-07-21',close:100},{time:'2026-07-23',close:110}], '2026-07-20')
assert.deepEqual(r.stock.map(p=>p.time),['2026-07-21','2026-07-23'])
assert.equal(r.stock[0].value,0)
assert.ok(Math.abs(r.benchmark[1].value-10)<1e-8)
assert.equal(normalizeReplayLayout({weekly:false}).weekly,false)
const symbols=[{symbol:'sh600110',name:'诺德'},{symbol:'sz002413',name:'雷科'}]
const run={pending_orders:[{symbol:'sh600110',signal:{volume_ratio:1.5}}],open_positions:[{symbol:'sz002413'}]}
assert.equal(filterReplaySymbols(symbols,run,'','volume',new Set(),false)[0].symbol,'sh600110')
assert.equal(filterReplaySymbols(symbols,run,'','held',new Set(),false)[0].symbol,'sz002413')
assert.deepEqual(filterReplaySymbols(symbols,run,'','all',new Set(),true),[])
const now=Date.parse('2026-10-20T14:00:00+08:00')
const alert={enabled:true,price:10,direction:'above',expires_on:'2026-10-21'}
assert.ok(quoteTriggersAlert(alert,{quote_time:'2026-10-20 13:59:59',current:'10'},now))
assert.ok(!quoteTriggersAlert(alert,{quote_time:'2026-10-19 14:00:00',current:'12'},now))
assert.ok(!quoteTriggersAlert({...alert,fired_at:'2026-10-20'},{quote_time:'2026-10-20 14:00:00',current:'12'},now))
console.log('Frozen replay: cutoff, markers, aligned comparison, filters and alert freshness passed')
