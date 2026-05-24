<script setup lang="ts">
import { onMounted, reactive, ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'

interface CredField { name: string; label: string; type: string; required: boolean; placeholder?: string }
interface Template {
  name: string
  description: string
  needs_credential: boolean
  credential_hint?: { type: string; fields: CredField[] }
  schedule_type: string
  schedule_spec: string
  timeout: number
  variables: Array<{ name: string; label: string; default?: string; placeholder?: string; required: boolean }>
  pipeline: any
  indicators: Array<any>
}

const props = defineProps<{ modelValue: boolean; siteId?: number }>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'apply', payload: { template: Template; vars: Record<string, string>; credentialId: number; name: string; siteId: number }): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v)
})

const list = ref<Template[]>([])
const selectedName = ref<string>('')
const detail = ref<Template | null>(null)

const sites = ref<any[]>([])
const credentials = ref<any[]>([])

const siteMode = ref<'pick' | 'create'>('pick')
const currentSiteId = ref<number>(0)
const newSite = reactive({ name: '', base_url: '' })

const credentialMode = ref<'pick' | 'create'>('pick')
const newCredName = ref<string>('')
const newCredFields = ref<Record<string, string>>({})

const form = reactive<{ name: string; credential_id: number; vars: Record<string, string> }>({
  name: '',
  credential_id: 0,
  vars: {}
})

async function reloadTemplates() {
  list.value = await api.templates.list()
  if (list.value.length && !selectedName.value) {
    selectedName.value = list.value[0].name
    await loadDetail()
  }
}

async function reloadSites() {
  sites.value = await api.sites.list()
  if (sites.value.length) {
    if (props.siteId && sites.value.find(s => s.id === props.siteId)) {
      currentSiteId.value = props.siteId
    } else if (!currentSiteId.value) {
      currentSiteId.value = sites.value[0].id
    }
    siteMode.value = 'pick'
  } else {
    currentSiteId.value = 0
    siteMode.value = 'create'
  }
}

async function reloadCredentials() {
  if (!currentSiteId.value) {
    credentials.value = []
    form.credential_id = 0
    if (detail.value?.needs_credential) credentialMode.value = 'create'
    return
  }
  credentials.value = await api.credentials.list(currentSiteId.value)
  if (credentials.value.length) {
    if (!form.credential_id || !credentials.value.find(c => c.id === form.credential_id)) {
      form.credential_id = credentials.value[0].id
    }
    credentialMode.value = 'pick'
  } else if (detail.value?.needs_credential) {
    credentialMode.value = 'create'
    form.credential_id = 0
  }
}

async function loadDetail() {
  if (!selectedName.value) return
  detail.value = await api.templates.get(selectedName.value)
  form.name = selectedName.value
  form.vars = {}
  for (const v of detail.value!.variables) {
    form.vars[v.name] = v.default ?? ''
  }
  syncBaseURL()
  resetNewCred()
  if (!credentials.value.length && detail.value!.needs_credential) {
    credentialMode.value = 'create'
  }
}

function selectedSite() {
  return sites.value.find(s => s.id === currentSiteId.value)
}

function syncBaseURL() {
  if (!detail.value) return
  if (!detail.value.variables.find(v => v.name === 'base_url')) return
  const baseURL = siteMode.value === 'create' ? newSite.base_url : (selectedSite()?.base_url || '')
  if (baseURL) form.vars.base_url = baseURL
}

watch(currentSiteId, () => {
  reloadCredentials()
  syncBaseURL()
})

watch(siteMode, () => {
  syncBaseURL()
  if (siteMode.value === 'create') {
    credentials.value = []
    form.credential_id = 0
    if (detail.value?.needs_credential) credentialMode.value = 'create'
  }
})

watch(() => newSite.base_url, syncBaseURL)
watch(selectedName, loadDetail)
watch(visible, async (v) => {
  if (v) {
    await Promise.all([reloadTemplates(), reloadSites()])
    await reloadCredentials()
    syncBaseURL()
  }
})

function resetNewCred() {
  newCredName.value = `${detail.value?.name || ''}-${Date.now().toString().slice(-6)}`
  const fields: Record<string, string> = {}
  for (const f of detail.value?.credential_hint?.fields || []) {
    fields[f.name] = ''
  }
  newCredFields.value = fields
}

async function ensureSite(): Promise<number | null> {
  if (siteMode.value === 'pick') {
    if (!currentSiteId.value) {
      ElMessage.warning('请选择 Site 或切换到"新建"')
      return null
    }
    return currentSiteId.value
  }
  if (!newSite.name.trim()) {
    ElMessage.warning('请填写 Site 名称')
    return null
  }
  if (!newSite.base_url.trim()) {
    ElMessage.warning('请填写 Site Base URL')
    return null
  }
  try {
    const baseURL = newSite.base_url.trim().replace(/\/+$/, '')
    const created = await api.sites.create({ name: newSite.name.trim(), base_url: baseURL })
    ElMessage.success(`新 Site #${created.id} 已创建`)
    sites.value = await api.sites.list()
    currentSiteId.value = created.id
    return created.id
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'Site 创建失败')
    return null
  }
}

