<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import { useUiStore } from '@/store/ui'
import ResponsiveTable, { type RTColumn } from '@/components/ResponsiveTable.vue'

const route = useRoute()
const router = useRouter()
const ui = useUiStore()
const id = computed(() => Number(route.params.id))

const runs = ref<any[]>([])
const indicators = ref<any[]>([])
const collectorName = ref('')
const selected = ref<number | null>(null)
const dateRange = ref<[Date, Date] | null>(null)
const page = ref(1)
const pageSize = ref(50)
const total = ref(0)
const loading = ref(false)
const error = ref('')
let requestID = 0

const indicatorByID = computed<Record<number, any>>(() => {
  const m: Record<number, any> = {}
  for (const indicator of indicators.value) m[indicator.id] = indicator
  return m
})

function dpDisplay(row: any): string {
  if (row.value_num !== null && row.value_num !== undefined) return String(row.value_num)
  if (row.value_str !== null && row.value_str !== undefined) return row.value_str
  if (row.value_json !== null && row.value_json !== undefined) return String(row.value_json)
  return '—'
}

function visibleDataPoints(row: any): any[] {
  const points = row.data_points || []
  return selected.value ? points.filter((dp: any) => dp.indicator_id === selected.value) : points
}

const columns: RTColumn[] = [
  { key: 'started', label: 'Started', slot: 'started', width: 200, primary: true },
  { key: 'id', label: 'Run', slot: 'run', width: 90 },
  { key: 'status', label: 'Status', slot: 'status', width: 110 },
  { key: 'trigger', label: 'Trigger', width: 100 },
  { key: 'duration', label: 'Duration', slot: 'duration', width: 110 },
  { key: 'datapoints', label: 'Data Points', slot: 'datapoints' },
  { key: 'error', label: 'Error', slot: 'error' }
]

const dateShortcuts = [
  { text: 'Today', value: () => [new Date(), new Date()] },
  ...[7, 30].map(days => ({
    text: `Last ${days} days`,
    value: () => {
      const start = new Date()
      start.setDate(start.getDate() - days + 1)
      return [start, new Date()]
    }
  }))
]

async function loadHistory() {
  const currentRequest = ++requestID
  loading.value = true
  error.value = ''
  try {
    const params: Record<string, string | number> = {
      limit: pageSize.value,
      offset: (page.value - 1) * pageSize.value
    }
    if (selected.value) params.indicator_id = selected.value
    if (dateRange.value?.length === 2) {
      const start = new Date(dateRange.value[0])
      start.setHours(0, 0, 0, 0)
      const end = new Date(dateRange.value[1])
      end.setHours(0, 0, 0, 0)
      end.setDate(end.getDate() + 1)
      params.from = start.toISOString()
      params.to = end.toISOString()
    }
    const result = await api.collectors.history(id.value, params)
    if (currentRequest !== requestID) return
    runs.value = result.runs || []
    total.value = result.total
    // Cleanup may remove the last page while it is being viewed.
    const lastPage = Math.max(1, Math.ceil(total.value / pageSize.value))
    if (page.value > lastPage) page.value = lastPage
  } catch (e: any) {
    if (currentRequest !== requestID) return
    runs.value = []
    total.value = 0
    error.value = e?.response?.data?.error || 'Could not load history. Please try again.'
  } finally {
    if (currentRequest === requestID) loading.value = false
  }
}

async function loadCollector() {
  const collectorID = id.value
  try {
    const [collector, defs] = await Promise.all([
      api.collectors.get(collectorID),
      api.indicators.list(collectorID)
    ])
    if (collectorID !== id.value) return
    collectorName.value = collector.name
    indicators.value = defs || []
  } catch (e: any) {
    if (collectorID === id.value) ElMessage.error(e?.response?.data?.error || 'Could not load collector')
  }
}

function openRun(row: any) {
  router.push({ name: 'run-detail', params: { id: row.id } })
}

watch([id, selected, dateRange, pageSize], () => {
  if (page.value !== 1) page.value = 1
  else void loadHistory()
}, { immediate: true })
watch(page, loadHistory)
watch(id, () => {
  collectorName.value = ''
  indicators.value = []
  selected.value = null
  void loadCollector()
}, { immediate: true })
</script>

