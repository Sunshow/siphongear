<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import StepEditor from '@/components/StepEditor.vue'
import IndicatorsEditor from '@/components/IndicatorsEditor.vue'
import DryRunPanel from '@/components/DryRunPanel.vue'
import TemplatePicker from '@/components/TemplatePicker.vue'

const route = useRoute()
const router = useRouter()
const id = computed(() => Number(route.params.id || 0))

const sites = ref<any[]>([])
const stepMeta = ref<any[]>([])
const drawer = ref(false)
const dryResult = ref<any>(null)
const templateDialog = ref(false)

const collector = ref<any>({
  id: 0,
  site_id: 0,
  name: '',
  description: '',
  pipeline_json: '',
  schedule_type: 'none',
  schedule_spec: '',
  enabled: true,
  timeout: 60
})

const def = ref<any>({ steps: [], indicators: [] })
const indicators = ref<any[]>([])
const addStepDialog = ref(false)
const addStepStage = ref<string>('input')
const addStepKind = ref<string>('')
const addInsertAt = ref<number>(-1)

const stages = [
  { key: 'input', color: '#409eff', label: 'INPUT' },
  { key: 'fetch', color: '#e6a23c', label: 'FETCH' },
  { key: 'transform', color: '#9b59b6', label: 'TRANSFORM' },
  { key: 'parse', color: '#67c23a', label: 'PARSE' },
  { key: 'extract', color: '#f56c6c', label: 'EXTRACT' }
]

function stageOf(kind: string): string {
  const m = stepMeta.value.find(x => x.kind === kind)
  return m?.stage || 'any'
}

function stageColor(kind: string): string {
  const s = stages.find(x => x.key === stageOf(kind))
  return s?.color || '#909399'
}

function stageLabel(kind: string): string {
  const s = stages.find(x => x.key === stageOf(kind))
  return s?.label || stageOf(kind).toUpperCase()
}

function metaOf(kind: string) {
  return stepMeta.value.find(x => x.kind === kind) || { schema: {} }
}

function kindsByStage(stage: string) {
  return stepMeta.value.filter(s => s.stage === stage)
}

function openAdd(insertAt = -1) {
  addInsertAt.value = insertAt
  addStepStage.value = 'input'
  const candidates = kindsByStage('input')
  addStepKind.value = candidates[0]?.kind || ''
  addStepDialog.value = true
}

function confirmAdd() {
  if (!addStepKind.value) return
  const newStep = { kind: addStepKind.value, name: '', config: {}, enabled: true }
  if (addInsertAt.value < 0 || addInsertAt.value > def.value.steps.length) {
    def.value.steps.push(newStep)
  } else {
    def.value.steps.splice(addInsertAt.value, 0, newStep)
  }
  addStepDialog.value = false
}

function removeStep(idx: number) {
  def.value.steps.splice(idx, 1)
}

function moveStep(idx: number, dir: -1 | 1) {
  const j = idx + dir
  if (j < 0 || j >= def.value.steps.length) return
  const tmp = def.value.steps[idx]
  def.value.steps[idx] = def.value.steps[j]
  def.value.steps[j] = tmp
}

async function load() {
  [sites.value, stepMeta.value] = await Promise.all([api.sites.list(), api.registry()])
  if (id.value) {
    const c = await api.collectors.get(id.value)
    collector.value = c
    try { def.value = c.pipeline_json ? JSON.parse(c.pipeline_json) : { steps: [], indicators: [] } }
    catch (e) { def.value = { steps: [], indicators: [] } }
    indicators.value = await api.indicators.list(id.value)
  }
}

async function save() {
  collector.value.pipeline_json = JSON.stringify(def.value)
  try {
    if (collector.value.id) {
      await api.collectors.update(collector.value.id, collector.value)
    } else {
      const res = await api.collectors.create(collector.value)
      collector.value = res
      router.replace({ name: 'collector-edit', params: { id: res.id } })
    }
    ElMessage.success('saved')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'save failed')
  }
}

async function dryRun() {
  if (!collector.value.id) {
    ElMessage.warning('save first')
    return
  }
  collector.value.pipeline_json = JSON.stringify(def.value)
  await api.collectors.update(collector.value.id, collector.value)
  try {
    const res = await api.collectors.dryrun(collector.value.id, {})
    dryResult.value = res
    drawer.value = true
  } catch (e: any) {
    dryResult.value = e?.response?.data || { error: 'failed' }
    drawer.value = true
  }
}

