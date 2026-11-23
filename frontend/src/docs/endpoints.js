// API 文档数据源：路径、字段、响应均以后端 api/main.go 与各 handler 为准
// path 中的 :param 会在示例代码里替换为 example 值

const T = '2026-09-30T08:00:00Z'
const MB = {
  id: '6f1c2b0e-8a1d-4c55-9a7e-2b3c4d5e6f70',
  account_id: '0b7d5c3a-1e2f-4a6b-9c8d-7e6f5a4b3c2d',
  address: 'k7xq2',
  domain_id: 1,
  full_address: 'k7xq2@example.com',
  created_at: T,
  expires_at: '2026-09-30T08:30:00Z',
}
const DOMAIN = {
  id: 1,
  domain: 'example.com',
  domain_type: 'exact',
  base_domain: 'example.com',
  supports_single: true,
  supports_wildcard: true,
  is_active: true,
  status: 'active',
  created_at: T,
  mx_checked_at: T,
}
const MX_DETAILS = [
  { kind: 'single', name: 'example.com', matched: true, mx_hosts: ['mail.example.com'], status: 'MX记录匹配：mail.example.com → 203.0.113.10' },
  { kind: 'wildcard', name: 'probe.example.com', matched: false, mx_hosts: [], status: '未找到MX记录' },
]
// mx_status 为各检测项的文字汇总，用“；”连接
const MX_STATUS = 'example.com: MX记录匹配：mail.example.com → 203.0.113.10；probe.example.com: 未找到MX记录'
const DNS = [
  { type: 'MX', host: 'example.com', value: 'mail.example.com', priority: 10 },
  { type: 'MX', host: '*.example.com', value: 'mail.example.com', priority: 10 },
  { type: 'TXT', host: 'example.com', value: 'v=spf1 ip4:203.0.113.10 ~all' },
]

const E401 = { code: 401, desc: '缺少或无效的 API Key' }
const E429 = { code: 429, desc: '触发速率限制，响应含 limit、retry_after（秒）' }
const E403 = { code: 403, desc: '需要管理员权限' }
const PAGE = [
  { name: 'page', in: 'query', type: 'int', desc: '页码，默认 1' },
  { name: 'size', in: 'query', type: 'int', desc: '每页条数，1–100，默认 20' },
]

