import { ACCESS_WEEKDAYS, alwaysAccessSchedule, normalizeAccessSchedule, validateAccessSchedule, type AccessSchedule, type AccessTimeWindow, type AccessWeekday } from '../schedule'

type AccessScheduleEditorProps = {
  value: AccessSchedule
  disabled?: boolean
  onChange: (value: AccessSchedule) => void
}

const defaultWindow: AccessTimeWindow = { days: ['mon', 'tue', 'wed', 'thu', 'fri'], start: '20:00', end: '22:00' }

export function AccessScheduleEditor({ value, disabled = false, onChange }: AccessScheduleEditorProps) {
  const error = validateAccessSchedule(value)
  const updateWindow = (index: number, update: Partial<AccessTimeWindow>) => {
    const windows = value.windows.map((window, windowIndex) => windowIndex === index ? { ...window, ...update } : window)
    onChange(normalizeAccessSchedule({ mode: 'weekly', windows }))
  }
  const toggleDay = (index: number, day: AccessWeekday) => {
    const window = value.windows[index]
    if (!window) return
    const days = window.days.includes(day) ? window.days.filter((item) => item !== day) : [...window.days, day]
    updateWindow(index, { days })
  }
  const removeWindow = (index: number) => onChange(normalizeAccessSchedule({ mode: 'weekly', windows: value.windows.filter((_, windowIndex) => windowIndex !== index) }))

  return <div className="access-schedule-editor">
    <div className="access-schedule-intro"><strong>在以下时段阻断访问</strong><span>时间按目标 RouterOS 的本地时区执行；窗口外不阻断。</span></div>
    <div className="access-schedule-mode" role="radiogroup" aria-label="时间阻断模式">
      <label className={value.mode === 'always' ? 'active' : ''}>
        <input type="radio" checked={value.mode === 'always'} disabled={disabled} onChange={() => onChange(alwaysAccessSchedule())} />
        <span><strong>始终阻断</strong><small>全天阻断匹配访问</small></span>
      </label>
      <label className={value.mode === 'weekly' ? 'active' : ''}>
        <input type="radio" checked={value.mode === 'weekly'} disabled={disabled} onChange={() => onChange(normalizeAccessSchedule({ mode: 'weekly', windows: value.windows.length ? value.windows : [defaultWindow] }))} />
        <span><strong>按时间段阻断</strong><small>仅在下面的时间段阻断访问</small></span>
      </label>
    </div>
    {value.mode === 'weekly' ? <div className="access-schedule-windows">
      {value.windows.map((window, index) => <div className="access-schedule-window" key={index}>
        <div className="access-schedule-window-head"><strong>阻断时间段 {index + 1}</strong><button type="button" className="access-schedule-remove" disabled={disabled} onClick={() => removeWindow(index)}>删除</button></div>
        <div className="access-schedule-days" role="group" aria-label={`阻断时间段 ${index + 1} 的星期`}>
          {ACCESS_WEEKDAYS.map((day) => <label key={day.value} className={window.days.includes(day.value) ? 'active' : ''}><input type="checkbox" checked={window.days.includes(day.value)} disabled={disabled} onChange={() => toggleDay(index, day.value)} /><span>周{day.label}</span></label>)}
        </div>
        <div className="access-schedule-times"><label>开始<input type="time" value={window.start} disabled={disabled} onChange={(event) => updateWindow(index, { start: event.target.value })} /></label><span>至</span><label>结束<input type="time" value={window.end} disabled={disabled} onChange={(event) => updateWindow(index, { end: event.target.value })} /></label></div>
      </div>)}
      <button type="button" className="access-schedule-add" disabled={disabled || value.windows.length >= 16} onClick={() => onChange(normalizeAccessSchedule({ mode: 'weekly', windows: [...value.windows, defaultWindow] }))}>+ 添加阻断时间段</button>
    </div> : null}
    {error ? <p className="access-schedule-error" role="alert">{error}</p> : null}
  </div>
}
