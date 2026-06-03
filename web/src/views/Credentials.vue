<script setup lang="ts">
import { onMounted, reactive, ref, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import ResponsiveTable, { type RTColumn } from '@/components/ResponsiveTable.vue'

interface CredField { name: string; label: string; type: string; required: boolean; placeholder?: string }

const CRED_FIELDS: Record<string, CredField[]> = {
  password: [
    { name: 'email', label: 'Email / Username', type: 'text', required: true, placeholder: 'user@example.com' },
    { name: 'password', label: 'Password', type: 'password', required: true },
  ],
  token: [
    { name: 'api_key', label: 'API Key / Token', type: 'password', required: true, placeholder: 'sk-...' },
    { name: 'user_id', label: 'User ID', type: 'text', required: false, placeholder: 'optional' },
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
const customPayload = ref<string>('{}')

const fieldsForType = computed(() => CRED_FIELDS[form.type] || [])
const isCustom = computed(() => form.type === 'custom')

function resetFields() {
  for (const k of Object.keys(fieldsData)) delete fieldsData[k]
  for (const f of fieldsForType.value) fieldsData[f.name] = ''
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

function buildPayload(): any {
  if (isCustom.value) {
    try { return JSON.parse(customPayload.value || '{}') } catch (e: any) {
      ElMessage.error('payload must be valid JSON: ' + e.message)
      return null
    }
  }
  const payload: Record<string, string> = {}
  for (const f of fieldsForType.value) {
    if (fieldsData[f.name]) payload[f.name] = fieldsData[f.name]
    else if (f.required) payload[f.name] = ''
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
    if (full.payload) {
      if (isCustom.value) {
        customPayload.value = JSON.stringify(full.payload, null, 2)
      } else {
        for (const f of fieldsForType.value) {
          if (full.payload[f.name] !== undefined) {
            fieldsData[f.name] = String(full.payload[f.name])
          }
        }
      }
    }
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
        </template>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">Cancel</el-button>
        <el-button type="primary" @click="save">Save</el-button>
      </template>
    </el-dialog>
  </div>
</template>
