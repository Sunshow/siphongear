<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import ResponsiveTable, { type RTColumn } from '@/components/ResponsiveTable.vue'

interface NotifyType {
  type: string
  description: string
  schema: Record<string, any>
}

interface ChannelRow {
  id: number
  name: string
  type: string
  enabled: boolean
  notes: string
  created_at?: string
  updated_at?: string
}

interface LogRow {
  id: number
  channel_id: number
  rule_id: number
  collector_id: number
  indicator_id: number
  severity: string
  title: string
  snippet: string
  status: string
  error: string
  created_at: string
}

const types = ref<NotifyType[]>([])
const rows = ref<ChannelRow[]>([])
const logs = ref<LogRow[]>([])
const tab = ref<'channels' | 'logs'>('channels')
const dialog = ref(false)
const submitting = ref(false)

const form = reactive({
  id: 0,
  name: '',
  type: '',
  enabled: true,
  notes: '',
  payload: {} as Record<string, any>
})

const columns: RTColumn[] = [
  { key: 'id', label: 'ID', width: 80, hideOnMobile: true },
  { key: 'name', label: 'Name', primary: true },
  { key: 'type', label: 'Type', width: 140, slot: 'type' },
  { key: 'enabled', label: 'On', width: 80, slot: 'enabled' },
  { key: 'notes', label: 'Notes' },
  { key: 'actions', label: 'Actions', slot: 'actions', width: 220 }
]

const logColumns: RTColumn[] = [
  { key: 'id', label: 'ID', width: 80, hideOnMobile: true },
  { key: 'created_at', label: 'Time', width: 200, slot: 'created_at' },
  { key: 'channel_id', label: 'Channel', width: 120, slot: 'channel' },
  { key: 'severity', label: 'Severity', width: 110, slot: 'severity' },
  { key: 'title', label: 'Title', primary: true },
  { key: 'status', label: 'Status', width: 110, slot: 'status' },
  { key: 'error', label: 'Error', slot: 'error' }
]

const currentSchema = computed(() => {
  const t = types.value.find(t => t.type === form.type)
  return t ? t.schema : {}
})

const schemaEntries = computed<[string, any][]>(() => Object.entries(currentSchema.value || {}))

const channelMap = computed(() => {
  const m: Record<number, ChannelRow> = {}
  for (const r of rows.value) m[r.id] = r
  return m
})

watch(() => form.type, (t, prev) => {
  if (form.id && t === prev) return
  if (t === prev) return
  // Reset payload to defaults when picking a different type.
  const next: Record<string, any> = {}
  const sch = (types.value.find(x => x.type === t)?.schema) || {}
  for (const [key, s] of Object.entries(sch)) {
    if ((s as any).default !== undefined) next[key] = (s as any).default
  }
  form.payload = next
})

async function reloadChannels() {
  rows.value = await api.notify.channels.list()
}

async function reloadLogs() {
  logs.value = await api.notify.logs({ limit: 100 })
}

async function reloadTypes() {
  types.value = await api.notify.types()
}

function openCreate() {
  const defType = types.value[0]?.type || ''
  Object.assign(form, {
    id: 0,
    name: '',
    type: defType,
    enabled: true,
    notes: '',
    payload: {} as Record<string, any>
  })
  // Apply defaults from schema once.
  const sch = types.value.find(x => x.type === defType)?.schema || {}
  const next: Record<string, any> = {}
  for (const [key, s] of Object.entries(sch)) {
    if ((s as any).default !== undefined) next[key] = (s as any).default
  }
  form.payload = next
  dialog.value = true
}

async function openEdit(row: ChannelRow) {
  Object.assign(form, {
    id: row.id,
    name: row.name,
    type: row.type,
    enabled: row.enabled,
    notes: row.notes || '',
    payload: {} as Record<string, any>
  })
  dialog.value = true
}

function setPayload(key: string, v: any) {
  form.payload = { ...form.payload, [key]: v }
}

async function save() {
  if (!form.name.trim()) {
    ElMessage.error('name is required')
    return
  }
  if (!form.type) {
    ElMessage.error('type is required')
    return
  }
  submitting.value = true
  try {
    const body: any = {
      name: form.name.trim(),
      type: form.type,
      enabled: form.enabled,
      notes: form.notes,
      payload: form.payload
    }
    if (form.id) {
      // For updates, only send payload if user filled at least one field.
      const hasAny = Object.values(form.payload).some(v => v !== '' && v !== undefined && v !== null)
      if (!hasAny) delete body.payload
      await api.notify.channels.update(form.id, body)
    } else {
      await api.notify.channels.create(body)
    }
    dialog.value = false
    await reloadChannels()
    ElMessage.success('saved')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'save failed')
  } finally {
    submitting.value = false
  }
}

async function remove(row: ChannelRow) {
  await ElMessageBox.confirm(`Delete notification channel "${row.name}"?`, 'Confirm')
  await api.notify.channels.remove(row.id)
  await reloadChannels()
}

async function toggleEnabled(row: ChannelRow) {
  try {
    await api.notify.channels.update(row.id, {
      name: row.name,
      type: row.type,
      enabled: !row.enabled,
      notes: row.notes
    })
    await reloadChannels()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'toggle failed')
  }
}

async function testSend(row: ChannelRow) {
  try {
    await api.notify.channels.test(row.id, {
      title: `[TEST] SiphonGear · ${row.name}`,
      body: 'This is a test notification triggered from SiphonGear.'
    })
    ElMessage.success('test sent')
    if (tab.value === 'logs') await reloadLogs()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'test failed')
  }
}

