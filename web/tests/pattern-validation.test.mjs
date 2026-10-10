import assert from 'node:assert/strict'
import { validationPatterns, validationSymbols, validationPercent, validationOutcome, validationState } from '../src/pattern-validation.mjs'

assert.equal(validationPatterns.length,8)
assert.deepEqual(validationSymbols('002080,600519，002080\n000001'),['002080','600519','000001'])
assert.equal(validationPercent(null),'--')
assert.equal(validationPercent(0),'0.00%')
assert.equal(validationPercent(-2),'-2.00%')
assert.equal(validationState('pending'),'待成熟')
assert.equal(validationState('unavailable'),'数据缺失')
assert.equal(validationOutcome({outcomes:[{horizon:5,state:'mature',direction_return:0}]},5).direction_return,0)
assert.equal(validationOutcome(null,20).state,'unconfirmed')
console.log('PASS: validation filters, missing values, states and explicit zero returns')
