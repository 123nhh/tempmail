// 验证码识别：优先匹配关键词附近的码，其次是主题/正文中独立的 4–8 位数字

const KEYWORDS = /(验证码|校验码|动态码|确认码|激活码|驗證碼|verification code|security code|one[- ]time (?:pass)?code|login code|your code|code is|code:|passcode|OTP|PIN)/i
const NEAR = /[:：\s]*([A-Z0-9]{4,8}|\d{3}[\s-]\d{3})\b/i
const DIGITS = /(?<![\d#.:/\-])(\d{4,8})(?![\d.:/\-])/g

// 排除看起来像年份、日期、价格的数字
function plausible(code) {
  if (/^(19|20)\d{2}$/.test(code)) return false
  if (/^0+$/.test(code)) return false
  return true
}

function normalize(c) {
  return c.replace(/[\s-]/g, '').toUpperCase()
}

function fromKeyword(text) {
  const re = new RegExp(KEYWORDS.source, 'gi')
  let m
  while ((m = re.exec(text))) {
    const tail = text.slice(m.index + m[0].length, m.index + m[0].length + 40)
    const n = tail.match(NEAR)
    if (n) {
      const c = normalize(n[1])
      // 字母数字混合的码必须至少含一位数字，避免匹配普通单词
      if (/\d/.test(c) && plausible(c)) return c
    }
  }
  return null
}

function fromDigits(text) {
  for (const m of text.matchAll(DIGITS)) {
    if (plausible(m[1])) return m[1]
  }
  return null
}

export function extractOtp({ subject = '', text = '', html = '' } = {}) {
  const body = text || htmlToText(html)
  const s = String(subject || '')
  return fromKeyword(s) || fromKeyword(body) || fromDigits(s) || (KEYWORDS.test(body) ? fromDigits(body) : null)
}

export function htmlToText(html) {
  if (!html) return ''
  try {
    const doc = new DOMParser().parseFromString(html, 'text/html')
    doc.querySelectorAll('script,style,head').forEach(n => n.remove())
    return (doc.body?.textContent || '').replace(/\s+/g, ' ').trim()
  } catch {
    return String(html).replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim()
  }
}
