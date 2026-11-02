<script setup lang="ts">
import { onMounted, reactive, ref, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import ResponsiveTable, { type RTColumn } from '@/components/ResponsiveTable.vue'

interface CredField { name: string; label: string; type: string; required: boolean; placeholder?: string }

// Fields shown as first-class inputs per credential type. Templates declare
// their own credential_hint fields (e.g. new-api's access_token, sub2api's
// email); anything not listed here is preserved through the "Extra fields"
// JSON box so editing a credential never silently drops template fields.
const CRED_FIELDS: Record<string, CredField[]> = {
  password: [
    { name: 'email', label: 'Email', type: 'text', required: false, placeholder: 'user@example.com (sub2api / udealproxy)' },
    { name: 'username', label: 'Username', type: 'text', required: false, placeholder: 'new-api 等模板使用；与 Email 二选一' },
    { name: 'password', label: 'Password', type: 'password', required: true },
  ],
  token: [
    { name: 'api_key', label: 'API Key / Token', type: 'password', required: false, placeholder: 'sk-...' },
    { name: 'access_token', label: 'Access Token', type: 'password', required: false, placeholder: 'nap_... (new-api: 个人设置 → 安全设置 → 访问令牌)' },
    { name: 'user_id', label: 'User ID', type: 'text', required: false, placeholder: '如 69；仅旧版 new-api 需要' },
  ],
  cookie: [
    { name: 'cookie', label: 'Cookie', type: 'password', required: true, placeholder: 'session=...; key=...' },
  ],
  custom: [],
}

const rows = ref<any[]>([])
const sites = ref<any[]>([])
const dialog = ref(false)
const form = reactive({ id: 0, site_id: 0, name: '', type: 'token' })
const fieldsData = reactive<Record<string, string>>({})
const extraJson = ref<string>('{}')
const customPayload = ref<string>('{}')

const fieldsForType = computed(() => CRED_FIELDS[form.type] || [])
const isCustom = computed(() => form.type === 'custom')

// Template credentials may carry fields the form does not know about, so they
// are surfaced (and preserved) through the Extra fields JSON box.
const knownFieldNames = computed(() => new Set(fieldsForType.value.map(f => f.name)))

function resetFields() {
  for (const k of Object.keys(fieldsData)) delete fieldsData[k]
  for (const f of fieldsForType.value) fieldsData[f.name] = ''
  extraJson.value = '{}'
  customPayload.value = '{}'
}

watch(() => form.type, resetFields)

async function reload() {
  [rows.value, sites.value] = await Promise.all([api.credentials.list(), api.sites.list()])
}

const siteName = computed(() => (id: number) => sites.value.find(s => s.id === id)?.name || `#${id}`)

const columns: RTColumn[] = [
  { key: 'id', label: 'ID', width: 80, hideOnMobile: true },
  { key: 'name', label: 'Name', primary: true },
  { key: 'site', label: 'Site', slot: 'site', width: 180 },
  { key: 'type', label: 'Type', width: 120 },
  { key: 'updated', label: 'Updated', slot: 'updated', width: 200 },
  { key: 'actions', label: 'Actions', slot: 'actions', width: 220 }
]

function openCreate() {
  form.id = 0
  form.site_id = sites.value[0]?.id || 0
  form.name = ''
  form.type = 'token'
  resetFields()
  dialog.value = true
}

// normalizeSecret trims pasted values and removes a leading "Bearer " so a
// token copied from docs/curl works as-is.
function normalizeSecret(name: string, value: string): string {
  let out = (value || '').trim()
  if (name === 'api_key' || name === 'access_token' || name === 'token' || name === 'cookie') {
    out = out.replace(/^Bearer\s+/i, '').trim()
  }
  return out
}

function hasValue(payload: Record<string, any>, key: string): boolean {
  const v = payload[key]
  if (typeof v === 'number') return true
  return typeof v === 'string' && v.trim() !== ''
}

function validatePayload(type: string, payload: Record<string, any>): string | null {
  if (type === 'password') {
    if (!hasValue(payload, 'password')) return 'Password is required'
    if (!hasValue(payload, 'email') && !hasValue(payload, 'username')) {
      return 'Fill in either Email or Username (templates differ in which field they read)'
    }
  }
  if (type === 'token') {
    if (!hasValue(payload, 'api_key') && !hasValue(payload, 'access_token') && !hasValue(payload, 'token')) {
      return 'Fill in an API Key or an Access Token'
    }
  }
  if (type === 'cookie' && !hasValue(payload, 'cookie')) return 'Cookie is required'
  return null
}

function parseExtras(): Record<string, any> | null {
  const text = extraJson.value.trim()
  if (!text || text === '{}') return {}
  let parsed: any
  try {
    parsed = JSON.parse(text)
  } catch (e: any) {
    ElMessage.error('Extra fields must be valid JSON: ' + e.message)
    return null
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    ElMessage.error('Extra fields must be a JSON object')
    return null
  }
  return parsed
}

function buildPayload(): any {
  if (isCustom.value) {
    try { return JSON.parse(customPayload.value || '{}') } catch (e: any) {
      ElMessage.error('payload must be valid JSON: ' + e.message)
      return null
    }
  }
  const extras = parseExtras()
  if (extras === null) return null
  const payload: Record<string, any> = { ...extras }
  for (const f of fieldsForType.value) {
    const raw = fieldsData[f.name]
    if (raw !== undefined && raw !== null && String(raw).trim() !== '') {
      payload[f.name] = normalizeSecret(f.name, String(raw))
    } else {
      delete payload[f.name]
    }
  }
  const err = validatePayload(form.type, payload)
  if (err) {
    ElMessage.error(err)
    return null
  }
  return payload
}

async function save() {
  const payload = buildPayload()
  if (payload === null) return
  const body = { site_id: form.site_id, name: form.name, type: form.type, payload }
  if (form.id) await api.credentials.update(form.id, body)
  else await api.credentials.create(body)
  dialog.value = false
  await reload()
  ElMessage.success('saved')
}

async function openEdit(row: any) {
  form.id = row.id
  form.site_id = row.site_id
  form.name = row.name
  form.type = row.type
  resetFields()
  dialog.value = true
  try {
    const full = await api.credentials.get(row.id)
    const payload = full?.payload || {}
    if (isCustom.value) {
      customPayload.value = JSON.stringify(payload, null, 2)
      return
    }
    const extras: Record<string, any> = {}
    for (const [k, v] of Object.entries(payload)) {
      if (knownFieldNames.value.has(k)) {
        fieldsData[k] = v === null || v === undefined ? '' : String(v)
      } else {
        extras[k] = v
      }
    }
    extraJson.value = Object.keys(extras).length ? JSON.stringify(extras, null, 2) : '{}'
  } catch { /* ignore */ }
}

async function remove(row: any) {
  await ElMessageBox.confirm(`Delete ${row.name}?`, 'Confirm')
  await api.credentials.remove(row.id)
  await reload()
}

onMounted(reload)
</script>

<template>
  <div>
    <div class="page-bar">
      <h2>Credentials</h2>
      <div class="page-bar-actions">
        <el-button type="primary" @click="openCreate">New Credential</el-button>
      </div>
    </div>
    <ResponsiveTable :rows="rows" :columns="columns" row-key="id">
      <template #site="{ row }">{{ siteName(row.site_id) }}</template>
      <template #updated="{ row }">{{ new Date(row.updated_at).toLocaleString() }}</template>
      <template #actions="{ row }">
        <el-button link @click="openEdit(row)">Edit / Replace</el-button>
        <el-button link type="danger" @click="remove(row)">Delete</el-button>
      </template>
    </ResponsiveTable>

    <el-dialog v-model="dialog" :title="form.id ? 'Edit Credential' : 'New Credential'" width="640px">
      <el-form label-width="150px" style="max-width: 520px">
        <el-form-item label="Site">
          <el-select v-model="form.site_id">
            <el-option v-for="s in sites" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="Name"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="Type">
          <el-select v-model="form.type">
            <el-option label="password" value="password" />
            <el-option label="token" value="token" />
            <el-option label="cookie" value="cookie" />
            <el-option label="custom" value="custom" />
          </el-select>
        </el-form-item>

        <el-divider content-position="left">Fields</el-divider>

        <template v-if="isCustom">
          <el-form-item label="Payload (JSON)">
            <el-input v-model="customPayload" type="textarea" :rows="8" placeholder='{"api_key": "..."}' />
          </el-form-item>
        </template>
        <template v-else>
          <el-form-item
            v-for="f in fieldsForType"
            :key="f.name"
            :label="f.label"
            :required="f.required"
          >
            <el-input
              v-model="fieldsData[f.name]"
              :type="f.type === 'password' ? 'password' : 'text'"
              :show-password="f.type === 'password'"
              :placeholder="f.placeholder"
            />
          </el-form-item>
          <el-form-item label="Extra fields (JSON)">
            <el-input
              v-model="extraJson"
              type="textarea"
              :rows="3"
              placeholder='{"access_token": "nap_...", "token": "..."}'
            />
            <el-text type="info" size="small">
              Template-specific fields the form above does not know about. They are preserved on edit and merged into the payload.
            </el-text>
          </el-form-item>
        </template>
        <el-alert v-if="form.id" type="info" show-icon :closable="false">
          Current values are loaded for editing; saving replaces the whole payload.
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">Cancel</el-button>
        <el-button type="primary" @click="save">Save</el-button>
      </template>
    </el-dialog>
  </div>
</template>
