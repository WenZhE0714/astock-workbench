export function shanghaiToday(now = new Date()) {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now)
  const values = Object.fromEntries(parts.map(part => [part.type, part.value]))
  return `${values.year}-${values.month}-${values.day}`
}

export function validCalendarMonth(month) {
  return /^\d{4}-(0[1-9]|1[0-2])$/.test(month || '') && Number(month.slice(0, 4)) >= 1900 && Number(month.slice(0, 4)) <= 2200
}

export function shiftCalendarMonth(month, delta) {
  if (!validCalendarMonth(month)) return month
  const [year, number] = month.split('-').map(Number)
  const shifted = new Date(Date.UTC(year, number - 1 + delta, 1)).toISOString().slice(0, 7)
  return validCalendarMonth(shifted) ? shifted : month
}

export function calendarCells(month) {
  if (!validCalendarMonth(month)) return []
  const [year, number] = month.split('-').map(Number)
  const start = (new Date(Date.UTC(year, number - 1, 1)).getUTCDay() + 6) % 7
  const count = new Date(Date.UTC(year, number, 0)).getUTCDate()
  return Array.from({ length: 42 }, (_, index) => {
    const day = index - start + 1
    return day < 1 || day > count ? { key: `empty-${index}`, date: '', day: '' } : { key: `${month}-${day}`, date: `${month}-${String(day).padStart(2, '0')}`, day }
  })
}

export function calendarValue(totals, source) {
  const value = source === 'shadow' ? totals?.net_profit : totals?.average_r
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

export function compactCalendarValue(value) {
  if (value == null) return '--'
  const magnitude = Math.abs(value)
  const text = magnitude >= 1000 ? new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 }).format(magnitude) : magnitude.toFixed(magnitude >= 100 ? 0 : 1)
  return `${value < 0 ? '-' : value > 0 ? '+' : ''}${text}`
}
