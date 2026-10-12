import assert from 'node:assert/strict'
import { validationPatterns, validationSymbols, validationPercent, validationOutcome, validationState, validationActionState, validationSourceURL } from '../src/pattern-validation.mjs'

assert.equal(validationPatterns.length,8)
assert.deepEqual(validationSymbols('002080,600519，002080\n000001'),['002080','600519','000001'])
assert.equal(validationPercent(null),'--')
assert.equal(validationPercent(0),'0.00%')
assert.equal(validationPercent(-2),'-2.00%')
assert.equal(validationState('pending'),'待成熟')
assert.equal(validationState('unavailable'),'数据缺失')
assert.equal(validationOutcome({outcomes:[{horizon:5,state:'mature',direction_return:0}]},5).direction_return,0)
assert.equal(validationOutcome(null,20).state,'unconfirmed')
assert.equal(validationActionState(null),'旧口径未隔离')
assert.equal(validationActionState({state:'unverified'}),'公司行动未核验')
assert.equal(validationActionState({state:'adjusted'}),'分红已复权对齐')
assert.equal(validationActionState({state:'adjusted'},true),'复权校验通过')
assert.equal(validationSourceURL('javascript:alert(1)'), '')
assert.equal(validationSourceURL('https://data.eastmoney.com/yjfp/'), 'https://data.eastmoney.com/yjfp/')
console.log('PASS: validation filters, missing values, states and explicit zero returns')
