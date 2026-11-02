<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import StepEditor from '@/components/StepEditor.vue'
import IndicatorsEditor from '@/components/IndicatorsEditor.vue'
import DryRunPanel from '@/components/DryRunPanel.vue'
import TemplatePicker from '@/components/TemplatePicker.vue'

interface SessionRow {
  id: number
  key: string
  collector_id: number
  cookie_count: number
  var_keys: string
  login_count: number
  last_login_at: string | null
  last_used_at: string | null
  expires_at: string | null
  status: string
  created_at: string
  updated_at: string
}

const route = useRoute()
const router = useRouter()
const id = computed(() => Number(route.params.id || 0))

const sites = ref<any[]>([])
const stepMeta = ref<any[]>([])
const drawer = ref(false)
const dryResult = ref<any>(null)
const templateDialog = ref(false)
const presetTemplate = ref<string>('')

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

const activeTab = ref('basics')
const sessions = ref<SessionRow[]>([])
const sessionsLoading = ref(false)
const clearingSessionId = ref(0)
const clearingAllSessions = ref(false)

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

function normalizeSteps(d: any) {
  if (d && Array.isArray(d.steps)) {
    for (const s of d.steps) s.enabled = s.enabled !== false
  }
  return d
}

// --- per-step run conditions -------------------------------------------------

function stepWhen(step: any): string {
  return typeof step?.when === 'string' ? step.when : ''
}

function isConditionalStep(step: any): boolean {
  return stepWhen(step).trim() !== ''
}

// Empty/undefined `on_error` means "fail" (the pipeline default); display it that
// way instead of showing an empty select. Only an explicit pick is written back.
function stepOnError(step: any): string {
  return step?.on_error === 'continue' ? 'continue' : 'fail'
}

function setStepOnError(idx: number, v: string) {
  if (v !== 'fail' && v !== 'continue') return
  def.value.steps[idx].on_error = v
}

// Serialize the pipeline for the API. `when`/`on_error` stay top-level step
// fields as defined by the session-reuse contract; a blank `when` is dropped so
// the backend never tries to evaluate an empty expression.
function pipelineJson(): string {
  const clone = JSON.parse(JSON.stringify(def.value))
  if (Array.isArray(clone?.steps)) {
    for (const s of clone.steps) {
      if (typeof s.when === 'string' && !s.when.trim()) delete s.when
    }
  }
  return JSON.stringify(clone)
}

// --- sessions ----------------------------------------------------------------

function formatSessionDate(s: string | null | undefined): string {
  if (!s) return '-'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  return d.toLocaleString()
}

function sessionVars(row: SessionRow): string[] {
  return (row.var_keys || '')
    .split(',')
    .map(k => k.trim())
    .filter(Boolean)
}

async function loadSessions() {
  if (!collector.value.id) {
    sessions.value = []
    return
  }
  sessionsLoading.value = true
  try {
    sessions.value = await api.sessions.list(collector.value.id)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'load sessions failed')
  } finally {
    sessionsLoading.value = false
  }
}

function onTabChange(name: any) {
  if (String(name) === 'session') loadSessions()
}

async function clearSession(row: SessionRow) {
  try {
    await ElMessageBox.confirm(
      `Clear session "${row.key}"? The next run will log in again and create a new session.`,
      'Confirm',
      { type: 'warning', confirmButtonText: 'Clear' }
    )
  } catch {
    return
  }
  clearingSessionId.value = row.id
  try {
    await api.sessions.remove(row.id)
    await loadSessions()
    ElMessage.success(`session "${row.key}" cleared`)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'clear session failed')
  } finally {
    clearingSessionId.value = 0
  }
}

async function clearAllSessions() {
  if (!collector.value.id) return
  try {
    await ElMessageBox.confirm(
      `Clear all sessions for collector #${collector.value.id}? The next run will log in again.`,
      'Confirm',
      { type: 'warning', confirmButtonText: 'Clear all' }
    )
  } catch {
    return
  }
  clearingAllSessions.value = true
  try {
    const res = await api.sessions.clearCollector(collector.value.id)
    await loadSessions()
    ElMessage.success(`cleared ${Number(res?.deleted ?? 0)} session(s)`)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'clear sessions failed')
  } finally {
    clearingAllSessions.value = false
  }
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
    try { def.value = c.pipeline_json ? normalizeSteps(JSON.parse(c.pipeline_json)) : { steps: [], indicators: [] } }
    catch (e) { def.value = { steps: [], indicators: [] } }
    indicators.value = await api.indicators.list(id.value)
  }
  if (!id.value && (route.query.template || route.query.fromTemplate === '1')) {
    presetTemplate.value = route.query.template ? String(route.query.template) : ''
    templateDialog.value = true
  }
}