async function applyTemplate(payload: { template: any; vars: Record<string, string>; credentialId: number; name: string; siteId: number }) {
  const tpl = payload.template
  const vars: Record<string, string> = { ...payload.vars }
  if (typeof vars.base_url === 'string') {
    vars.base_url = vars.base_url.trim().replace(/\/+$/, '')
  }
  const cloned = JSON.parse(JSON.stringify(tpl.pipeline)) as { steps: any[]; indicators: any[] }
  for (const step of cloned.steps) {
    if (step.kind === 'input.credential' && step.config) {
      step.config.credential_id = payload.credentialId
    }
    if (step.config) {
      for (const k of Object.keys(step.config)) {
        const v = step.config[k]
        if (typeof v === 'string') {
          step.config[k] = substituteVars(v, vars)
        } else if (v && typeof v === 'object' && !Array.isArray(v)) {
          for (const kk of Object.keys(v)) {
            if (typeof v[kk] === 'string') v[kk] = substituteVars(v[kk], vars)
          }
        }
      }
    }
  }
  def.value = cloned
  collector.value.name = payload.name
  collector.value.site_id = payload.siteId
  collector.value.timeout = tpl.timeout || 60
  collector.value.schedule_type = tpl.schedule_type || 'none'
  collector.value.schedule_spec = tpl.schedule_spec || ''
  collector.value.enabled = true
  collector.value.pipeline_json = JSON.stringify(def.value)

  try {
    sites.value = await api.sites.list()
    if (collector.value.id) {
      await api.collectors.update(collector.value.id, collector.value)
    } else {
      const created = await api.collectors.create(collector.value)
      collector.value = created
      try { def.value = created.pipeline_json ? JSON.parse(created.pipeline_json) : { steps: [], indicators: [] } } catch {}
      router.replace({ name: 'collector-edit', params: { id: created.id } })
    }

    const pending = (tpl.indicators || []) as any[]
    if (collector.value.id && pending.length) {
      for (const ind of pending) {
        try { await api.indicators.create(collector.value.id, ind) } catch {}
      }
      indicators.value = await api.indicators.list(collector.value.id)
    }

    ElMessage.success(`Collector "${collector.value.name}" created. ${pending.length ? pending.length + ' indicator(s) added.' : ''}`)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'save failed')
  }
}

function substituteVars(s: string, vars: Record<string, string>): string {
  let out = s
  for (const k of Object.keys(vars)) {
    out = out.split(`{{${k.toUpperCase()}}}`).join(vars[k]).split(`{{${k}}}`).join(vars[k])
  }
  return out
}

async function saveWithIndicators() {
  await save()
}

onMounted(load)
</script>

