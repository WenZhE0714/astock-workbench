export const validationPatterns = [
  ['double-bottom','双底'], ['double-top','双顶'], ['ascending-triangle','上升三角形'], ['descending-triangle','下降三角形'],
  ['head-shoulders-bottom','头肩底'], ['head-shoulders-top','头肩顶'], ['bull-flag','看涨旗形'], ['bear-flag','看跌旗形'],
]
export function validationPercent(value) { return value == null || !Number.isFinite(Number(value)) ? '--' : `${Number(value).toFixed(2)}%` }
export function validationState(state) { return {watching:'待确认',forming:'待确认',confirmed:'已确认',invalidated:'已失效',expired:'观察到期',mature:'已成熟',pending:'待成熟',unavailable:'数据缺失',unconfirmed:'未确认'}[state] || '--' }
export function validationOutcome(sample,horizon) { return sample?.outcomes?.find(item => item.horizon === Number(horizon)) || {state:'unconfirmed'} }
export function validationSymbols(value) { return [...new Set(String(value || '').split(/[\s,，、;；]+/).map(item=>item.trim()).filter(Boolean))] }
export function validationTone(value) { return value == null ? '' : value>0 ? 'gain' : value<0 ? 'loss' : '' }
