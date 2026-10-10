import assert from 'node:assert/strict'
import { emptyAlertState, collectLocalAlerts, deliveryCandidates, parseAlertState } from '../src/local-alerts.mjs'

const now = Date.parse('2026-10-09T10:00:00+08:00')
const event = (sequence, time = now) => ({ id: `e${sequence}`, sequence, kind: 'zone_entered', notify: true, message: 'entered', observed_at: new Date(time).toISOString() })
const monitor = events => [{ plan_id: 'p1', symbol: 'sh600519', name: 'sample', enabled: true, sequence: events.at(-1)?.sequence || 0, events }]
let baseline = collectLocalAlerts({ ...emptyAlertState(), enabled: true, enabledAt: now - 60000 }, monitor([event(1)]), now)
assert.equal(baseline.incoming.length, 0)
assert.equal(baseline.state.records[0].read, true)
let update = collectLocalAlerts(parseAlertState(JSON.stringify(baseline.state)), monitor([event(1), event(2, now + 1000)]), now + 1000)
assert.equal(update.incoming.length, 1)
assert.equal(update.state.records[0].read, false)
assert.equal(deliveryCandidates(update.state, update.incoming, now + 1000).length, 1)
let repeated = collectLocalAlerts(parseAlertState(JSON.stringify(update.state)), monitor([event(1), event(2)]), now + 2000)
assert.equal(repeated.incoming.length, 0)
let cooldown = collectLocalAlerts(update.state, monitor([event(3, now + 2000)]), now + 2000)
assert.equal(deliveryCandidates(cooldown.state, cooldown.incoming, now + 2000).length, 0)
assert.equal(cooldown.state.records[0].delivery, 'cooldown')
let offline = collectLocalAlerts(cooldown.state, monitor([event(4, now + 3000)]), now + 3600000)
assert.equal(offline.incoming.length, 0)
assert.equal(offline.state.records.find(item => item.id === 'e4').read, false)
assert.throws(() => parseAlertState('{"version":1}'))
console.log('PASS: notification baseline, persistence, deduplication, cooldown and offline records')
