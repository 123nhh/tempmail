<script setup>
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Shuffle } from '@lucide/vue'
import BaseModal from '../ui/BaseModal.vue'
import { api } from '../../api/client'
import { toast } from '../../composables/feedback'
import { mailboxesChanged } from '../../composables/mailboxBus'

const props = defineProps({ open: { type: Boolean, default: false } })
const emit = defineEmits(['close'])

const router = useRouter()

const domains = ref([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')

const address = ref('')
const domainId = ref('')
const mode = ref('single')
const subdomain = ref('')

const LOCAL_RE = /^[A-Za-z0-9_-]+$/
const SUB_RE = /^([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)(\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*$/

const baseOf = d => d.base_domain || String(d.domain || '').replace(/^\*\./, '')

const selected = computed(() => domains.value.find(d => String(d.id) === String(domainId.value)) || null)
// 随机域名时两种模式都可选，由后端决定落在哪个域名
const canSingle = computed(() => !selected.value || !!selected.value.supports_single)
const canMulti = computed(() => !selected.value || !!selected.value.supports_wildcard)
const base = computed(() => (selected.value ? baseOf(selected.value) : 'example.com'))

const preview = computed(() => {
  const local = address.value.trim() || 'random'
  if (mode.value === 'multi') {
    const sub = subdomain.value.trim() || 'a.b.c'
    return `${local}@${sub}.${base.value}`
  }
  return `${local}@${base.value}`
})

const addressError = computed(() => (address.value.trim() && !LOCAL_RE.test(address.value.trim()) ? '只允许字母、数字、连字符和下划线' : ''))
const subError = computed(() =>
  mode.value === 'multi' && subdomain.value.trim() && !SUB_RE.test(subdomain.value.trim())
    ? '只允许字母、数字、连字符和点，每段不能以连字符开头或结尾'
    : '',
)

watch([canSingle, canMulti], ([s, m]) => {
  if (!s && m) mode.value = 'multi'
  if (s && !m) mode.value = 'single'
})

async function loadDomains() {
  loading.value = true
  try {
    const all = await api.domains()
    domains.value = (all || []).filter(d => d.is_active)
    domainId.value = ''
  } catch {
    domains.value = []
  } finally {
    loading.value = false
  }
}

function reset() {
  address.value = ''
  subdomain.value = ''
  mode.value = 'single'
  error.value = ''
  busy.value = false
}

watch(
  () => props.open,
  v => {
    if (v) {
      reset()
      loadDomains()
    }
  },
)

function label(d) {
  const s = d.supports_single
  const w = d.supports_wildcard
  return `${baseOf(d)}${s && w ? '（单域名 / 通配）' : w ? '（通配）' : '（单域名）'}`
}

async function submit() {
  if (addressError.value || subError.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const body = { mode: mode.value }
    const a = address.value.trim()
    if (a) body.address = a
    if (domainId.value) body.domain_id = Number(domainId.value)
    if (mode.value === 'multi' && canMulti.value && subdomain.value.trim()) body.subdomain = subdomain.value.trim()
    const mb = await api.createMailbox(body)
    toast.success(`已创建：${mb.full_address}`)
    mailboxesChanged(mb)
    emit('close')
    if (mb.id) router.push({ name: 'mailbox', params: { id: mb.id } })
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseModal :open="open" title="新建邮箱" :icon="Plus" @close="emit('close')">
    <form id="create-mb" class="stack" @submit.prevent="submit">
      <div class="field">
        <label for="mb-address">地址前缀（@ 之前）</label>
        <input
          id="mb-address"
          v-model="address"
          class="inp inp-mono"
          placeholder="留空则随机生成"
          autocomplete="off"
          spellcheck="false"
          :aria-invalid="addressError ? 'true' : undefined"
          aria-describedby="mb-address-hint"
        />
        <span v-if="addressError" id="mb-address-hint" class="err-t">{{ addressError }}</span>
        <span v-else id="mb-address-hint" class="hint">只允许字母、数字、连字符和下划线</span>
      </div>

      <div class="field">
        <label for="mb-domain">域名</label>
        <select id="mb-domain" v-model="domainId" class="inp" :disabled="loading">
          <option value="">随机选取</option>
          <option v-for="d in domains" :key="d.id" :value="String(d.id)">{{ label(d) }}</option>
        </select>
        <span class="hint">{{ loading ? '正在加载域名…' : `共 ${domains.length} 个可用域名，按该域名的 MX 能力创建` }}</span>
      </div>

      <div v-if="canSingle && canMulti" class="field">
        <span id="mb-mode-l" class="field-label">生成模式</span>
        <div class="seg" role="radiogroup" aria-labelledby="mb-mode-l">
          <button type="button" role="radio" :aria-checked="mode === 'single' ? 'true' : 'false'" @click="mode = 'single'">单域名</button>
          <button type="button" role="radio" :aria-checked="mode === 'multi' ? 'true' : 'false'" @click="mode = 'multi'">
            <Shuffle aria-hidden="true" />多级子域
          </button>
        </div>
      </div>

      <div v-if="mode === 'multi' && canMulti" class="field">
        <label for="mb-sub">自定义多级子域</label>
        <div class="combo">
          <input
            id="mb-sub"
            v-model="subdomain"
            class="inp inp-mono"
            placeholder="留空随机，如 gmail.outlook"
            autocomplete="off"
            spellcheck="false"
            :aria-invalid="subError ? 'true' : undefined"
          />
          <span class="addon">.{{ base }}</span>
        </div>
        <span v-if="subError" class="err-t">{{ subError }}</span>
      </div>

      <div class="notice">
        <span class="small text-2">预览</span>
        <code class="ellipsis">{{ preview }}</code>
      </div>

      <p v-if="error" class="err-t" role="alert">{{ error }}</p>
    </form>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">取消</button>
      <button type="submit" form="create-mb" class="btn btn-primary" :disabled="busy || !!addressError || !!subError">
        {{ busy ? '创建中…' : '创建邮箱' }}
      </button>
    </template>
  </BaseModal>
</template>
