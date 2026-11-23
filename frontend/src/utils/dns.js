// DNS 配置提示：与旧版 dnsRecordsForInput 保持一致

export function normalizeDomain(domain) {
  return String(domain || '').trim().toLowerCase().replace(/\.$/, '')
}

export function domainBase(domain) {
  const d = normalizeDomain(domain)
  return d.startsWith('*.') ? d.slice(2) : d
}

export function dnsRecordsFor(domain, serverIP, serverHostname) {
  const base = domainBase(domain) || 'example.com'
  const ip = serverIP || '<服务器IP>'
  const mxTarget = serverHostname || `mail.${base}`
  const records = [
    { type: 'MX', host: base, value: mxTarget, priority: 10, note: '单域名' },
    { type: 'MX', host: `*.${base}`, value: mxTarget, priority: 10, note: '通配子域' },
    { type: 'TXT', host: base, value: `v=spf1 ip4:${ip} ~all`, priority: '', note: 'SPF' },
  ]
  if (!serverHostname) records.push({ type: 'A', host: mxTarget, value: ip, priority: '', note: '邮件服务器' })
  return records
}

export function mxKindLabel(kind) {
  if (kind === 'wildcard') return '通配子域'
  if (kind === 'single' || kind === 'base') return '单域名'
  return '域名'
}

// 简单的域名合法性校验（给出即时提示用，最终以后端为准）
export function isValidDomain(d) {
  const v = domainBase(d)
  return /^(?=.{1,253}$)([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/.test(v)
}