<template>
  <div>
    <div class="bar">
      <h2>{{ collector.id ? `Edit Collector #${collector.id}` : 'New Collector' }}</h2>
      <div>
        <el-button @click="templateDialog = true">From Template</el-button>
        <el-button @click="dryRun" :disabled="!collector.id">Dry Run</el-button>
        <el-button type="primary" @click="saveWithIndicators">Save</el-button>
      </div>
    </div>

    <el-tabs>
      <el-tab-pane label="Basics">
        <el-form label-width="140px" style="max-width: 720px">
          <el-form-item label="Name"><el-input v-model="collector.name" /></el-form-item>
          <el-form-item label="Site">
            <el-select v-model="collector.site_id">
              <el-option v-for="s in sites" :key="s.id" :label="s.name" :value="s.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="Description"><el-input v-model="collector.description" type="textarea" :rows="2" /></el-form-item>
          <el-form-item label="Enabled"><el-switch v-model="collector.enabled" /></el-form-item>
          <el-form-item label="Timeout (s)"><el-input-number v-model="collector.timeout" :min="1" :max="3600" /></el-form-item>
          <el-form-item label="Schedule Type">
            <el-select v-model="collector.schedule_type">
              <el-option label="None" value="none" />
              <el-option label="Interval" value="interval" />
              <el-option label="Cron" value="cron" />
              <el-option label="Event" value="event" />
            </el-select>
          </el-form-item>
          <el-form-item label="Schedule Spec">
            <el-input v-model="collector.schedule_spec"
              :placeholder="collector.schedule_type === 'interval' ? '5m / 30s / 1h' : collector.schedule_type === 'cron' ? '0 */15 * * * *' : 'collector.<id>.completed'" />
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="Pipeline">
        <div class="pipeline-toolbar">
          <el-button type="primary" @click="openAdd(-1)">+ Add Step</el-button>
          <el-text type="info" style="margin-left: 12px;">
            Steps execute top-to-bottom. Reorder with ↑/↓ or insert at any position.
          </el-text>
        </div>
        <el-empty v-if="!def.steps.length" description="No steps yet — click + Add Step or use a template" />
        <div v-for="(step, i) in def.steps" :key="i" class="step-card">
          <div class="step-head">
            <span class="stage-badge" :style="{ background: stageColor(step.kind) }">
              #{{ i + 1 }} · {{ stageLabel(step.kind) }}
            </span>
            <el-select v-model="def.steps[i].kind" size="small" style="width: 220px; margin-left: 8px">
              <el-option-group v-for="g in stages" :key="g.key" :label="g.label">
                <el-option v-for="m in kindsByStage(g.key)" :key="m.kind" :label="m.kind" :value="m.kind" />
              </el-option-group>
            </el-select>
            <el-input v-model="def.steps[i].name" size="small" placeholder="optional name" style="width: 160px; margin-left: 8px" />
            <el-switch v-model="def.steps[i].enabled" :default-value="true" style="margin-left: 8px" />
            <div class="spacer"></div>
            <el-button-group>
              <el-button size="small" @click="moveStep(i, -1)" :disabled="i === 0">↑</el-button>
              <el-button size="small" @click="moveStep(i, 1)" :disabled="i === def.steps.length - 1">↓</el-button>
              <el-button size="small" @click="openAdd(i + 1)">Insert↓</el-button>
              <el-button size="small" type="danger" @click="removeStep(i)">Delete</el-button>
            </el-button-group>
          </div>
          <div class="step-desc">{{ metaOf(def.steps[i].kind).description }}</div>
          <StepEditor :schema="metaOf(def.steps[i].kind).schema" v-model="def.steps[i].config" />
        </div>
      </el-tab-pane>

      <el-tab-pane label="Indicators" :disabled="!collector.id">
        <IndicatorsEditor v-if="collector.id" :collector-id="collector.id" v-model:indicators="indicators" />
      </el-tab-pane>
    </el-tabs>

    <el-drawer v-model="drawer" title="Dry Run" size="60%">
      <DryRunPanel :result="dryResult" />
    </el-drawer>

    <TemplatePicker v-model="templateDialog" :site-id="collector.site_id" @apply="applyTemplate" />

    <el-dialog v-model="addStepDialog" title="Add Step" width="480px">
      <el-form label-width="80px">
        <el-form-item label="Stage">
          <el-select v-model="addStepStage" @change="(v: string) => { const c = kindsByStage(v); addStepKind = c[0]?.kind || '' }">
            <el-option v-for="g in stages" :key="g.key" :label="g.label" :value="g.key" />
          </el-select>
        </el-form-item>
        <el-form-item label="Kind">
          <el-select v-model="addStepKind">
            <el-option v-for="m in kindsByStage(addStepStage)" :key="m.kind" :label="m.kind" :value="m.kind">
              <span>{{ m.kind }}</span>
              <span style="margin-left: 8px; color: #999; font-size: 12px">{{ m.description }}</span>
            </el-option>
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addStepDialog = false">Cancel</el-button>
        <el-button type="primary" @click="confirmAdd">Add</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.bar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.pipeline-toolbar { margin-bottom: 12px; display: flex; align-items: center; }
.step-card { border: 1px solid #eaecef; border-radius: 6px; padding: 12px; margin-bottom: 10px; position: relative; }
.step-card::after {
  content: '';
  position: absolute;
  left: 50%;
  bottom: -10px;
  width: 0;
  height: 0;
  border-left: 6px solid transparent;
  border-right: 6px solid transparent;
  border-top: 8px solid #c0c4cc;
  transform: translateX(-50%);
}
.step-card:last-child::after { display: none; }
.step-head { display: flex; align-items: center; }
.stage-badge { color: #fff; padding: 2px 8px; font-size: 11px; border-radius: 3px; font-weight: 500; letter-spacing: 0.5px; }
.spacer { flex: 1; }
.step-desc { color: #888; font-size: 12px; margin: 6px 0 8px; }
</style>
