export function directoryNameError(name: string): string {
  if (!name.trim()) return '请输入文件夹名称'
  if (
    name === '.' ||
    name === '..' ||
    name.includes('/') ||
    name.includes('\\') ||
    Array.from(name).some(
      (c) =>
        c.charCodeAt(0) < 32 ||
        (c.charCodeAt(0) >= 127 && c.charCodeAt(0) <= 159),
    )
  )
    return '名称不能包含路径分隔符或控制字符，不能为 . 或 ..'
  if (new TextEncoder().encode(name).length > 128)
    return '名称不能超过 128 字节'
  return ''
}

export function directoryPath(value: string): string | null {
  const parts = value.split('/').filter(Boolean)
  if (
    new TextEncoder().encode(value).length > 1024 ||
    value.includes('\\') ||
    parts.some((p) => p === '.' || p === '..') ||
    Array.from(value).some(
      (c) =>
        c.charCodeAt(0) < 32 ||
        (c.charCodeAt(0) >= 127 && c.charCodeAt(0) <= 159),
    )
  )
    return null
  return '/' + parts.join('/')
}

export function parentDirectory(path: string): string {
  return path.slice(0, path.lastIndexOf('/')) || '/'
}

export function replaceDirectory(
  value: string,
  oldPath: string,
  newPath: string,
): string {
  const p = directoryPath(value)
  if (!value || !p || (p !== oldPath && !p.startsWith(oldPath + '/')))
    return value
  return newPath ? newPath + p.slice(oldPath.length) : ''
}
