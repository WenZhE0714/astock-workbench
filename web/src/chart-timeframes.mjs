export function matchingTimeframes(report, analysis) {
  if (!report || !analysis || report.version !== 'timeframes-v1' || report.symbol !== analysis.symbol || report.data_date !== analysis.data_date || report.base_fingerprint !== analysis.fingerprint) return null
  return report
}

export function timeframeAlignmentLabel(state) {
  return { aligned_bullish:'同向偏多', aligned_bearish:'同向偏空', conflict:'周期冲突', mixed:'方向待明', insufficient:'样本不足' }[state] || '等待对照'
}

export function timeframeTrendLabel(state) {
  return { bullish:'偏多', bearish:'偏空', sideways:'整理', insufficient:'样本不足' }[state] || '--'
}

export function timeframeRangeLabel(state) {
  return { above:'收盘高于区间', below:'收盘低于区间', inside:'收盘仍在区间', unavailable:'区间样本不足' }[state] || '--'
}