<template>
  <div>
    <div class="page-bar">
      <h2>{{ collectorName || `Collector #${id}` }} — History</h2>
      <div class="page-bar-actions">
        <el-button :loading="loading" @click="loadHistory">Refresh</el-button>
        <el-button @click="router.push({ name: 'collector-edit', params: { id } })">Back to Edit</el-button>
      </div>
    </div>

    <el-form class="history-filters" label-position="top" @submit.prevent>
      <el-form-item label="Date range">
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          start-placeholder="Start date"
          end-placeholder="End date"
          range-separator="to"
          :shortcuts="dateShortcuts"
          :single-panel="ui.isMobile"
          popper-class="history-date-picker"
          :unlink-panels="true"
          clearable
        />
      </el-form-item>
      <el-form-item label="Indicator">
        <el-select v-model="selected" clearable placeholder="All indicators" class="indicator-select">
          <el-option v-for="i in indicators" :key="i.id" :label="`${i.name || i.key} (${i.key})`" :value="i.id" />
        </el-select>
      </el-form-item>
    </el-form>
    <p class="history-hint">Newest first · Dates use your local time · Click a run for step details</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon class="history-error" />

    <ResponsiveTable
      :rows="runs"
      :columns="columns"
      :loading="loading"
      row-key="id"
      row-clickable
      @row-click="openRun"
    >
      <template #run="{ row }">#{{ row.id }}</template>
      <template #status="{ row }">
        <el-tag :type="row.status === 'success' ? 'success' : row.status === 'failed' ? 'danger' : 'info'">
          {{ row.status }}
        </el-tag>
      </template>
      <template #started="{ row }">{{ new Date(row.started_at).toLocaleString() }}</template>
      <template #duration="{ row }">{{ row.status === 'running' ? '—' : `${row.duration_ms} ms` }}</template>
      <template #datapoints="{ row }">
        <div v-if="visibleDataPoints(row).length" class="dp-chips">
          <el-tag
            v-for="dp in visibleDataPoints(row)"
            :key="dp.id"
            size="small"
            type="info"
            effect="plain"
            class="dp-chip"
          >
            {{ indicatorByID[dp.indicator_id]?.name || indicatorByID[dp.indicator_id]?.key || `#${dp.indicator_id}` }}:
            {{ dpDisplay(dp) }} {{ indicatorByID[dp.indicator_id]?.unit || '' }}
          </el-tag>
        </div>
        <span v-else class="dp-empty">—</span>
      </template>
      <template #error="{ row }"><span class="run-error">{{ row.error || '—' }}</span></template>
    </ResponsiveTable>

    <div class="history-pagination">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[20, 50, 100, 200]"
        :pager-count="ui.isMobile ? 5 : 7"
        :layout="ui.isMobile ? 'prev, pager, next' : 'total, sizes, prev, pager, next'"
        :size="ui.isMobile ? 'small' : 'default'"
      />
    </div>
  </div>
</template>

<style scoped>
.history-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.history-filters :deep(.el-form-item) {
  margin-bottom: 0;
  max-width: 100%;
}
.history-filters :deep(.el-date-editor) {
  width: 360px;
  max-width: 100%;
  box-sizing: border-box;
}
.indicator-select {
  width: 240px;
}
.history-hint,
.dp-empty {
  color: var(--sg-text-secondary);
}
.history-hint {
  font-size: 12px;
  margin: 12px 0;
}
.history-error {
  margin-bottom: 12px;
}
.history-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
.dp-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.dp-chip {
  height: auto;
  max-width: 100%;
  padding: 3px 6px;
  white-space: normal;
  overflow-wrap: anywhere;
}
.run-error {
  overflow-wrap: anywhere;
}
@media (max-width: 768px) {
  .history-filters :deep(.el-form-item),
  .history-filters :deep(.el-date-editor),
  .indicator-select {
    width: 100%;
    min-width: 0;
  }
}
</style>

<style>
@media (max-width: 768px) {
  .history-date-picker .el-date-range-picker.single-panel.has-sidebar {
    width: min(340px, calc(100vw - 24px));
  }
  .history-date-picker .el-picker-panel__sidebar {
    position: static;
    display: flex;
    width: 100%;
    padding: 6px;
    border-right: 0;
    border-bottom: 1px solid var(--el-border-color-light);
  }
  .history-date-picker .el-picker-panel__shortcut {
    width: auto;
    flex: 1;
    padding: 0 6px;
    text-align: center;
  }
  .history-date-picker .el-date-range-picker.single-panel .el-picker-panel__body {
    min-width: 0;
    margin-left: 0;
  }
  .history-date-picker .el-date-range-picker.single-panel .el-picker-panel__content {
    width: auto;
  }
}
</style>
