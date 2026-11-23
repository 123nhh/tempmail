<script setup>
import { computed, ref, watch } from 'vue'
import { Plus, Search, Zap, CircleCheck, TriangleAlert, Info } from '@lucide/vue'
import BaseModal from '../ui/BaseModal.vue'
import DnsTable from './DnsTable.vue'
import MxDetails from './MxDetails.vue'
import { api } from '../../api/client'
import { toast } from '../../composables/feedback'
import { useSession } from '../../stores/session'
import { dnsRecordsFor, isValidDomain, normalizeDomain } from '../../utils/dns'

// 管理员手动添加：先检测 MX，通过则导入；不通过可选择强制添加
const props = defineProps({ open: { type: Boolean, default: false } })
const emit = defineEmits(['close', 'changed'])

const session = useSession()
const domain = ref('')
const busy = ref(false)
const step = ref('input') // input | failed | done
const failInfo = ref(null)
const done = ref(null) // { domain, records, instructions, forced }

const invalid = computed(() => domain.value.trim() && !isValidDomain(domain.value))
const hintRecords = computed(() => dnsRecordsFor(domain.value || 'example.com', session.settings.smtp_server_ip, session.settings.smtp_hostname))

watch(
  () => props.open,
  v => {
    if (!v) return
    domain.value = ''
    busy.value = false
    step.value = 'input'
    failInfo.value = null
    done.value = null
  },
)

async function check() {
  const d = normalizeDomain(domain.value)
  if (!d) return
  busy.value = true
  try {
    const r = await api.admin.mxImport(d, false)
    done.value = { domain: d, records: r.dns_records || [], details: r.mx_details || [], forced: false }
    step.value = 'done'
    toast.success(`${d} MX 验证通过，已加入域名池`)
    emit('changed')
  } catch (e) {
    if (e.status === 422) {
      failInfo.value = { message: e.message, details: e.details || [] }
      step.value = 'failed'
    } else {
      toast.error(e.status === 409 ? '域名已存在' : `检测失败：${e.message}`)
    }
  } finally {
    busy.value = false
  }
}

async function force() {
  const d = normalizeDomain(domain.value)
  busy.value = true
  try {
    const r = await api.admin.addDomain(d)
    done.value = { domain: d, records: r.dns_records || [], instructions: r.instructions, forced: true }
    step.value = 'done'
    toast.success(`已添加 ${d}`)
    emit('changed')
  } catch (e) {
    toast.error(e.status === 409 ? '域名已存在' : `添加失败：${e.message}`)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseModal :open="open" title="手动添加域名" :icon="Plus" size="lg" @close="emit('close')">
    <template v-if="step !== 'done'">
      <form id="add-domain" class="field" @submit.prevent="check">
        <label for="add-domain-inp">域名</label>
        <input
          id="add-domain-inp"
          v-model="domain"
          class="inp inp-mono"
          placeholder="example.com"
          autocomplete="off"
          spellcheck="false"
          :aria-invalid="invalid ? 'true' : undefined"
          @input="step = 'input'"
        />
        <span v-if="invalid" class="err-t">域名格式看起来不正确</span>
        <span v-else class="hint">先检测 MX 记录，通过后立即加入域名池</span>
      </form>

      <div v-if="step === 'failed'" class="notice warn" role="alert">
        <TriangleAlert aria-hidden="true" />
        <div class="grow stack" style="gap: 6px">
          <strong>MX 记录未检测到</strong>
          <span class="small">请先配置下方 DNS 记录后重新检测；如果确定配置无误，也可以强制添加，跳过检测。</span>
          <MxDetails :details="failInfo?.details" />
        </div>
      </div>

      <div class="field">
        <span class="field-label">需要配置的 DNS 记录</span>
        <DnsTable :records="hintRecords" compact />
      </div>
    </template>

    <template v-else>
      <div class="notice ok">
        <CircleCheck aria-hidden="true" />
        <div class="grow stack" style="gap: 6px">
          <strong>{{ done.forced ? '域名已添加' : 'MX 验证通过' }}：<code>{{ done.domain }}</code></strong>
          <MxDetails v-if="!done.forced" :details="done.details" />
        </div>
      </div>
      <div v-if="done.forced" class="field">
        <span class="field-label">请在 DNS 服务商添加以下记录（一般 5–30 分钟生效）</span>
        <DnsTable :records="done.records" compact />
        <p v-if="done.instructions" class="small muted row"><Info class="i" aria-hidden="true" />{{ done.instructions }}</p>
      </div>
    </template>

    <template #footer>
      <template v-if="step !== 'done'">
        <button type="button" class="btn btn-secondary" @click="emit('close')">取消</button>
        <button v-if="step === 'failed'" type="button" class="btn btn-danger" :disabled="busy" @click="force">
          <Zap aria-hidden="true" />强制添加
        </button>
        <button type="submit" form="add-domain" class="btn btn-primary" :disabled="busy || !domain.trim() || !!invalid">
          <Search aria-hidden="true" />{{ busy ? '检测中…' : step === 'failed' ? '重新检测' : '检测 MX' }}
        </button>
      </template>
      <button v-else type="button" class="btn btn-primary" @click="emit('close')">完成</button>
    </template>
  </BaseModal>
</template>

<style scoped>
.i { width: 14px; height: 14px; }
</style>
