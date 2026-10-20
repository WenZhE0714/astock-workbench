import assert from 'node:assert/strict'
import {phaseLabel,deliveryLabel,ruleFromLegacy,pendingLegacyAlerts} from '../src/price-alerts.mjs'
const old={id:'price-12345678',symbol:'sh600110',name:'test',direction:'above',price:10,expires_on:'2026-10-21',enabled:true,source_kind:'historical',source_date:'2026-07-16',source_fingerprint:'a'.repeat(64)}
assert.equal(pendingLegacyAlerts([old,{...old,fired_at:'2026-10-20'},null,{...old,enabled:false}]).length,1)
assert.deepEqual(ruleFromLegacy(old).source,{id:'historical',date:'2026-07-16',fingerprint:'a'.repeat(64)})
assert.equal(ruleFromLegacy({...old,source_fingerprint:null}).source,undefined)
assert.equal(phaseLabel('paused'),'已暂停')
assert.equal(deliveryLabel({event:{}}),'触发已记录')
assert.equal(deliveryLabel({notification:{status:'submitted'}}),'已提交系统')
assert.equal(deliveryLabel({notification:{status:'failed'}}),'发送失败')
console.log('Price alerts: explicit legacy mapping and delivery status labels passed')
