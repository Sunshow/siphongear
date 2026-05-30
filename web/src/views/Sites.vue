<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import ResponsiveTable, { type RTColumn } from '@/components/ResponsiveTable.vue'

const rows = ref<any[]>([])
const dialog = ref(false)
const editing = ref<any>(null)
const form = reactive({ id: 0, name: '', base_url: '', tags: '', notes: '' })

const tagList = ref<string[]>([])
const tagInput = ref('')
const tagInputVisible = ref(false)
const tagInputRef = ref<any>(null)

const columns: RTColumn[] = [
  { key: 'id', label: 'ID', width: 80, hideOnMobile: true },
  { key: 'name', label: 'Name', primary: true },
  { key: 'base_url', label: 'Base URL' },
  { key: 'tags', label: 'Tags', width: 220, slot: 'tags' },
  { key: 'actions', label: 'Actions', slot: 'actions', width: 160 }
]

function parseTags(s: string): string[] {
  if (!s) return []
  const out: string[] = []
  const seen = new Set<string>()
  for (const part of s.split(',')) {
    const t = part.trim()
    if (!t || seen.has(t)) continue
    seen.add(t)
    out.push(t)
  }
  return out
}

const dialogTags = computed(() => tagList.value)

const allSiteTags = computed(() => {
  const tags = new Set<string>()
  for (const s of rows.value) {
    for (const t of parseTags(s.tags || '')) tags.add(t)
  }
  return [...tags].sort()
})

const suggestedTagsRemaining = computed(() =>
  allSiteTags.value.filter(t => !tagList.value.includes(t))
)

function pickTag(t: string) {
  if (!tagList.value.includes(t)) tagList.value.push(t)
}

async function reload() { rows.value = await api.sites.list() }

function openCreate() {
  editing.value = null
  Object.assign(form, { id: 0, name: '', base_url: '', tags: '', notes: '' })
  tagList.value = []
  tagInput.value = ''
  tagInputVisible.value = false
  dialog.value = true
}

function openEdit(row: any) {
  editing.value = row
  Object.assign(form, row)
  tagList.value = parseTags(row.tags || '')
  tagInput.value = ''
  tagInputVisible.value = false
  dialog.value = true
}

function showTagInput() {
  tagInputVisible.value = true
  nextTick(() => tagInputRef.value?.focus?.())
}

function commitTagInput() {
  const v = tagInput.value.trim()
  if (v && !tagList.value.includes(v)) {
    tagList.value.push(v)
  }
  tagInput.value = ''
  tagInputVisible.value = false
}

function onTagInputKey(e: KeyboardEvent) {
  if (e.key === ',') {
    e.preventDefault()
    commitTagInput()
    tagInputVisible.value = true
    nextTick(() => tagInputRef.value?.focus?.())
  } else if (e.key === 'Backspace' && tagInput.value === '' && tagList.value.length) {
    tagList.value.pop()
  }
}

function removeTag(t: string) {
  tagList.value = tagList.value.filter(x => x !== t)
}

async function save() {
  if (typeof form.base_url === 'string') {
    form.base_url = form.base_url.trim().replace(/\/+$/, '')
  }
  if (tagInput.value.trim()) commitTagInput()
  form.tags = tagList.value.join(',')
  if (form.id) await api.sites.update(form.id, form)
  else await api.sites.create(form)
  dialog.value = false
  await reload()
  ElMessage.success('saved')
}

async function remove(row: any) {
  await ElMessageBox.confirm(`Delete ${row.name}?`, 'Confirm')
  await api.sites.remove(row.id)
  await reload()
}

onMounted(reload)
</script>

<template>
  <div>
    <div class="page-bar">
      <h2>Sites</h2>
      <div class="page-bar-actions">
        <el-button type="primary" @click="openCreate">New Site</el-button>
      </div>
    </div>
    <ResponsiveTable :rows="rows" :columns="columns" row-key="id">
      <template #tags="{ row }">
        <div class="tag-cell">
          <el-tag
            v-for="t in parseTags(row.tags || '')"
            :key="t"
            size="small"
            effect="plain"
            class="tag-cell-item"
          >{{ t }}</el-tag>
          <span v-if="!parseTags(row.tags || '').length" class="tag-cell-empty">—</span>
        </div>
      </template>
      <template #actions="{ row }">
        <el-button link @click="openEdit(row)">Edit</el-button>
        <el-button link type="danger" @click="remove(row)">Delete</el-button>
      </template>
    </ResponsiveTable>

    <el-dialog v-model="dialog" :title="form.id ? 'Edit Site' : 'New Site'" width="520px">
      <el-form label-width="100px">
        <el-form-item label="Name"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="Base URL"><el-input v-model="form.base_url" /></el-form-item>
        <el-form-item label="Tags">
          <div class="tag-editor">
            <el-tag
              v-for="t in dialogTags"
              :key="t"
              closable
              size="small"
              effect="plain"
              @close="removeTag(t)"
            >{{ t }}</el-tag>
            <el-input
              v-if="tagInputVisible"
              ref="tagInputRef"
              v-model="tagInput"
              size="small"
              class="tag-editor-input"
              @keydown="onTagInputKey"
              @keydown.enter.prevent="commitTagInput"
              @blur="commitTagInput"
            />
            <el-button v-else size="small" plain @click="showTagInput">+ Tag</el-button>
          </div>
          <div v-if="suggestedTagsRemaining.length" class="tag-hints">
            <span class="form-hint">Existing tags:</span>
            <el-tag
              v-for="t in suggestedTagsRemaining"
              :key="t"
              size="small"
              effect="plain"
              class="tag-hint-item"
              @click="pickTag(t)"
            >+ {{ t }}</el-tag>
          </div>
        </el-form-item>
        <el-form-item label="Notes"><el-input v-model="form.notes" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">Cancel</el-button>
        <el-button type="primary" @click="save">Save</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.tag-cell {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.tag-cell-item {
  margin: 0;
}
.tag-cell-empty {
  color: var(--sg-text-muted);
}
.tag-editor {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}
.tag-editor-input {
  width: 120px;
}
.tag-hints {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
  align-items: center;
}
.tag-hint-item {
  cursor: pointer;
}
.form-hint {
  color: var(--sg-text-muted);
  font-size: 12px;
}
</style>
