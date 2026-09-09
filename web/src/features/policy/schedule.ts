export type AccessScheduleMode = 'always' | 'weekly'
export type AccessWeekday = 'mon' | 'tue' | 'wed' | 'thu' | 'fri' | 'sat' | 'sun'
export type AccessTimeWindow = { days: AccessWeekday[]; start: string; end: string }
export type AccessSchedule = { mode: AccessScheduleMode; windows: AccessTimeWindow[] }

export const ACCESS_WEEKDAYS: Array<{ value: AccessWeekday; label: string }> = [
  { value: 'mon', label: '一' }, { value: 'tue', label: '二' }, { value: 'wed', label: '三' },
  { value: 'thu', label: '四' }, { value: 'fri', label: '五' }, { value: 'sat', label: '六' }, { value: 'sun', label: '日' },
]

const weekdayIndex = new Map(ACCESS_WEEKDAYS.map((day, index) => [day.value, index]))

export function alwaysAccessSchedule(): AccessSchedule {
  return { mode: 'always', windows: [] }
}

export function normalizeAccessSchedule(schedule: Partial<AccessSchedule> | null | undefined): AccessSchedule {
  if (!schedule || schedule.mode === undefined || schedule.mode === 'always') return alwaysAccessSchedule()
  const windows = (schedule.windows ?? []).map((window) => ({
    days: Array.from(new Set(window.days)).sort((left, right) => (weekdayIndex.get(left) ?? 99) - (weekdayIndex.get(right) ?? 99)),
    start: window.start,
    end: window.end,
  }))
  return { mode: 'weekly', windows }
}

export function validateAccessSchedule(schedule: AccessSchedule): string | null {
  if (schedule.mode === 'always') return schedule.windows.length === 0 ? null : '始终生效模式不能包含时间段。'
  if (schedule.mode !== 'weekly') return '时间模式无效。'
  if (schedule.windows.length === 0) return '请至少添加一个阻断时间段。'
  if (schedule.windows.length > 16) return '最多支持 16 个阻断时间段。'

  const segments: Array<{ day: number; start: number; end: number }> = []
  for (const window of schedule.windows) {
    if (window.days.length === 0) return '每个时间段至少需要选择一天。'
    const start = parseClock(window.start)
    const end = parseClock(window.end)
    if (start === null || end === null) return '时间必须使用 HH:MM 格式。'
    if (start === end) return '开始和结束时间不能相同。'
    for (const day of window.days) {
      const dayIndex = weekdayIndex.get(day)
      if (dayIndex === undefined) return '星期设置无效。'
      if (start < end) segments.push({ day: dayIndex, start, end })
      else {
        segments.push({ day: dayIndex, start, end: 1440 })
        if (end > 0) segments.push({ day: (dayIndex + 1) % 7, start: 0, end })
      }
    }
  }
  segments.sort((left, right) => left.day - right.day || left.start - right.start || left.end - right.end)
  for (let index = 1; index < segments.length; index += 1) {
    const previous = segments[index - 1]
    const current = segments[index]
    if (previous.day === current.day && current.start < previous.end) return '时间段存在重叠，请合并后再保存。'
  }
  return null
}

export function accessScheduleSummary(schedule: AccessSchedule): string {
  if (schedule.mode === 'always') return '始终'
  if (!schedule.windows.length) return '未设置'
  const first = schedule.windows[0]
  const summary = `${formatDays(first.days)} ${first.start}–${first.end}`
  return schedule.windows.length > 1 ? `${summary}，另有 ${schedule.windows.length - 1} 个时间段` : summary
}

export function accessScheduleWindowLabel(window: AccessTimeWindow): string {
  return `${formatDays(window.days)} ${window.start}–${window.end}`
}

function parseClock(value: string): number | null {
  if (!/^\d{2}:\d{2}$/.test(value)) return null
  const hour = Number(value.slice(0, 2))
  const minute = Number(value.slice(3))
  return hour <= 23 && minute <= 59 ? hour * 60 + minute : null
}

function formatDays(days: AccessWeekday[]): string {
  const labels = days.map((day) => ACCESS_WEEKDAYS.find((item) => item.value === day)?.label ?? day)
  if (labels.length === 7) return '每天'
  if (labels.length === 5 && days.join(',') === 'mon,tue,wed,thu,fri') return '周一至周五'
  if (labels.length === 2 && days.join(',') === 'sat,sun') return '周末'
  return labels.map((label) => `周${label}`).join('、')
}
