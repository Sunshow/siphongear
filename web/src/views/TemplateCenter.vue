<script setup lang="ts">
import { onMounted, ref, computed, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import ResponsiveTable, { type RTColumn } from '@/components/ResponsiveTable.vue'

const router = useRouter()

const rows = ref<any[]>([])
const loading = ref(false)

const editor = reactive({
  visible: false,
  isNew: true,
  originalName: '',
  text: ''
})

const importDialog = reactive({
  visible: false,
  text: '',
  onConflict: 'skip' as 'skip' | 'overwrite'
})

const fileInput = ref<HTMLInputElement | null>(null)

const columns: RTColumn[] = [
  { key: 'name', label: 'Name', primary: true },
  { key: 'source', label: 'Source', slot: 'source', width: 120 },
  { key: 'description', label: 'Description' },
  { key: 'needs_credential', label: 'Credential', slot: 'cred', width: 110 },
  { key: 'schedule', label: 'Schedule', slot: 'schedule', width: 200 },
  { key: 'actions', label: 'Actions', slot: 'actions', width: 320 }
]

async function reload() {
  loading.value = true
  try {
    rows.value = await api.templates.list()
  } finally { loading.value = false }
}

function applyTemplate(row: any) {
  router.push({ name: 'collector-new', query: { template: row.name } })
}

function openCreate() {
  editor.isNew = true
  editor.originalName = ''
  editor.text = JSON.stringify(emptyTemplate(), null, 2)
  editor.visible = true
}

function openEdit(row: any) {
  editor.isNew = false
  editor.originalName = row.name
  api.templates.get(row.name).then((t: any) => {
    editor.text = JSON.stringify(t, null, 2)
    editor.visible = true
  }).catch(() => {
    ElMessage.error('failed to load template')
  })
}

async function saveEditor() {
  let body: any
  try {
    body = JSON.parse(editor.text)
  } catch (e: any) {
    ElMessage.error('invalid JSON: ' + e.message)
    return
  }
  if (!body.name) {
    ElMessage.warning('template name is required')
    return
  }
  try {
    if (editor.isNew) {
      await api.templates.create(body)
    } else {
      await api.templates.update(editor.originalName, body)
    }
    editor.visible = false
    await reload()
    ElMessage.success('saved')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'save failed')
  }
}

async function remove(row: any) {
  await ElMessageBox.confirm(`Delete template "${row.name}"?`, 'Confirm')
  try {
    await api.templates.remove(row.name)
    await reload()
    ElMessage.success('deleted')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'delete failed')
  }
}

async function exportOne(row: any) {
  try {
    const data = await api.templates.exportAll([row.name])
    download(data, `siphongear-template-${row.name}.json`)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'export failed')
  }
}

async function exportAll() {
  try {
    const data = await api.templates.exportAll()
    if (!data?.templates?.length) {
      ElMessage.info('no user templates to export')
      return
    }
    const ts = new Date().toISOString().replace(/[:.]/g, '-')
    download(data, `siphongear-templates-${ts}.json`)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'export failed')
  }
}

function download(data: any, filename: string) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

function pickFile() {
  fileInput.value?.click()
}

function onFileSelected(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  if (!f) return
  const reader = new FileReader()
  reader.onload = () => {
    importDialog.text = String(reader.result || '')
    importDialog.visible = true
  }
  reader.readAsText(f)
  input.value = ''
}

async function runImport() {
  let parsed: any
  try {
    parsed = JSON.parse(importDialog.text)
  } catch (e: any) {
    ElMessage.error('invalid JSON: ' + e.message)
    return
  }
  let templates: any[] = []
  if (Array.isArray(parsed)) {
    templates = parsed
  } else if (parsed && Array.isArray(parsed.templates)) {
    templates = parsed.templates
  } else if (parsed && typeof parsed === 'object' && parsed.name) {
    templates = [parsed]
  } else {
    ElMessage.error('JSON must be an array, an object with "templates" array, or a single template object')
    return
  }
  try {
    const res = await api.templates.import({ templates, on_conflict: importDialog.onConflict })
    importDialog.visible = false
    const parts = [
      `imported ${res.imported?.length || 0}`,
      `skipped ${res.skipped?.length || 0}`,
      `builtin ${res.skipped_builtin?.length || 0}`
    ]
    if (res.errors?.length) parts.push(`errors ${res.errors.length}`)
    ElMessage.success(parts.join(' / '))
    if (res.errors?.length) {
      ElMessageBox.alert(res.errors.join('\n'), 'Import errors').catch(() => {})
    }
    await reload()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'import failed')
  }
}

