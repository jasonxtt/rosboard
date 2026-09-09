import assert from 'node:assert/strict'
import test from 'node:test'
import { accessScheduleSummary, alwaysAccessSchedule, normalizeAccessSchedule, validateAccessSchedule } from '../src/features/policy/schedule.ts'

test('missing access schedule falls back to always blocking', () => {
  assert.deepEqual(normalizeAccessSchedule(undefined), alwaysAccessSchedule())
  assert.equal(accessScheduleSummary(alwaysAccessSchedule()), '始终')
})

test('weekly schedule validates and normalizes weekdays', () => {
  const schedule = normalizeAccessSchedule({ mode: 'weekly', windows: [{ days: ['fri', 'mon', 'fri'], start: '20:00', end: '22:00' }] })
  assert.deepEqual(schedule.windows[0].days, ['mon', 'fri'])
  assert.equal(validateAccessSchedule(schedule), null)
  assert.equal(accessScheduleSummary(schedule), '周一、周五 20:00–22:00')
})

test('cross-midnight schedule is valid but overlapping expanded windows are rejected', () => {
  const overnight = normalizeAccessSchedule({ mode: 'weekly', windows: [{ days: ['fri'], start: '22:00', end: '07:00' }] })
  assert.equal(validateAccessSchedule(overnight), null)
  const overlap = normalizeAccessSchedule({ mode: 'weekly', windows: [
    { days: ['mon'], start: '22:00', end: '02:00' },
    { days: ['tue'], start: '01:00', end: '03:00' },
  ] })
  assert.match(validateAccessSchedule(overlap) ?? '', /重叠/)
})

test('weekly empty schedule is invalid while always mode remains valid', () => {
  assert.match(validateAccessSchedule({ mode: 'weekly', windows: [] }), /至少添加/)
  assert.equal(validateAccessSchedule(alwaysAccessSchedule()), null)
})