export const groups = [
  {
    id: 'public',
    title: '公共接口',
    desc: '无需 API Key，可直接调用。',
    endpoints: [
      {
        id: 'public-settings',
        method: 'GET',
        path: '/public/settings',
        auth: false,
        title: '获取站点公开配置',
        desc: '返回站点名称、登录方式开关、公告以及生成 DNS 提示所需的邮件服务器信息。',
        response: {
          registration_open: true,
          key_login_enabled: true,
          linuxdo_login_enabled: false,
          github_login_enabled: false,
          site_title: 'TempMail',
          site_logo_url: '',
          smtp_server_ip: '203.0.113.10',
          smtp_hostname: 'mail.example.com',
          announcement: '',
        },
      },
      {
        id: 'public-stats',
        method: 'GET',
        path: '/public/stats',
        auth: false,
        title: '平台统计',
        desc: '全平台的邮箱、邮件、域名与账户数量，结果缓存 5 秒。total_emails 为累计收件数；过期邮箱会被自动删除，total_mailboxes 与 active_mailboxes 反映的是当前数量而非历史累计。与 GET /api/stats 返回相同。',
        response: { total_mailboxes: 216, active_mailboxes: 214, total_emails: 12840, active_domains: 5, pending_domains: 1, total_accounts: 87 },
      },
      {
        id: 'public-register',
        method: 'POST',
        path: '/public/register',
        auth: false,
        title: '注册账户',
        desc: '需管理员开启「开放自行注册」。成功后返回 API Key，只显示这一次，请妥善保存。',
        body: [{ name: 'username', type: 'string', required: true, desc: '用户名，2–64 个字符' }],
        example: { username: 'alice' },
        status: 201,
        response: { id: '0b7d5c3a-1e2f-4a6b-9c8d-7e6f5a4b3c2d', username: 'alice', api_key: 'tm_xxxxxxxxxxxxxxxx', message: "registration successful — save your API key, it won't be shown again" },
        errors: [
          { code: 400, desc: '用户名不合法' },
          { code: 403, desc: '注册未开放' },
          { code: 409, desc: '用户名已存在' },
        ],
      },
      {
        id: 'public-key-login',
        method: 'POST',
        path: '/public/key-login',
        auth: false,
        title: '校验 API Key',
        desc: '网页登录使用的接口，用于验证 Key 并返回账户信息。受「API Key 登录」开关控制；程序调用业务接口时直接携带 Key 即可，无需先调用此接口。',
        body: [{ name: 'api_key', type: 'string', required: true, desc: '账户 API Key' }],
        example: { api_key: 'tm_xxxxxxxxxxxxxxxx' },
        response: { api_key: 'tm_xxxxxxxxxxxxxxxx', id: '0b7d5c3a-1e2f-4a6b-9c8d-7e6f5a4b3c2d', username: 'alice', is_admin: false, created_at: T },
        errors: [{ code: 401, desc: 'API Key 无效' }, { code: 403, desc: 'API Key 登录已关闭' }],
      },
      {
        id: 'public-oauth',
        method: 'GET',
        path: '/public/auth/{provider}',
        auth: false,
        title: 'OAuth 登录',
        desc: 'provider 为 linuxdo 或 github。浏览器跳转到该地址即可发起授权，回调完成后会自动写入登录状态并回到首页。仅用于网页登录。',
        noCode: true,
      },
    ],
  },
  {
    id: 'account',
    title: '账户',
    endpoints: [
      {
        id: 'me',
        method: 'GET',
        path: '/api/me',
        title: '当前账户信息',
        desc: '可用来检查 API Key 是否有效。',
        response: { id: '0b7d5c3a-1e2f-4a6b-9c8d-7e6f5a4b3c2d', username: 'alice', is_admin: false, created_at: T },
        errors: [E401],
      },
      {
        id: 'stats',
        method: 'GET',
        path: '/api/stats',
        title: '平台统计',
        desc: '与 /public/stats 相同，需鉴权。',
        response: { total_mailboxes: 216, active_mailboxes: 214, total_emails: 12840, active_domains: 5, pending_domains: 1, total_accounts: 87 },
        errors: [E401],
      },
    ],
  },
  {
    id: 'mailbox',
    title: '邮箱',
    desc: '邮箱创建后会在管理员设置的有效期（默认 30 分钟）后自动删除，expires_at 为到期时间。',
    endpoints: [
      {
        id: 'mailbox-create',
        method: 'POST',
        path: '/api/mailboxes',
        title: '创建临时邮箱',
        desc: '所有字段都可选。不传 mode 且未指定域名时，随机生成单域名或多级子域邮箱；指定了域名但未传 mode 时按单域名处理。',
        body: [
          { name: 'address', type: 'string', desc: '@ 前的本地部分，只允许字母、数字、- 和 _；留空随机生成' },
          { name: 'domain', type: 'string', desc: '指定域名（须为已启用域名），如 example.com' },
          { name: 'domain_id', type: 'int', desc: '按 ID 指定域名，优先于 domain' },
          { name: 'mode', type: 'string', desc: 'single = 单域名；multi = 多级子域（需域名支持通配 MX）' },
          { name: 'subdomain', type: 'string', desc: 'multi 模式下自定义子域前缀，如 a.b.c；留空随机' },
        ],
        example: { mode: 'single', address: 'mytest', domain: 'example.com' },
        variants: [
          { label: '全部随机', body: {} },
          { label: '单域名 + 指定前缀', body: { mode: 'single', address: 'mytest', domain: 'example.com' } },
          { label: '多级子域', body: { mode: 'multi', domain: 'example.com', subdomain: 'shop.news' } },
        ],
        status: 201,
        response: { mailbox: MB },
        errors: [
          { code: 400, desc: 'mode 不合法、域名不存在或未启用、域名不支持所选模式、subdomain 格式错误' },
          { code: 409, desc: '地址已被占用，换一个 address 或留空重试' },
          { code: 503, desc: '没有可用的域名' },
          E401,
          E429,
        ],
      },
      {
        id: 'mailbox-list',
        method: 'GET',
        path: '/api/mailboxes',
        title: '邮箱列表',
        desc: '列出当前账户的邮箱。',
        params: PAGE,
        query: { page: 1, size: 20 },
        response: { data: [MB], total: 1, page: 1, size: 20 },
        errors: [E401],
      },
      {
        id: 'mailbox-delete',
        method: 'DELETE',
        path: '/api/mailboxes/:id',
        title: '删除邮箱',
        desc: '立即删除邮箱及其中的全部邮件。',
        params: [{ name: 'id', in: 'path', type: 'uuid', required: true, desc: '邮箱 ID', example: MB.id }],
        response: { message: 'mailbox deleted' },
        errors: [{ code: 400, desc: 'ID 格式错误' }, { code: 404, desc: '邮箱不存在或不属于当前账户' }, E401],
      },
    ],
  },
  {
    id: 'email',
    title: '邮件',
    endpoints: [
      {
        id: 'email-list',
        method: 'GET',
        path: '/api/mailboxes/:id/emails',
        title: '收件列表',
        desc: '按收件时间倒序返回邮件摘要（不含正文）。轮询这个接口即可等待新邮件。',
        params: [{ name: 'id', in: 'path', type: 'uuid', required: true, desc: '邮箱 ID', example: MB.id }, ...PAGE],
        response: {
          data: [{ id: '9a8b7c6d-5e4f-4a3b-2c1d-0e9f8a7b6c5d', sender: 'GitHub <noreply@github.com>', subject: '[GitHub] Your verification code', size_bytes: 5120, received_at: T }],
          total: 1,
          page: 1,
          size: 20,
        },
        errors: [{ code: 404, desc: '邮箱不存在或不属于当前账户' }, E401],
      },
      {
        id: 'email-get',
        method: 'GET',
        path: '/api/mailboxes/:id/emails/:email_id',
        title: '读取邮件',
        desc: '返回完整内容，包括纯文本正文、HTML 正文和原始邮件。',
        params: [
          { name: 'id', in: 'path', type: 'uuid', required: true, desc: '邮箱 ID', example: MB.id },
          { name: 'email_id', in: 'path', type: 'uuid', required: true, desc: '邮件 ID', example: '9a8b7c6d-5e4f-4a3b-2c1d-0e9f8a7b6c5d' },
        ],
        response: {
          email: {
            id: '9a8b7c6d-5e4f-4a3b-2c1d-0e9f8a7b6c5d',
            mailbox_id: MB.id,
            sender: 'GitHub <noreply@github.com>',
            subject: '[GitHub] Your verification code',
            body_text: 'Your verification code is 482915.',
            body_html: '<p>Your verification code is <b>482915</b>.</p>',
            raw_message: 'Received: from …',
            size_bytes: 5120,
            received_at: T,
          },
        },
        errors: [{ code: 404, desc: '邮箱或邮件不存在' }, E401],
      },
      {
        id: 'email-delete',
        method: 'DELETE',
        path: '/api/mailboxes/:id/emails/:email_id',
        title: '删除邮件',
        params: [
          { name: 'id', in: 'path', type: 'uuid', required: true, desc: '邮箱 ID', example: MB.id },
          { name: 'email_id', in: 'path', type: 'uuid', required: true, desc: '邮件 ID', example: '9a8b7c6d-5e4f-4a3b-2c1d-0e9f8a7b6c5d' },
        ],
        response: { message: 'email deleted' },
        errors: [{ code: 404, desc: '邮箱或邮件不存在' }, E401],
      },
    ],
  },
  {
    id: 'domain',
    title: '域名',
    endpoints: [
      {
        id: 'domain-list',
        method: 'GET',
        path: '/api/domains',
        title: '域名列表',
        desc: '列出域名池中的全部域名。status 为 active / pending / disabled；supports_single 与 supports_wildcard 表示可用的生成模式。',
        response: { domains: [DOMAIN] },
        errors: [E401],
      },
      {
        id: 'domain-submit',
        method: 'POST',
        path: '/api/domains/submit',
        title: '提交域名自动验证',
        desc: '立即检测 MX：通过则直接加入域名池（201）；未通过则进入待验证队列（202），后台每 30 秒自动重试。dns_required 为需要添加的 DNS 记录。',
        body: [{ name: 'domain', type: 'string', required: true, desc: '基础域名，如 example.com' }],
        example: { domain: 'example.com' },
        status: 201,
        response: { domain: DOMAIN, status: 'active', mx_status: MX_STATUS, mx_details: MX_DETAILS, dns_required: DNS, message: 'MX验证通过，域名已立即加入域名池' },
        errors: [{ code: 400, desc: '缺少 domain' }, E401],
        extra: '未通过时返回 202 Accepted，status 为 pending，结构相同。',
      },
      {
        id: 'domain-status',
        method: 'GET',
        path: '/api/domains/:id/status',
        title: '查询域名验证状态',
        desc: '用于轮询待验证域名；会实时检测一次 MX 并返回各项结果。',
        params: [{ name: 'id', in: 'path', type: 'int', required: true, desc: '域名 ID', example: 1 }],
        response: {
          id: 1,
          domain: 'example.com',
          supports_single: true,
          supports_wildcard: false,
          status: 'pending',
          is_active: false,
          mx_checked_at: T,
          mx_status: MX_STATUS,
          mx_details: MX_DETAILS,
        },
        errors: [{ code: 404, desc: '域名不存在' }, E401],
      },
    ],
  },
  {
    id: 'admin',
    title: '管理员',
    desc: '以下接口需要管理员账户的 API Key。',
    admin: true,
    endpoints: [
      {
        id: 'admin-accounts-list',
        method: 'GET',
        path: '/api/admin/accounts',
        title: '账户列表',
        params: PAGE,
        query: { page: 1, size: 20 },
        response: {
          data: [{ id: '0b7d5c3a-1e2f-4a6b-9c8d-7e6f5a4b3c2d', username: 'alice', api_key: 'tm_xxxxxxxxxxxxxxxx', is_admin: false, is_active: true, created_at: T, updated_at: T, mailbox_count: 12, active_mailbox_count: 3, current_email_count: 5, received_email_count: 48 }],
          total: 1,
          page: 1,
          size: 20,
        },
        errors: [E401, E403],
      },
      {
        id: 'admin-accounts-create',
        method: 'POST',
        path: '/api/admin/accounts',
        title: '创建账户',
        body: [{ name: 'username', type: 'string', required: true, desc: '用户名，2–64 个字符' }],
        example: { username: 'bob' },
        status: 201,
        response: { id: '1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f', username: 'bob', api_key: 'tm_xxxxxxxxxxxxxxxx' },
        errors: [{ code: 409, desc: '用户名已存在' }, E401, E403],
      },
      {
        id: 'admin-accounts-delete',
        method: 'DELETE',
        path: '/api/admin/accounts/:id',
        title: '删除账户',
        params: [{ name: 'id', in: 'path', type: 'uuid', required: true, desc: '账户 ID', example: '1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f' }],
        response: { message: 'account deleted' },
        errors: [E401, E403],
      },
      {
        id: 'admin-domains-add',
        method: 'POST',
        path: '/api/admin/domains',
        title: '直接添加域名',
        desc: '跳过 MX 检测直接加入域名池，并返回需要配置的 DNS 记录。',
        body: [{ name: 'domain', type: 'string', required: true, desc: '域名（FQDN）' }],
        example: { domain: 'example.com' },
        status: 201,
        response: { domain: DOMAIN, dns_records: DNS, instructions: '请在域名 example.com 的 DNS 管理面板中添加以上记录。添加后约 5-30 分钟生效。' },
        errors: [{ code: 400, desc: '域名格式错误' }, { code: 409, desc: '域名已存在' }, E401, E403],
      },
      {
        id: 'admin-domains-import',
        method: 'POST',
        path: '/api/admin/domains/mx-import',
        title: '检测 MX 后导入',
        desc: 'MX 检测通过才导入；force 为 true 时即使检测失败也导入。检测失败且未强制时返回 422。',
        body: [
          { name: 'domain', type: 'string', required: true, desc: '域名' },
          { name: 'force', type: 'bool', desc: '跳过检测强制导入，默认 false' },
        ],
        example: { domain: 'example.com', force: false },
        status: 201,
        response: { domain: DOMAIN, mx_status: MX_STATUS, mx_details: MX_DETAILS, mx_matched: true, dns_records: DNS, message: '域名 example.com 已导入域名池，Postfix 将在 60 秒内自动同步' },
        errors: [{ code: 409, desc: '域名已存在' }, { code: 422, desc: 'MX 检测未通过，响应含 mx_details 与 dns_hint' }, E401, E403],
      },
      {
        id: 'admin-domains-register',
        method: 'POST',
        path: '/api/admin/domains/mx-register',
        title: '提交域名自动验证（管理员）',
        desc: '与 POST /api/domains/submit 行为相同。',
        body: [{ name: 'domain', type: 'string', required: true, desc: '基础域名' }],
        example: { domain: 'example.com' },
        status: 202,
        response: { domain: { ...DOMAIN, is_active: false, status: 'pending' }, status: 'pending', server_ip: '203.0.113.10', mx_status: MX_STATUS, mx_details: MX_DETAILS, dns_required: DNS, message: '域名 example.com 已进入待验证队列，后台每30秒自动检测MX记录，通过后自动加入域名池' },
        errors: [E401, E403],
      },
      {
        id: 'admin-domains-refresh',
        method: 'POST',
        path: '/api/admin/domains/refresh-mx',
        title: '刷新全部域名的 MX 能力',
        desc: '重新检测每个域名的单域名与通配子域 MX，并更新 supports_single / supports_wildcard。',
        response: { domains: [DOMAIN] },
        errors: [E401, E403],
      },
      {
        id: 'admin-domains-toggle',
        method: 'PUT',
        path: '/api/admin/domains/:id/toggle',
        title: '启用 / 停用域名',
        params: [{ name: 'id', in: 'path', type: 'int', required: true, desc: '域名 ID', example: 1 }],
        body: [{ name: 'active', type: 'bool', required: true, desc: 'true 启用，false 停用' }],
        example: { active: false },
        response: { message: 'domain updated' },
        errors: [E401, E403],
      },
      {
        id: 'admin-domains-delete',
        method: 'DELETE',
        path: '/api/admin/domains/:id',
        title: '删除域名',
        desc: '域名下仍有邮箱时会拒绝删除，可先停用。',
        params: [{ name: 'id', in: 'path', type: 'int', required: true, desc: '域名 ID', example: 1 }],
        response: { message: 'domain deleted' },
        errors: [{ code: 409, desc: '域名下仍有邮箱，响应含 mailbox_count' }, E401, E403],
      },
      {
        id: 'admin-domains-status',
        method: 'GET',
        path: '/api/admin/domains/:id/status',
        title: '查询域名状态（管理员）',
        desc: '与 GET /api/domains/:id/status 相同。',
        params: [{ name: 'id', in: 'path', type: 'int', required: true, desc: '域名 ID', example: 1 }],
        response: { id: 1, domain: 'example.com', status: 'active', is_active: true, supports_single: true, supports_wildcard: true, mx_checked_at: T, mx_status: MX_STATUS, mx_details: MX_DETAILS },
        errors: [{ code: 404, desc: '域名不存在' }, E401, E403],
      },
      {
        id: 'admin-settings-get',
        method: 'GET',
        path: '/api/admin/settings',
        title: '读取系统设置',
        desc: '返回全部设置（值均为字符串）。Client Secret 不会回显，只返回 *_client_secret_set 表示是否已配置。',
        response: {
          registration_open: 'true',
          key_login_enabled: 'true',
          site_title: 'TempMail',
          mailbox_ttl_minutes: '30',
          max_mailboxes_per_user: '5',
          smtp_server_ip: '203.0.113.10',
          smtp_hostname: 'mail.example.com',
          github_client_secret: '',
          github_client_secret_set: 'false',
        },
        errors: [E401, E403],
      },
      {
        id: 'admin-settings-put',
        method: 'PUT',
        path: '/api/admin/settings',
        title: '更新系统设置',
        desc: '传入要修改的键值对（值都是字符串），未传的键保持不变。Client Secret 传空字符串表示不修改。可用的键：registration_open、key_login_enabled、linuxdo_login_enabled、github_login_enabled、linuxdo_client_id、linuxdo_client_secret、linuxdo_redirect_url、github_client_id、github_client_secret、github_redirect_url、site_title、site_logo_url、announcement、smtp_server_ip、smtp_hostname、default_domain、mailbox_ttl_minutes、max_mailboxes_per_user、rate_limit_enabled。',
        body: [{ name: '<key>', type: 'string', desc: '任意可用设置键，值为字符串' }],
        example: { registration_open: 'true', mailbox_ttl_minutes: '60' },
        response: { message: 'settings updated' },
        errors: [{ code: 400, desc: '包含未知的设置键' }, E401, E403],
      },
    ],
  },
]