async function ensureCredential(siteId: number): Promise<number | null> {
  if (!detail.value?.needs_credential) return 0
  if (credentialMode.value === 'pick') {
    if (!form.credential_id) {
      ElMessage.warning('请选择凭证或切换到"新建"')
      return null
    }
    return form.credential_id
  }
  const hint = detail.value.credential_hint
  if (!hint) return null
  for (const f of hint.fields) {
    if (f.required && !newCredFields.value[f.name]) {
      ElMessage.warning(`请填写 ${f.label || f.name}`)
      return null
    }
  }
  if (!newCredName.value.trim()) {
    ElMessage.warning('请填写凭证名称')
    return null
  }
  try {
    const cred = await api.credentials.create({
      site_id: siteId,
      name: newCredName.value.trim(),
      type: hint.type,
      payload: { ...newCredFields.value }
    })
    ElMessage.success(`新凭证 #${cred.id} 已创建`)
    return cred.id
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || '凭证创建失败')
    return null
  }
}

async function apply() {
  if (!detail.value) return
  for (const v of detail.value.variables) {
    if (v.required && !form.vars[v.name]) {
      ElMessage.warning(`请填写 ${v.label || v.name}`)
      return
    }
  }
  const siteId = await ensureSite()
  if (!siteId) return

  const credId = await ensureCredential(siteId)
  if (credId === null) return

  emit('apply', {
    template: detail.value,
    vars: form.vars,
    credentialId: credId,
    name: form.name,
    siteId
  })
  visible.value = false
}

onMounted(() => {})
</script>

<template>
  <el-dialog v-model="visible" title="From Template" width="720px">
    <div v-if="list.length">
      <el-alert type="info" :closable="false" show-icon style="margin-bottom: 12px;">
        本向导会根据模板自动创建 / 复用 Site、Credential，并填好整条 Pipeline。
      </el-alert>

      <el-form label-width="120px">
        <el-form-item label="Template">
          <el-select v-model="selectedName" filterable style="width: 100%">
            <el-option v-for="t in list" :key="t.name" :label="t.name" :value="t.name">
              <span>{{ t.name }}</span>
              <span style="margin-left: 12px; color: #999">{{ t.description }}</span>
            </el-option>
          </el-select>
        </el-form-item>

        <el-form-item label="Collector Name">
          <el-input v-model="form.name" />
        </el-form-item>

        <el-divider content-position="left">Site</el-divider>

        <el-form-item label="Source">
          <el-radio-group v-model="siteMode">
            <el-radio-button label="pick" :disabled="!sites.length">Use Existing</el-radio-button>
            <el-radio-button label="create">Create New</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item v-if="siteMode === 'pick'" label="Site">
          <el-select v-model="currentSiteId" filterable placeholder="Search and select site" style="width: 100%">
            <el-option
              v-for="s in sites"
              :key="s.id"
              :label="`${s.name} (${s.base_url})`"
              :value="s.id"
            />
          </el-select>
        </el-form-item>

        <template v-else>
          <el-form-item label="Name" required>
            <el-input v-model="newSite.name" placeholder="huamo" />
          </el-form-item>
          <el-form-item label="Base URL" required>
            <el-input v-model="newSite.base_url" placeholder="http://example.com:port" />
          </el-form-item>
        </template>

        <el-divider content-position="left">Variables</el-divider>

        <template v-for="v in detail?.variables || []" :key="v.name">
          <el-form-item :label="v.label || v.name" :required="v.required">
            <el-input v-model="form.vars[v.name]" :placeholder="v.placeholder" />
          </el-form-item>
        </template>

        <template v-if="detail?.needs_credential">
          <el-divider content-position="left">Credential</el-divider>

          <el-form-item label="Source">
            <el-radio-group v-model="credentialMode">
              <el-radio-button label="pick" :disabled="!credentials.length">Use Existing</el-radio-button>
              <el-radio-button label="create">Create New</el-radio-button>
            </el-radio-group>
          </el-form-item>

          <el-form-item v-if="credentialMode === 'pick'" label="Credential">
            <el-select
              v-model="form.credential_id"
              filterable
              placeholder="Search and select credential"
              style="width: 100%"
            >
              <el-option
                v-for="c in credentials"
                :key="c.id"
                :label="`${c.name} (${c.type})`"
                :value="c.id"
              />
            </el-select>
          </el-form-item>

          <template v-else>
            <el-form-item label="Name">
              <el-input v-model="newCredName" placeholder="credential name" />
            </el-form-item>
            <el-form-item label="Type">
              <el-input :model-value="detail.credential_hint?.type || ''" disabled />
            </el-form-item>
            <el-form-item
              v-for="f in detail.credential_hint?.fields || []"
              :key="f.name"
              :label="f.label || f.name"
              :required="f.required"
            >
              <el-input
                v-model="newCredFields[f.name]"
                :type="f.type === 'password' ? 'password' : 'text'"
                :show-password="f.type === 'password'"
                :placeholder="f.placeholder"
              />
            </el-form-item>
          </template>
        </template>

        <el-alert type="info" :closable="false" show-icon>
          Save 后会自动创建对应 Indicator（{{ detail?.indicators?.map(i => i.key).join(', ') || '无' }}），调度类型 = {{ detail?.schedule_type }}，间隔/cron = {{ detail?.schedule_spec || '—' }}。
        </el-alert>
      </el-form>
    </div>
    <el-empty v-else description="暂无模板" />

    <template #footer>
      <el-button @click="visible = false">Cancel</el-button>
      <el-button type="primary" :disabled="!detail" @click="apply">Apply</el-button>
    </template>
  </el-dialog>
</template>
