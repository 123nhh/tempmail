import { reactive } from 'vue'
import { api } from '../api/client'
import { toast } from './feedback'

// 跨页面共享的邮箱事件：打开“新建邮箱”弹窗、通知列表刷新、续期后的同步
export const mailboxBus = reactive({
  createOpen: false,
  version: 0,
  lastCreated: null,
  // 最近一次被更新（续期）的邮箱，列表页据此原地替换，不必整页刷新
  updated: null,
})

export function openCreateMailbox() {
  mailboxBus.createOpen = true
}

export function mailboxesChanged(created = null) {
  if (created) mailboxBus.lastCreated = created
  mailboxBus.version++
}

export function mailboxUpdated(mb) {
  if (mb?.id) mailboxBus.updated = { mailbox: mb, at: Date.now() }
}

// 用新数据替换列表中同 ID 的邮箱
export function patchMailbox(list, mb) {
  const i = list.findIndex(m => m.id === mb.id)
  if (i >= 0) list.splice(i, 1, { ...list[i], ...mb })
}

const extending = new Set()

// 续期：成功返回更新后的邮箱，失败返回 null（已提示）
export async function extendMailbox(mb) {
  if (!mb?.id || extending.has(mb.id)) return null
  extending.add(mb.id)
  try {
    const fresh = await api.extendMailbox(mb.id)
    mailboxUpdated(fresh)
    const t = new Date(fresh.expires_at)
    toast.success(`已续期，${mb.full_address || '邮箱'} 将于 ${t.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })} 到期`)
    return fresh
  } catch (e) {
    if (e.status === 409) toast.warn('已达到最长存活时间，无法继续续期')
    else if (e.status === 404) toast.warn(e.data?.error === 'mailbox not found' ? '邮箱已过期或不存在' : '当前后端版本不支持续期')
    else toast.error(`续期失败：${e.message}`)
    return null
  } finally {
    extending.delete(mb.id)
  }
}

export function isExtending(id) {
  return extending.has(id)
}