// ─── 示例代码生成 ───

function fillPath(ep) {
  let p = ep.path
  for (const prm of ep.params || []) {
    if (prm.in === 'path') p = p.replace(`:${prm.name}`, String(prm.example ?? `<${prm.name}>`))
  }
  if (ep.query) p += `?${new URLSearchParams(ep.query).toString()}`
  return p
}

const needsAuth = ep => ep.auth !== false

export function curl(ep, base, key, body = ep.example) {
  const url = `${base}${fillPath(ep)}`
  const lines = [`curl -s${ep.method !== 'GET' ? ` -X ${ep.method}` : ''} "${url}"`]
  if (needsAuth(ep)) lines.push(`  -H "Authorization: Bearer ${key}"`)
  if (body !== undefined) {
    lines.push('  -H "Content-Type: application/json"')
    lines.push(`  -d '${JSON.stringify(body)}'`)
  }
  return lines.join(' \\\n')
}

export function python(ep, base, key, body = ep.example) {
  const url = `${base}${fillPath(ep)}`
  const args = [`"${url}"`]
  if (needsAuth(ep)) args.push(`headers={"Authorization": "Bearer ${key}"}`)
  if (body !== undefined) args.push(`json=${pyLiteral(body)}`)
  args.push('timeout=15')
  return `import requests\n\nr = requests.${ep.method.toLowerCase()}(\n    ${args.join(',\n    ')},\n)\nr.raise_for_status()\nprint(r.json())`
}

