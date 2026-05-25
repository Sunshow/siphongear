<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'

const props = defineProps<{ modelValue: boolean; collector: any }>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'saved', name: string): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v)
})

const form = reactive<{
  name: string
  description: string
  needs_credential: boolean
  tokenize_base_url: boolean
  credential_type: string
  fields: Array<{ name: string; label: string; type: string; required: boolean; placeholder: string }>
}>({
  name: '',
  description: '',
  needs_credential: false,
  tokenize_base_url: true,
  credential_type: 'password',
  fields: []
})

const sites = ref<any[]>([])
const indicators = ref<any[]>([])
const submitting = ref(false)
const baseURLPlaceholder = '{{BASE_URL}}'

watch(visible, async v => {
  if (!v) return
  const c = props.collector
  form.name = (c?.name ? `${c.name}-template` : '').toLowerCase().replace(/\s+/g, '-')
  form.description = c?.description || ''
  form.needs_credential = pipelineHasCredential(c?.pipeline_json)
  form.tokenize_base_url = true
  form.credential_type = 'password'
  form.fields = []
  sites.value = await api.sites.list()
  indicators.value = c?.id ? await api.indicators.list(c.id) : []
})

function pipelineHasCredential(json: string): boolean {
  if (!json) return false
  try {
    const def = JSON.parse(json)
    return Array.isArray(def?.steps) && def.steps.some((s: any) => s.kind === 'input.credential')
  } catch { return false }
}

function addField() {
  form.fields.push({ name: '', label: '', type: 'text', required: true, placeholder: '' })
}
function removeField(idx: number) {
  form.fields.splice(idx, 1)
}

function siteBaseURL(): string {
  const s = sites.value.find(x => x.id === props.collector?.site_id)
  return (s?.base_url || '').trim().replace(/\/+$/, '')
}

function tokenizeStrings(obj: any, baseURL: string) {
  if (!obj || typeof obj !== 'object') return
  for (const k of Object.keys(obj)) {
    const v = obj[k]
    if (typeof v === 'string') {
      if (baseURL && v.includes(baseURL)) {
        obj[k] = v.split(baseURL).join('{{BASE_URL}}')
      }
    } else if (v && typeof v === 'object') {
      tokenizeStrings(v, baseURL)
    }
  }
}

async function save() {
  const c = props.collector
  if (!c) return
  if (!form.name.trim()) {
    ElMessage.warning('template name is required')
    return
  }
  let def: any = { steps: [], indicators: [] }
  try {
    def = c.pipeline_json ? JSON.parse(c.pipeline_json) : { steps: [], indicators: [] }
  } catch (e: any) {
    ElMessage.error('collector pipeline JSON invalid: ' + e.message)
    return
  }
  def = JSON.parse(JSON.stringify(def))

  const variables: any[] = []
  if (form.tokenize_base_url) {
    const baseURL = siteBaseURL()
    if (baseURL) {
      for (const step of def.steps || []) {
        if (step?.config) tokenizeStrings(step.config, baseURL)
      }
    }
    variables.push({ name: 'base_url', label: 'Base URL', placeholder: baseURL || 'http://example.com:port', required: true, default: '' })
  }

  if (form.needs_credential) {
    for (const step of def.steps || []) {
      if (step?.kind === 'input.credential' && step.config) {
        step.config.credential_id = 0
      }
    }
  }

  const tplIndicators = (indicators.value || []).map((i: any) => ({
    key: i.key,
    name: i.name,
    type: i.type,
    unit: i.unit,
    display: i.display
  }))

  const credential_hint = form.needs_credential
    ? {
        type: form.credential_type,
        fields: form.fields
          .filter(f => f.name.trim())
          .map(f => ({ name: f.name.trim(), label: f.label || f.name, type: f.type, required: !!f.required, placeholder: f.placeholder }))
      }
    : null

  const body: any = {
    name: form.name.trim(),
    description: form.description,
    needs_credential: form.needs_credential,
    credential_hint,
    schedule_type: c.schedule_type || 'none',
    schedule_spec: c.schedule_spec || '',
    timeout: c.timeout || 60,
    variables,
    pipeline: def,
    indicators: tplIndicators
  }

  submitting.value = true
  try {
    await api.templates.create(body)
    ElMessage.success(`template "${body.name}" saved`)
    visible.value = false
    emit('saved', body.name)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'save failed')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <el-dialog v-model="visible" title="Save as Template" width="720px">
    <el-form label-width="160px">
      <el-form-item label="Template Name" required>
        <el-input v-model="form.name" placeholder="my-collector-template" />
      </el-form-item>
      <el-form-item label="Description">
        <el-input v-model="form.description" type="textarea" :rows="2" />
      </el-form-item>
      <el-form-item label="Tokenize Base URL">
        <el-switch v-model="form.tokenize_base_url" />
        <el-text type="info" style="margin-left: 12px">
          Replace site base_url occurrences with <code>{{ baseURLPlaceholder }}</code> variable.
        </el-text>
      </el-form-item>
      <el-form-item label="Needs Credential">
        <el-switch v-model="form.needs_credential" />
      </el-form-item>

      <template v-if="form.needs_credential">
        <el-divider content-position="left">Credential Hint</el-divider>
        <el-form-item label="Type">
          <el-select v-model="form.credential_type" style="width: 200px">
            <el-option label="password" value="password" />
            <el-option label="token" value="token" />
            <el-option label="cookie" value="cookie" />
            <el-option label="custom" value="custom" />
          </el-select>
        </el-form-item>
        <el-form-item label="Fields">
          <el-button size="small" @click="addField">+ Add Field</el-button>
        </el-form-item>
        <div v-for="(f, idx) in form.fields" :key="idx" class="field-row">
          <el-input v-model="f.name" placeholder="name" style="width: 140px" />
          <el-input v-model="f.label" placeholder="label" style="width: 140px" />
          <el-select v-model="f.type" style="width: 120px">
            <el-option label="text" value="text" />
            <el-option label="password" value="password" />
          </el-select>
          <el-checkbox v-model="f.required">required</el-checkbox>
          <el-input v-model="f.placeholder" placeholder="placeholder" style="flex: 1" />
          <el-button size="small" type="danger" link @click="removeField(idx)">Delete</el-button>
        </div>
      </template>

      <el-alert type="info" :closable="false" show-icon style="margin-top: 12px">
        Pipeline + indicators are copied from this Collector. Credentials are NOT exported.
      </el-alert>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">Cancel</el-button>
      <el-button type="primary" :loading="submitting" @click="save">Save</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.field-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
</style>