watch(tab, async t => {
  if (t === 'logs') await reloadLogs()
})

onMounted(async () => {
  await reloadTypes()
  await reloadChannels()
})

function fmtTime(t: string): string {
  if (!t) return '—'
  try { return new Date(t).toLocaleString() } catch { return t }
}
</script>

<template>
  <div>
    <div class="page-bar">
      <div>
        <h2>Notifications</h2>
        <div class="subtitle">Configure notification channels and review delivery logs.</div>
      </div>
      <div class="page-bar-actions">
        <el-button v-if="tab === 'channels'" type="primary" @click="openCreate">New Channel</el-button>
        <el-button v-if="tab === 'logs'" @click="reloadLogs">Refresh</el-button>
      </div>
    </div>

    <el-tabs v-model="tab">
      <el-tab-pane label="Channels" name="channels">
        <ResponsiveTable :rows="rows" :columns="columns" row-key="id">
          <template #type="{ row }">
            <el-tag size="small" effect="plain">{{ row.type }}</el-tag>
          </template>
          <template #enabled="{ row }">
            <el-switch :model-value="row.enabled" @click.stop="toggleEnabled(row)" />
          </template>
          <template #actions="{ row }">
            <el-button link @click="testSend(row)">Test</el-button>
            <el-button link @click="openEdit(row)">Edit</el-button>
            <el-button link type="danger" @click="remove(row)">Delete</el-button>
          </template>
        </ResponsiveTable>
      </el-tab-pane>
      <el-tab-pane label="Logs" name="logs">
        <ResponsiveTable :rows="logs" :columns="logColumns" row-key="id">
          <template #created_at="{ row }">{{ fmtTime(row.created_at) }}</template>
          <template #channel="{ row }">
            <span>{{ channelMap[row.channel_id]?.name || `#${row.channel_id}` }}</span>
          </template>
          <template #severity="{ row }">
            <el-tag
              size="small"
              :type="row.severity === 'recovery' ? 'success' : 'warning'"
              effect="plain"
            >{{ row.severity || '—' }}</el-tag>
          </template>
          <template #status="{ row }">
            <el-tag
              size="small"
              :type="row.status === 'success' ? 'success' : 'danger'"
              effect="plain"
            >{{ row.status }}</el-tag>
          </template>
          <template #error="{ row }">
            <span v-if="row.error" class="log-error" :title="row.error">{{ row.error }}</span>
            <span v-else class="log-muted">—</span>
          </template>
        </ResponsiveTable>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="dialog" :title="form.id ? 'Edit Channel' : 'New Channel'" width="640px">
      <el-form label-width="120px">
        <el-form-item label="Name">
          <el-input v-model="form.name" placeholder="e.g., my serverchan" />
        </el-form-item>
        <el-form-item label="Type">
          <el-select v-model="form.type" :disabled="!!form.id" style="width: 240px">
            <el-option
              v-for="t in types"
              :key="t.type"
              :value="t.type"
              :label="t.type"
            />
          </el-select>
          <div v-if="currentSchema && form.type" class="form-hint" style="margin-left: 8px">
            {{ types.find(t => t.type === form.type)?.description }}
          </div>
        </el-form-item>
        <el-form-item label="Enabled">
          <el-switch v-model="form.enabled" />
        </el-form-item>

        <el-divider content-position="left">Channel parameters</el-divider>

        <el-form-item
          v-for="[key, s] in schemaEntries"
          :key="key"
          :label="(s as any).label || key"
          :required="!!(s as any).required"
        >
          <div class="field-wrap">
            <template v-if="(s as any).type === 'string' && (s as any).secret">
              <el-input
                :model-value="form.payload[key] ?? ''"
                type="password"
                show-password
                :placeholder="form.id ? 'Leave blank to keep current' : ((s as any).placeholder || '')"
                @update:model-value="(v: string) => setPayload(key, v)"
              />
            </template>
            <template v-else-if="(s as any).type === 'string'">
              <el-input
                :model-value="form.payload[key] ?? ''"
                :placeholder="(s as any).placeholder || ''"
                @update:model-value="(v: string) => setPayload(key, v)"
              />
            </template>
            <template v-else-if="(s as any).type === 'number'">
              <el-input-number
                :model-value="Number(form.payload[key] ?? 0)"
                @update:model-value="(v: any) => setPayload(key, v)"
              />
            </template>
            <template v-else-if="(s as any).type === 'boolean'">
              <el-switch
                :model-value="!!form.payload[key]"
                @update:model-value="(v: any) => setPayload(key, v)"
              />
            </template>
            <template v-else>
              <el-input
                :model-value="form.payload[key] ?? ''"
                :placeholder="(s as any).placeholder || ''"
                @update:model-value="(v: string) => setPayload(key, v)"
              />
            </template>
            <div v-if="(s as any).description" class="field-hint">{{ (s as any).description }}</div>
          </div>
        </el-form-item>

        <el-form-item label="Notes">
          <el-input v-model="form.notes" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">Cancel</el-button>
        <el-button type="primary" :loading="submitting" @click="save">Save</el-button>
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
.form-hint {
  font-size: 12px;
  color: var(--sg-text-secondary);
}
.log-error {
  color: var(--el-color-danger);
  display: inline-block;
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.log-muted {
  color: var(--sg-text-muted);
}
.field-wrap {
  width: 100%;
}
.field-hint {
  font-size: 12px;
  color: var(--sg-text-secondary);
  line-height: 1.5;
  margin-top: 4px;
}
</style>
