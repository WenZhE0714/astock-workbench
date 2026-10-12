export const validationPatterns = [
  ['double-bottom','双底'], ['double-top','双顶'], ['ascending-triangle','上升三角形'], ['descending-triangle','下降三角形'],
  ['head-shoulders-bottom','头肩底'], ['head-shoulders-top','头肩顶'], ['bull-flag','看涨旗形'], ['bear-flag','看跌旗形'],
]
export function validationPercent(value) { return value == null || !Number.isFinite(Number(value)) ? '--' : `${Number(value).toFixed(2)}%` }
export function validationState(state) { return {watching:'待确认',forming:'待确认',confirmed:'已确认',invalidated:'已失效',expired:'观察到期',mature:'已成熟',pending:'待成熟',unavailable:'数据缺失',unconfirmed:'未确认'}[state] || '--' }
export function validationOutcome(sample,horizon) { return sample?.outcomes?.find(item => item.horizon === Number(horizon)) || {state:'unconfirmed'} }
export function validationSymbols(value) { return [...new Set(String(value || '').split(/[\s,，、;；]+/).map(item=>item.trim()).filter(Boolean))] }
export function validationTone(value) { return value == null ? '' : value>0 ? 'gain' : value<0 ? 'loss' : '' }
export function validationCoverageStatus(item) {
  return {aligned:'已对齐基准日期',partial:'覆盖有缺口',unavailable:'覆盖无法核验',benchmark:'基准日期参照'}[item?.status] || '旧归档未记录覆盖诊断'
}
export function validationActionState(check, numericQuality = false) {
  return {clear:'未发现支持范围内事件',affected:'事件隔离',unverified:'公司行动未核验',adjusted:numericQuality ? '复权校验通过' : '分红已复权对齐'}[check?.state] || '旧口径未隔离'
}
export function validationQualityState(state) { return {passed:'校验通过',mismatch:'数值不一致',unverified:'未核验'}[state] || '未记录' }
export function validationPriceBasis(value) { return value === 'hfq' ? '后复权研究' : '未复权' }
export function validationActionKinds(kinds) {
  return (kinds || []).map(kind=>({'cash-dividend':'现金分红','bonus-share':'送股',capitalization:'转增'}[kind] || kind)).join('、')
}
export function validationSourceURL(value) {
  try { const url = new URL(value); return url.protocol === 'https:' ? url.href : '' } catch { return '' }
}