async function save() {
  collector.value.pipeline_json = pipelineJson()
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
  collector.value.pipeline_json = pipelineJson()
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

async function applyTemplate(payload: { template: any; vars: Record<string, string>; credentialId: number; name: string; siteId: number; hiddenKeys?: string[]; scheduleType?: string; scheduleSpec?: string }) {
  const tpl = payload.template
  const vars: Record<string, string> = { ...payload.vars }
  if (typeof vars.base_url === 'string') {
    vars.base_url = vars.base_url.trim().replace(/\/+$/, '')
  }
  const cloned = normalizeSteps(JSON.parse(JSON.stringify(tpl.pipeline))) as { steps: any[]; indicators: any[] }
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
  collector.value.schedule_type = payload.scheduleType ?? tpl.schedule_type ?? 'none'
  collector.value.schedule_spec = payload.scheduleSpec ?? tpl.schedule_spec ?? ''
  collector.value.enabled = true
  collector.value.pipeline_json = pipelineJson()

  try {
    sites.value = await api.sites.list()
    if (collector.value.id) {
      await api.collectors.update(collector.value.id, collector.value)
    } else {
      const created = await api.collectors.create(collector.value)
      collector.value = created
      try { def.value = created.pipeline_json ? normalizeSteps(JSON.parse(created.pipeline_json)) : { steps: [], indicators: [] } } catch {}
      router.replace({ name: 'collector-edit', params: { id: created.id } })
    }

    const pending = (tpl.indicators || []) as any[]
    if (collector.value.id && pending.length) {
      const hiddenSet = new Set(payload.hiddenKeys || [])
      for (const ind of pending) {
        try { await api.indicators.create(collector.value.id, { ...ind, hidden: hiddenSet.has(ind.key) }) } catch {}
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
    <div class="page-bar">
      <h2>{{ collector.id ? `Edit Collector #${collector.id}` : 'New Collector' }}</h2>
      <div class="page-bar-actions">
        <el-button @click="templateDialog = true">From Template</el-button>
        <el-button @click="dryRun" :disabled="!collector.id">Dry Run</el-button>
        <el-button type="primary" @click="saveWithIndicators">Save</el-button>
      </div>
    </div>

    <el-tabs v-model="activeTab" @tab-change="onTabChange">
      <el-tab-pane label="Basics" name="basics">
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

      <el-tab-pane label="Pipeline" name="pipeline">
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
            <el-tag
              v-if="isConditionalStep(def.steps[i])"
              size="small"
              type="warning"
              effect="plain"
              class="step-when-tag"
              :title="stepWhen(def.steps[i])"
            >when: {{ stepWhen(def.steps[i]) }}</el-tag>
            <div class="spacer"></div>
            <el-button-group>
              <el-button size="small" @click="moveStep(i, -1)" :disabled="i === 0">↑</el-button>
              <el-button size="small" @click="moveStep(i, 1)" :disabled="i === def.steps.length - 1">↓</el-button>
              <el-button size="small" @click="openAdd(i + 1)">Insert↓</el-button>
              <el-button size="small" type="danger" @click="removeStep(i)">Delete</el-button>
            </el-button-group>
          </div>
          <div class="step-desc">{{ metaOf(def.steps[i].kind).description }}</div>
          <div class="step-advanced">
            <div class="adv-field adv-when">
              <span class="adv-label">Run if</span>
              <el-input
                v-model="def.steps[i].when"
                size="small"
                clearable
                placeholder="e.g. !vars.session_ok"
              />
            </div>
            <div class="adv-field">
              <span class="adv-label">On error</span>
              <el-select
                :model-value="stepOnError(def.steps[i])"
                size="small"
                class="adv-error-select"
                @update:model-value="(v: string) => setStepOnError(i, v)"
              >
                <el-option label="Fail" value="fail" />
                <el-option label="Continue" value="continue" />
              </el-select>
            </div>
            <span class="adv-hint">
              Step is skipped when the expression is falsy · "Continue" records the error and keeps the pipeline going.
            </span>
          </div>
          <StepEditor :schema="metaOf(def.steps[i].kind).schema" v-model="def.steps[i].config" />
        </div>
      </el-tab-pane>

      <el-tab-pane label="Indicators" name="indicators" :disabled="!collector.id">
        <IndicatorsEditor v-if="collector.id" :collector-id="collector.id" v-model:indicators="indicators" />
      </el-tab-pane>

      <el-tab-pane label="Session" name="session" :disabled="!collector.id" lazy>
        <el-alert
          type="info"
          :closable="false"
          show-icon
          class="session-alert"
          title="Sessions keep the login state across scheduled runs"
        >
          <template #default>
            <div class="session-alert-text">
              Sessions store the login state (cookies and selected vars, <strong>encrypted at rest</strong>) so scheduled
              runs reuse it instead of logging in every time — that keeps sites from treating repeated logins as new
              devices and hitting device/connection limits. Clear a session after changing credentials or when the
              account is logged out elsewhere; the next run will log in again and store a fresh session.
            </div>
          </template>
        </el-alert>

        <div class="session-toolbar">
          <el-button :loading="sessionsLoading" @click="loadSessions">Refresh</el-button>
          <el-button
            type="danger"
            plain
            :loading="clearingAllSessions"
            :disabled="!collector.id || !sessions.length"
            @click="clearAllSessions"
          >Clear all</el-button>
          <span v-if="sessions.length" class="muted session-count">{{ sessions.length }} session(s)</span>
        </div>

        <el-table
          v-loading="sessionsLoading"
          :data="sessions"
          row-key="id"
          size="small"
          class="session-table"
        >
          <el-table-column prop="key" label="Key" min-width="170" show-overflow-tooltip />
          <el-table-column label="Status" width="100">
            <template #default="{ row }">
              <el-tag size="small" :type="row.status === 'active' ? 'success' : 'warning'">
                {{ row.status || 'unknown' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="Cookies" width="90" align="right">
            <template #default="{ row }">{{ row.cookie_count ?? 0 }}</template>
          </el-table-column>
          <el-table-column label="Vars" min-width="160">
            <template #default="{ row }">
              <template v-if="sessionVars(row).length">
                <el-tag
                  v-for="k in sessionVars(row)"
                  :key="k"
                  size="small"
                  effect="plain"
                  class="session-var-tag"
                >{{ k }}</el-tag>
              </template>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>
          <el-table-column label="Logins" width="90" align="right">
            <template #default="{ row }">
              <el-tooltip :content="`Last login: ${formatSessionDate(row.last_login_at)}`" placement="top">
                <span>{{ row.login_count ?? 0 }}</span>
              </el-tooltip>
            </template>
          </el-table-column>
          <el-table-column label="Last used" width="180">
            <template #default="{ row }">
              <span :class="{ muted: !row.last_used_at }">{{ formatSessionDate(row.last_used_at) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="Expires" width="180">
            <template #default="{ row }">
              <span :class="{ muted: !row.expires_at }">{{ formatSessionDate(row.expires_at) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="Actions" width="110" fixed="right">
            <template #default="{ row }">
              <el-button
                link
                type="danger"
                :loading="clearingSessionId === row.id"
                @click="clearSession(row)"
              >Clear</el-button>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty
              description="No stored session yet — a run that logs in will create one and later runs reuse it."
              :image-size="60"
            />
          </template>
        </el-table>
      </el-tab-pane>
    </el-tabs>

    <el-drawer v-model="drawer" title="Dry Run" size="60%">
      <DryRunPanel :result="dryResult" />
    </el-drawer>

    <TemplatePicker
      v-model="templateDialog"
      :site-id="collector.site_id"
      :preset-name="presetTemplate"
      @apply="applyTemplate"
    />

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
.pipeline-toolbar { margin-bottom: 12px; display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.step-card {
  border: 1px solid var(--sg-border-soft);
  background: var(--sg-bg-card);
  border-radius: var(--sg-radius);
  padding: 12px;
  margin-bottom: 12px;
  position: relative;
  box-shadow: var(--ep-box-shadow-light);
}
.step-card::after {
  content: '';
  position: absolute;
  left: 50%;
  bottom: -10px;
  width: 0;
  height: 0;
  border-left: 6px solid transparent;
  border-right: 6px solid transparent;
  border-top: 8px solid var(--sg-border);
  transform: translateX(-50%);
}
.step-card:last-child::after { display: none; }
.step-head { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.stage-badge { color: #fff; padding: 3px 10px; font-size: 11px; border-radius: 4px; font-weight: 500; letter-spacing: 0.5px; }
.spacer { flex: 1; }
.step-desc { color: var(--sg-text-secondary); font-size: 12px; margin: 6px 0 8px; }
.step-when-tag { max-width: 220px; }
.step-when-tag :deep(.el-tag__content) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.step-advanced {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin: 4px 0 10px;
  padding: 8px 10px;
  border: 1px dashed var(--sg-border-soft);
  border-radius: var(--sg-radius);
  background: var(--sg-bg-page);
}
.adv-field { display: flex; align-items: center; gap: 6px; }
.adv-label { color: var(--sg-text-secondary); font-size: 12px; white-space: nowrap; }
.adv-when :deep(.el-input) { width: 260px; }
.adv-error-select { width: 120px; }
.adv-hint { flex: 1 1 240px; color: var(--sg-text-muted); font-size: 12px; line-height: 1.4; }
.session-alert { margin-bottom: 12px; }
.session-alert-text { line-height: 1.5; }
.session-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-bottom: 12px; }
.session-count { font-size: 12px; }
.session-table { width: 100%; }
.session-var-tag { margin: 0 4px 2px 0; }
@media (max-width: 768px) {
  .step-head :deep(.el-select),
  .step-head :deep(.el-input) {
    width: 100% !important;
    margin-left: 0 !important;
  }
  .step-head :deep(.el-button-group) {
    width: 100%;
    display: flex;
  }
  .step-head :deep(.el-button-group .el-button) {
    flex: 1;
  }
  .step-advanced { flex-direction: column; align-items: stretch; }
  .adv-field { flex-wrap: wrap; }
  .adv-when :deep(.el-input),
  .adv-error-select { width: 100% !important; }
  .adv-hint { flex-basis: auto; }
}
</style>