function emptyTemplate() {
  return {
    name: '',
    description: '',
    needs_credential: false,
    credential_hint: null,
    schedule_type: 'none',
    schedule_spec: '',
    timeout: 60,
    variables: [],
    pipeline: { steps: [], indicators: [] },
    indicators: []
  }
}

const sourceTagType = computed(() => (s: string) => s === 'builtin' ? 'info' : 'success')

onMounted(reload)
</script>

<template>
  <div>
    <div class="page-bar">
      <h2>Templates</h2>
      <div class="page-bar-actions">
        <el-button @click="pickFile">Import</el-button>
        <el-button @click="exportAll">Export All</el-button>
        <el-button type="primary" @click="openCreate">New Template</el-button>
      </div>
    </div>
    <input
      ref="fileInput"
      type="file"
      accept="application/json"
      style="display: none"
      @change="onFileSelected"
    />

    <ResponsiveTable :rows="rows" :columns="columns" :loading="loading" row-key="name">
      <template #source="{ row }">
        <el-tag size="small" :type="sourceTagType(row.source)">{{ row.source || 'builtin' }}</el-tag>
      </template>
      <template #cred="{ row }">
        <el-tag size="small" :type="row.needs_credential ? 'warning' : 'info'">
          {{ row.needs_credential ? (row.credential_hint?.type || 'yes') : 'no' }}
        </el-tag>
      </template>
      <template #schedule="{ row }">
        <el-tag size="small">{{ row.schedule_type || 'none' }}</el-tag>
        <span v-if="row.schedule_spec" style="margin-left:6px">{{ row.schedule_spec }}</span>
      </template>
      <template #actions="{ row }">
        <el-button link type="primary" @click="applyTemplate(row)">Apply</el-button>
        <el-button link @click="openEdit(row)" :disabled="row.source === 'builtin'">Edit</el-button>
        <el-button link @click="exportOne(row)">Export</el-button>
        <el-button link type="danger" @click="remove(row)" :disabled="row.source === 'builtin'">Delete</el-button>
      </template>
    </ResponsiveTable>

    <el-dialog
      v-model="editor.visible"
      :title="editor.isNew ? 'New Template' : `Edit Template: ${editor.originalName}`"
      width="780px"
    >
      <el-alert type="info" :closable="false" show-icon style="margin-bottom: 12px">
        Edit the JSON spec directly. Required fields: name, pipeline.steps. Builtin templates are read-only.
      </el-alert>
      <el-input v-model="editor.text" type="textarea" :rows="20" style="font-family: ui-monospace, Menlo, monospace" />
      <template #footer>
        <el-button @click="editor.visible = false">Cancel</el-button>
        <el-button type="primary" @click="saveEditor">Save</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="importDialog.visible" title="Import Templates" width="780px">
      <el-form label-width="120px">
        <el-form-item label="On Conflict">
          <el-radio-group v-model="importDialog.onConflict">
            <el-radio-button label="skip">Skip Existing</el-radio-button>
            <el-radio-button label="overwrite">Overwrite</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="JSON">
          <el-input
            v-model="importDialog.text"
            type="textarea"
            :rows="16"
            style="font-family: ui-monospace, Menlo, monospace"
            placeholder='{ "templates": [ ... ] }'
          />
        </el-form-item>
        <el-alert type="info" :closable="false" show-icon>
          Builtin names are always skipped. Accepts either {"templates":[...]} or a raw array / single template object.
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="importDialog.visible = false">Cancel</el-button>
        <el-button type="primary" @click="runImport">Import</el-button>
      </template>
    </el-dialog>
  </div>
</template>
