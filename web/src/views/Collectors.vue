<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import ResponsiveTable, { type RTColumn } from '@/components/ResponsiveTable.vue'
import SaveAsTemplateDialog from '@/components/SaveAsTemplateDialog.vue'

const router = useRouter()
const rows = ref<any[]>([])
const sites = ref<any[]>([])
const loading = ref(false)

const saveTplVisible = ref(false)
const saveTplCollector = ref<any>(null)

async function reload() {
  loading.value = true
  try {
    [rows.value, sites.value] = await Promise.all([api.collectors.list(), api.sites.list()])
  } finally { loading.value = false }
}

const siteName = computed(() => (id: number) => sites.value.find(s => s.id === id)?.name || `#${id}`)

const columns: RTColumn[] = [
  { key: 'id', label: 'ID', width: 70, hideOnMobile: true },
  { key: 'name', label: 'Name', primary: true },
  { key: 'site', label: 'Site', slot: 'site', width: 180 },
  { key: 'schedule', label: 'Schedule', slot: 'schedule', width: 220 },
  { key: 'status', label: 'Status', slot: 'status', width: 140 },
  { key: 'enabled', label: 'Enabled', slot: 'enabled', width: 110 },
  { key: 'actions', label: 'Actions', slot: 'actions', width: 320 }
]

async function toggle(row: any) {
  await api.collectors.update(row.id, { ...row, enabled: !row.enabled })
  await reload()
}

async function trigger(row: any) {
  try {
    const res = await api.collectors.run(row.id, {})
    ElMessage.success(`run ${res.run.id}: ${res.run.status}`)
    await reload()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'run failed')
  }
}

async function remove(row: any) {
  await ElMessageBox.confirm(`Delete collector "${row.name}"?`, 'Confirm')
  await api.collectors.remove(row.id)
  await reload()
}

function gotoNew() { router.push({ name: 'collector-new' }) }
function gotoEdit(row: any) { router.push({ name: 'collector-edit', params: { id: row.id } }) }
function gotoRuns(row: any) { router.push({ name: 'collector-runs', params: { id: row.id } }) }

function openSaveAsTemplate(row: any) {
  saveTplCollector.value = row
  saveTplVisible.value = true
}

function onTemplateSaved(name: string) {
  ElMessage.success(`template "${name}" created`)
  router.push({ name: 'templates' })
}

onMounted(reload)
</script>

<template>
  <div>
    <div class="page-bar">
      <h2>Collectors</h2>
      <div class="page-bar-actions">
        <el-button type="primary" @click="gotoNew">New Collector</el-button>
      </div>
    </div>
    <ResponsiveTable :rows="rows" :columns="columns" :loading="loading" row-key="id">
      <template #site="{ row }">{{ siteName(row.site_id) }}</template>
      <template #schedule="{ row }">
        <el-tag size="small">{{ row.schedule_type || 'none' }}</el-tag>
        <span v-if="row.schedule_spec" style="margin-left:6px;">{{ row.schedule_spec }}</span>
      </template>
      <template #status="{ row }">
        <el-tag :type="row.last_status === 'success' ? 'success' : row.last_status === 'failed' ? 'danger' : 'info'">
          {{ row.last_status || '—' }}
        </el-tag>
      </template>
      <template #enabled="{ row }">
        <el-switch :model-value="row.enabled" @click="toggle(row)" />
      </template>
      <template #actions="{ row }">
        <el-button link type="primary" @click="trigger(row)">Run</el-button>
        <el-button link @click="gotoEdit(row)">Edit</el-button>
        <el-button link @click="gotoRuns(row)">Runs</el-button>
        <el-button link @click="openSaveAsTemplate(row)">Save as Template</el-button>
        <el-button link type="danger" @click="remove(row)">Delete</el-button>
      </template>
    </ResponsiveTable>

    <SaveAsTemplateDialog v-model="saveTplVisible" :collector="saveTplCollector" @saved="onTemplateSaved" />
  </div>
</template>