export function js(ep, base, key, body = ep.example) {
  const url = `${base}${fillPath(ep)}`
  const opts = []
  if (ep.method !== 'GET') opts.push(`method: '${ep.method}'`)
  const headers = []
  if (needsAuth(ep)) headers.push(`Authorization: 'Bearer ${key}'`)
  if (body !== undefined) headers.push(`'Content-Type': 'application/json'`)
  if (headers.length) opts.push(`headers: { ${headers.join(', ')} }`)
  if (body !== undefined) opts.push(`body: JSON.stringify(${JSON.stringify(body)})`)
  const o = opts.length ? `, {\n  ${opts.join(',\n  ')},\n}` : ''
  return `const res = await fetch('${url}'${o})\nif (!res.ok) throw new Error((await res.json()).error)\nconsole.log(await res.json())`
}

function pyLiteral(v) {
  return JSON.stringify(v).replace(/\btrue\b/g, 'True').replace(/\bfalse\b/g, 'False').replace(/\bnull\b/g, 'None')
}

// ─── 实战示例（取自旧版文档并按真实响应结构修正） ───

export function recipes(base, key) {
  return [
    {
      id: 'recipe-shell',
      title: '完整流程：创建邮箱 → 等待邮件 → 读取 → 清理',
      lang: 'bash',
      code: `#!/usr/bin/env bash
# 依赖：curl、jq
set -euo pipefail
BASE="${base}"
KEY="${key}"
AUTH=(-H "Authorization: Bearer $KEY")

# 1. 创建临时邮箱
MB=$(curl -s -X POST "$BASE/api/mailboxes" "\${AUTH[@]}" \\
  -H "Content-Type: application/json" -d '{"mode":"single"}')
MB_ID=$(echo "$MB" | jq -r '.mailbox.id')
ADDR=$(echo "$MB" | jq -r '.mailbox.full_address')
echo "邮箱：$ADDR"

# 2. 轮询等待邮件（最多 2 分钟）
for i in $(seq 1 24); do
  LIST=$(curl -s "$BASE/api/mailboxes/$MB_ID/emails" "\${AUTH[@]}")
  EMAIL_ID=$(echo "$LIST" | jq -r '.data[0].id // empty')
  [ -n "$EMAIL_ID" ] && break
  sleep 5
done

# 3. 读取第一封邮件
if [ -n "\${EMAIL_ID:-}" ]; then
  curl -s "$BASE/api/mailboxes/$MB_ID/emails/$EMAIL_ID" "\${AUTH[@]}" \\
    | jq '.email | {sender, subject, body_text}'
else
  echo "没有收到邮件"
fi

# 4. 删除邮箱
curl -s -X DELETE "$BASE/api/mailboxes/$MB_ID" "\${AUTH[@]}"`,
    },
    {
      id: 'recipe-python',
      title: 'Python：等待验证码',
      lang: 'python',
      code: `import re, time, requests

BASE = "${base}"
S = requests.Session()
S.headers["Authorization"] = "Bearer ${key}"

mb = S.post(f"{BASE}/api/mailboxes", json={"mode": "single"}, timeout=15).json()["mailbox"]
print("用这个地址注册：", mb["full_address"])

def wait_code(mailbox_id, timeout=120):
    deadline = time.time() + timeout
    while time.time() < deadline:
        emails = S.get(f"{BASE}/api/mailboxes/{mailbox_id}/emails", timeout=15).json()["data"]
        if emails:
            mail = S.get(f"{BASE}/api/mailboxes/{mailbox_id}/emails/{emails[0]['id']}", timeout=15).json()["email"]
            m = re.search(r"\\b(\\d{4,8})\\b", mail["subject"] + " " + mail["body_text"])
            if m:
                return m.group(1)
        time.sleep(5)
    raise TimeoutError("没有收到验证码")

print("验证码：", wait_code(mb["id"]))
S.delete(f"{BASE}/api/mailboxes/{mb['id']}", timeout=15)`,
    },
    {
      id: 'recipe-load',
      title: '压测示例（wrk / k6）',
      lang: 'bash',
      code: `# wrk：对创建邮箱接口施压（注意 /api 有速率限制）
cat > /tmp/create.lua << 'EOF'
wrk.method = "POST"
wrk.body   = "{}"
wrk.headers["Content-Type"]  = "application/json"
wrk.headers["Authorization"] = "Bearer ${key}"
EOF
wrk -t 4 -c 100 -d 30s --script /tmp/create.lua "${base}/api/mailboxes"

# k6
cat > /tmp/test.js << 'EOF'
import http from 'k6/http'
import { check } from 'k6'
export const options = { vus: 100, duration: '30s' }
export default function () {
  const r = http.post('${base}/api/mailboxes', '{}', {
    headers: { Authorization: 'Bearer ${key}', 'Content-Type': 'application/json' },
  })
  check(r, { '创建成功': res => res.status === 201 })
}
EOF
k6 run /tmp/test.js`,
    },
  ]
}
