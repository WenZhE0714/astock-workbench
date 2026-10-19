import assert from 'node:assert/strict'
import { normalizeBoardLayout, boardBubbles, boardDailyStats, linkedBoardRows, selectedSnapshot, shanghaiDate, boardDailySession } from '../src/board-workbench.mjs'

assert.deepEqual(normalizeBoardLayout({modules:['market','bad','market'],wide:['bad'],mode:'oops'}),{modules:['market'],wide:[],mode:'bubble'})
const bubbles=boardBubbles([{code:'a',percent:2,main_net_yuan:1e8},{code:'b',percent:-3,main_net_yuan:-2e8},{code:'c',percent:null,main_net_yuan:0}])
assert.equal(bubbles.points.length,2)
assert(bubbles.points[0].x>470&&bubbles.points[0].y<212)
assert(bubbles.points[1].x<470&&bubbles.points[1].y>212)
assert.deepEqual(boardDailyStats([{date:'2026-10-13',main_net_yuan:-1},{date:'2026-10-14',main_net_yuan:3},{date:'2026-10-15',main_net_yuan:2}]),{streak:2,sum3:4,sum5:null,count:3})
assert.equal(linkedBoardRows([{industry:'银行',symbol:'a'},{industry:'证券',symbol:'b'}],{name:'银行'}).length,1)
assert.equal(linkedBoardRows([{industry:'银行',symbol:'a'}],{name:'银行Ⅱ'}).length,1)
const snapshots=[{id:'1'},{id:'2'}]
assert.equal(selectedSnapshot(snapshots,0,false).id,'1')
assert.equal(selectedSnapshot(snapshots,0,true).id,'2')
assert.equal(shanghaiDate('2026-10-15T18:00:00Z'),'2026-10-16')
assert.equal(boardDailySession('2026-10-16T06:59:59Z'),'2026-10-16:intraday')
assert.equal(boardDailySession('2026-10-16T07:00:00Z'),'2026-10-16:closed')
assert.equal(boardDailySession('2026-10-16T16:00:00Z'),'2026-10-17:intraday')
console.log('PASS: board layout validation, quadrants, daily summaries, linking and replay selection')
