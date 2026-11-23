// 时间与数字格式化

export function formatDate(s) {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

export function formatFull(s) {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString('zh-CN', { hour12: false })
}

export function timeAgo(s, now = Date.now()) {
  if (!s) return '—'
  const diff = now - new Date(s).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return '刚刚'
  if (mins < 60) return `${mins} 分钟前`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs} 小时前`
  return `${Math.floor(hrs / 24)} 天前`
}

// 已过期的邮箱后端会延迟清理，界面上直接当作不存在
export function isExpired(mb, now = Date.now()) {
  return expiryInfo(mb, now).mins <= 0
}

// 邮箱剩余时间：level 用于配色（ok / warn / bad / none）
export function expiryInfo(mb, now = Date.now()) {
  if (!mb?.expires_at) return { level: 'none', label: '永不过期', pct: 100, mins: Infinity }
  const exp = new Date(mb.expires_at).getTime()
  if (Number.isNaN(exp) || new Date(mb.expires_at).getFullYear() < 2000) {
    return { level: 'none', label: '永不过期', pct: 100, mins: Infinity }
  }
  const created = mb.created_at ? new Date(mb.created_at).getTime() : exp - 3600000
  const left = exp - now
  if (left <= 0) return { level: 'bad', label: '已过期', pct: 0, mins: 0 }
  const mins = Math.ceil(left / 60000)
  const total = Math.max(exp - created, 1)
  const pct = Math.max(2, Math.min(100, (left / total) * 100))
  let label
  if (mins < 60) label = `${mins} 分钟`
  else if (mins < 1440) label = `${Math.floor(mins / 60)} 小时 ${mins % 60} 分`
  else label = `${Math.floor(mins / 1440)} 天`
  const level = mins <= 5 ? 'bad' : mins <= 15 ? 'warn' : 'ok'
  return { level, label, pct, mins }
}

export function num(v) {
  if (v === null || v === undefined || v === '') return '—'
  const n = Number(v)
  return Number.isFinite(n) ? n.toLocaleString('zh-CN') : String(v)
}

export function bytes(n) {
  if (!n && n !== 0) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1048576) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1048576).toFixed(1)} MB`
}

// 发件人显示名："Name <a@b.com>" → Name
export function senderName(s) {
  if (!s) return '(无发件人)'
  const m = String(s).match(/^\s*"?([^"<]+?)"?\s*<[^>]+>\s*$/)
  return m ? m[1].trim() : String(s).trim()
}

export function initial(s) {
  const n = senderName(s)
  return (n.match(/[A-Za-z0-9一-龥]/)?.[0] || '?').toUpperCase()
}
