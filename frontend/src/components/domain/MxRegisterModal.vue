<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Zap, Clock, CircleCheck, CircleX } from '@lucide/vue'
import BaseModal from '../ui/BaseModal.vue'
import DnsTable from './DnsTable.vue'
import MxDetails from './MxDetails.vue'
import { api } from '../../api/client'
import { toast } from '../../composables/feedback'
import { useSession } from '../../stores/session'
import { dnsRecordsFor, isValidDomain, normalizeDomain } from '../../utils/dns'
import { timeAgo } from '../../utils/format'

// 提交域名做 MX 自动验证：通过立即激活，否则进入待验证队列并在弹窗内轮询
const props = defineProps({ open: { type: Boolean, default: false } })
const emit = defineEmits(['close', 'changed'])

const session = useSession()
const domain = ref('')
const busy = ref(false)
const error = ref('')
const errDetails = ref([])
const result = ref(null) // { state: 'active' | 'pending', domain, records, details }
const attempts = ref(0)
const lastCheck = ref(null)
let timer = null

const records = computed(() =>
  result.value?.records?.length
    ? result.value.records
    : dnsRecordsFor(domain.value || 'example.com', session.settings.smtp_server_ip, session.settings.smtp_hostname),
)
const invalid = computed(() => domain.value.trim() && !isValidDomain(domain.value))

function stop() {
  if (timer) clearInterval(timer)
  timer = null
}

function reset() {
  stop()
  domain.value = ''
  busy.value = false
  error.value = ''
  errDetails.value = []
  result.value = null
  attempts.value = 0
  lastCheck.value = null
}

watch(() => props.open, v => (v ? reset() : stop()))
onBeforeUnmount(stop)

function poll(id) {
  stop()
  timer = setInterval(async () => {
    attempts.value++
    try {
      const d = await api.getDomainStatus(id)
      lastCheck.value = d.mx_checked_at
      if (result.value) result.value.details = d.mx_details || result.value.details
      if (d.status === 'active') {
        stop()
        result.value = { ...result.value, state: 'active' }
        toast.success(`${d.domain} 已通过 MX 验证并激活`)
        emit('changed')
      }
    } catch {
      /* 下次重试 */
    }
  }, 5000)
}

async function submit() {
  const d = normalizeDomain(domain.value)
  if (!d) {
    error.value = '请输入域名'
    return
  }
  busy.value = true
  error.value = ''
  errDetails.value = []
  try {
    const r = await api.submitDomain(d)
    const state = r.status === 'active' ? 'active' : 'pending'
    result.value = { state, domain: r.domain, records: r.dns_required || [], details: r.mx_details || [], message: r.message }
    emit('changed')
    if (state === 'active') toast.success(`${d} 已通过 MX 验证并加入域名池`)
    else if (r.domain?.id) poll(r.domain.id)
  } catch (e) {
    error.value = e.message
    errDetails.value = e.details || []
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseModal :open="open" title="提交域名自动验证" :icon="Zap" size="lg" @close="emit('close')">
    <template v-if="!result">
      <p class="small text-2">
        提交后系统立即检测 MX 记录：已生效则直接加入域名池；未生效则进入待验证队列，后台每 30 秒自动重试，无需手动确认。
      </p>
      <form id="mxr-form" class="field" @submit.prevent="submit">
        <label for="mxr-domain">域名</label>
        <input
          id="mxr-domain"
          v-model="domain"
          class="inp inp-mono"
          placeholder="example.com"
          autocomplete="off"
          spellcheck="false"
          :aria-invalid="invalid ? 'true' : undefined"
        />
        <span v-if="invalid" class="err-t">域名格式看起来不正确</span>
        <span v-else class="hint">填写基础域名，系统会同时检测单域名和通配子域（*.域名）的 MX</span>
      </form>
    </template>

    <div v-else-if="result.state === 'active'" class="notice ok">
      <CircleCheck aria-hidden="true" />
      <div class="grow stack" style="gap: 6px">
        <strong>MX 验证通过</strong>
        <span class="small">域名 <code>{{ result.domain?.domain || domain }}</code> 已加入域名池，现在可以用它创建邮箱。</span>
        <MxDetails :details="result.details" />
      </div>
    </div>

    <div v-else class="notice warn">
      <Clock aria-hidden="true" />
      <div class="grow stack" style="gap: 6px">
        <strong>已加入待验证队列</strong>
        <span class="small">
          DNS 生效后（通常 5–30 分钟）系统会自动激活。已检测 {{ attempts }} 次{{ lastCheck ? `，上次 ${timeAgo(lastCheck)}` : '' }}。
          关闭此窗口不影响验证，可在「域名」页查看进度。
        </span>
        <MxDetails :details="result.details" />
      </div>
    </div>

    <div v-if="error" class="notice bad" role="alert">
      <CircleX aria-hidden="true" />
      <div class="grow stack" style="gap: 6px">
        <span>{{ error }}</span>
        <MxDetails :details="errDetails" />
      </div>
    </div>

    <div v-if="!result || result.state === 'pending'" class="field">
      <span class="field-label">需要在 DNS 服务商添加的记录</span>
      <DnsTable :records="records" compact />
    </div>

    <template #footer>
      <template v-if="!result">
        <button type="button" class="btn btn-secondary" @click="emit('close')">取消</button>
        <button type="submit" form="mxr-form" class="btn btn-primary" :disabled="busy || !!invalid">
          {{ busy ? '检测中…' : error ? '重新提交' : '提交检测' }}
        </button>
      </template>
      <button v-else type="button" class="btn btn-primary" @click="emit('close')">完成</button>
    </template>
  </BaseModal>
</template>
