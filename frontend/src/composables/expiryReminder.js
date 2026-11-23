import { toast } from './feedback'
import { expiryInfo } from '../utils/format'
import { extendMailbox } from './mailboxBus'

// 到期提醒：邮箱剩余时间进入阈值时提示一次，并提供“续期”按钮。
// 同一邮箱同一到期时间只提醒一次（续期后到期时间变化，会重新计算）。
const THRESHOLDS = [5, 1]
const notified = new Map() // id -> Set(`${expires_at}|${threshold}`)

export function checkExpiry(list, now = Date.now()) {
  for (const mb of list || []) {
    const info = expiryInfo(mb, now)
    if (info.level === 'none' || info.mins <= 0) continue
    const hit = THRESHOLDS.find(t => info.mins <= t)
    if (hit == null) continue
    const key = `${mb.expires_at}|${hit}`
    let seen = notified.get(mb.id)
    if (!seen) notified.set(mb.id, (seen = new Set()))
    if (seen.has(key)) continue
    // 低阈值命中时，把更高阈值也标记为已提醒，避免一次弹两条
    for (const t of THRESHOLDS) if (t >= hit) seen.add(`${mb.expires_at}|${t}`)
    const left = info.mins < 1 ? '不到 1 分钟' : `${Math.ceil(info.mins)} 分钟`
    toast.warn(`${mb.full_address} 还有 ${left}过期`, { label: '续期', run: () => extendMailbox(mb) })
  }
}

export function forgetExpiry(id) {
  notified.delete(id)
}
