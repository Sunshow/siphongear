<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import ResponsiveTable, { type RTColumn } from '@/components/ResponsiveTable.vue'

interface ApiKeyRow {
  id: number
  name: string
  prefix: string
  enabled: boolean
  last_used_at: string | null
  notes: string
  created_at: string
  updated_at: string
}

const rows = ref<ApiKeyRow[]>([])
const loading = ref(false)

const dialog = ref(false)
const form = reactive({ id: 0, name: '', enabled: true, notes: '' })

const plaintextDialog = ref(false)
const plaintextValue = ref('')
const plaintextTitle = ref('API Key Created')

const columns: RTColumn[] = [
  { key: 'id', label: 'ID', width: 70, hideOnMobile: true },
  { key: 'name', label: 'Name', primary: true },
  { key: 'prefix', label: 'Prefix', slot: 'prefix', width: 180 },
  { key: 'enabled', label: 'Enabled', slot: 'enabled', width: 110 },
  { key: 'last_used_at', label: 'Last Used', slot: 'last_used', width: 200 },
  { key: 'actions', label: 'Actions', slot: 'actions', width: 260 }
]

async function reload() {
  loading.value = true
  try {
    rows.value = await api.apiKeys.list()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'load failed')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  Object.assign(form, { id: 0, name: '', enabled: true, notes: '' })
  dialog.value = true
}

function openEdit(row: ApiKeyRow) {
  Object.assign(form, { id: row.id, name: row.name, enabled: row.enabled, notes: row.notes || '' })
  dialog.value = true
}

async function save() {
  const name = form.name.trim()
  if (!name) {
    ElMessage.error('name is required')
    return
  }
  try {
    if (form.id) {
      await api.apiKeys.update(form.id, { name, enabled: form.enabled, notes: form.notes })
      ElMessage.success('saved')
      dialog.value = false
      await reload()
    } else {
      const res = await api.apiKeys.create({ name, notes: form.notes })
      dialog.value = false
      await reload()
      showPlaintext(res.plaintext, 'API Key Created')
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'save failed')
  }
}

async function remove(row: ApiKeyRow) {
  try {
    await ElMessageBox.confirm(`Delete API key "${row.name}"? This cannot be undone.`, 'Confirm', {
      type: 'warning'
    })
  } catch {
    return
  }
  try {
    await api.apiKeys.remove(row.id)
    ElMessage.success('deleted')
    await reload()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'delete failed')
  }
}

async function rotate(row: ApiKeyRow) {
  try {
    await ElMessageBox.confirm(
      `Rotate API key "${row.name}"? The current secret will stop working immediately.`,
      'Confirm Rotate',
      { type: 'warning' }
    )
  } catch {
    return
  }
  try {
    const res = await api.apiKeys.rotate(row.id)
    await reload()
    showPlaintext(res.plaintext, 'API Key Rotated')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'rotate failed')
  }
}

function showPlaintext(plaintext: string, title: string) {
  plaintextValue.value = plaintext
  plaintextTitle.value = title
  plaintextDialog.value = true
}

async function copyPlaintext() {
  try {
    await navigator.clipboard.writeText(plaintextValue.value)
    ElMessage.success('copied to clipboard')
  } catch {
    ElMessage.warning('copy failed; please select and copy manually')
  }
}

function closePlaintext() {
  plaintextDialog.value = false
  plaintextValue.value = ''
}

function formatTs(s: string | null): string {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  return d.toLocaleString()
}

function maskedPrefix(p: string): string {
  if (!p) return ''
  return `sg_${p}_…`
}

onMounted(reload)
</script>

<template>
  <div>
    <div class="page-bar">
      <div>
        <h2>API Keys</h2>
        <div class="subtitle">
          Used to authenticate calls to <code>/api/public/*</code> endpoints.
        </div>
      </div>
      <div class="page-bar-actions">
        <el-button :loading="loading" @click="reload">Reload</el-button>
        <el-button type="primary" @click="openCreate">New API Key</el-button>
      </div>
    </div>

    <ResponsiveTable :rows="rows" :columns="columns" :loading="loading" row-key="id">
      <template #prefix="{ row }">
        <code class="prefix-cell">{{ maskedPrefix(row.prefix) }}</code>
      </template>
      <template #enabled="{ row }">
        <el-tag size="small" :type="row.enabled ? 'success' : 'info'" effect="plain">
          {{ row.enabled ? 'enabled' : 'disabled' }}
        </el-tag>
      </template>
      <template #last_used="{ row }">
        <span :class="{ muted: !row.last_used_at }">{{ formatTs(row.last_used_at) }}</span>
      </template>
      <template #actions="{ row }">
        <el-button link @click="openEdit(row)">Edit</el-button>
        <el-button link type="warning" @click="rotate(row)">Rotate</el-button>
        <el-button link type="danger" @click="remove(row)">Delete</el-button>
      </template>
    </ResponsiveTable>

    <el-dialog v-model="dialog" :title="form.id ? 'Edit API Key' : 'New API Key'" width="480px">
      <el-form label-width="100px">
        <el-form-item label="Name">
          <el-input v-model="form.name" placeholder="e.g. monitoring-prod" />
        </el-form-item>
        <el-form-item v-if="form.id" label="Enabled">
          <el-switch v-model="form.enabled" />
        </el-form-item>
        <el-form-item label="Notes">
          <el-input v-model="form.notes" type="textarea" :rows="3" placeholder="optional" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">Cancel</el-button>
        <el-button type="primary" @click="save">Save</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="plaintextDialog"
      :title="plaintextTitle"
      width="540px"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      :show-close="false"
    >
      <el-alert
        type="warning"
        :closable="false"
        title="Copy and store this key now"
        description="This is the only time the full key is shown. After closing this dialog, only the prefix is recoverable."
        show-icon
        style="margin-bottom: 12px"
      />
      <div class="plaintext-box">
        <code>{{ plaintextValue }}</code>
      </div>
      <template #footer>
        <el-button type="primary" @click="copyPlaintext">Copy</el-button>
        <el-button @click="closePlaintext">I have saved it</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.subtitle {
  color: var(--sg-text-secondary);
  font-size: 13px;
  margin-top: 4px;
}
.subtitle code {
  background: var(--sg-aside-hover-bg);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: ui-monospace, "SF Mono", Menlo, monospace;
}
.prefix-cell {
  font-family: ui-monospace, "SF Mono", Menlo, monospace;
  background: var(--sg-aside-hover-bg);
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  color: var(--sg-text-primary);
}
.muted {
  color: var(--sg-text-muted);
}
.plaintext-box {
  background: var(--sg-aside-hover-bg);
  border: 1px solid var(--sg-border-soft);
  border-radius: 6px;
  padding: 12px 14px;
  font-family: ui-monospace, "SF Mono", Menlo, monospace;
  font-size: 13px;
  color: var(--sg-text-primary);
  word-break: break-all;
  user-select: all;
}
</style>
