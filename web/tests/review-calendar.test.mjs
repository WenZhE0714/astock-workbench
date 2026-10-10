import assert from 'node:assert/strict'
import { calendarCells, shiftCalendarMonth, validCalendarMonth, shanghaiToday, calendarValue, compactCalendarValue } from '../src/review-calendar.mjs'

assert.equal(calendarCells('2024-02').filter(cell => cell.date).length, 29)
assert.equal(calendarCells('2100-02').filter(cell => cell.date).length, 28)
assert.equal(calendarCells('2026-09').length, 42)
assert.equal(calendarCells('2026-09')[1].date, '2026-09-01')
assert.equal(shiftCalendarMonth('2026-12', 1), '2027-01')
assert.equal(shiftCalendarMonth('2026-01', -1), '2025-12')
assert.equal(shiftCalendarMonth('1900-01', -1), '1900-01')
assert.equal(validCalendarMonth('2026-2'), false)
assert.equal(shanghaiToday(new Date('2026-09-30T16:30:00Z')), '2026-10-01')
assert.equal(calendarValue({ average_r: 0, net_profit: 123 }, 'manual'), 0)
assert.equal(calendarValue({ average_r: 2, net_profit: null }, 'shadow'), null)
assert.equal(compactCalendarValue(null), '--')
assert.ok(compactCalendarValue(1234567).length <= 6)
console.log('PASS: calendar boundaries, Shanghai dates, units and missing values')
