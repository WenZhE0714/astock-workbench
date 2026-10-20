export const legacyPriceAlertKey = 'astock.replay-price-alerts.v1'
export function phaseLabel(value) { return ({ active:'监控中',paused:'已暂停',triggered:'已触发',expired:'已到期',deleted:'已删除' })[value] || '待检查' }
export function deliveryLabel(item) { return ({ submitted:'已提交系统',pending:'等待发送',failed:'发送失败',expired:'通知已过期',muted:'当天静音' })[item.notification?.status] || (item.event ? '触发已记录' : '--') }
export function ruleFromLegacy(item) {
  return { symbol:item.symbol,name:item.name||item.symbol,direction:item.direction,price:Number(item.price),expires_on:item.expires_on,
    source:item.source_kind&&item.source_date&&item.source_fingerprint ? {id:item.source_kind,date:item.source_date,fingerprint:item.source_fingerprint} : undefined }
}
export function pendingLegacyAlerts(value) { return Array.isArray(value) ? value.filter(a=>a&&typeof a.id==='string'&&typeof a.symbol==='string'&&['above','below'].includes(a.direction)&&Number.isFinite(a.price)&&a.price>0&&a.enabled&&!a.fired_at) : [] }
