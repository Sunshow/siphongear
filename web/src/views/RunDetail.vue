<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/api'
import { useUiStore } from '@/store/ui'

const route = useRoute()
const router = useRouter()
const ui = useUiStore()
const id = computed(() => Number(route.params.id))

const run = ref<any>(null)
const logs = ref<any[]>([])
const datapoints = ref<any[]>([])
const indicators = ref<any[]>([])

const indicatorName = computed<Record<number, string>>(() => {
  const m: Record<number, string> = {}
  for (const i of indicators.value) m[i.id] = i.name || i.key
  return m
})

function dpDisplay(row: any): string {
  if (row.value_num !== null && row.value_num !== undefined) return String(row.value_num)
  if (row.value_str !== null && row.value_str !== undefined) return row.value_str
  if (row.value_json !== null && row.value_json !== undefined) return String(row.value_json)
  return '—'
}

async function load() {
  const res = await api.runs.get(id.value)
  run.value = res.run
  logs.value = res.step_logs || []
  datapoints.value = res.data_points || []
  indicators.value = res.indicators || []
}

onMounted(load)
</script>

<template>
  <div v-if="run">
    <div class="page-bar">
      <h2>Run #{{ run.id }}</h2>
      <div class="page-bar-actions">
        <el-button @click="router.back()">Back</el-button>
      </div>
    </div>

    <el-descriptions border :column="ui.isMobile ? 1 : 3" :direction="ui.isMobile ? 'vertical' : 'horizontal'">
      <el-descriptions-item label="Collector">{{ run.collector_id }}</el-descriptions-item>
      <el-descriptions-item label="Status">
        <el-tag :type="run.status === 'success' ? 'success' : run.status === 'failed' ? 'danger' : 'info'">
          {{ run.status }}
        </el-tag>
      </el-descriptions-item>
      <el-descriptions-item label="Trigger">{{ run.trigger }}</el-descriptions-item>
      <el-descriptions-item label="Started">{{ new Date(run.started_at).toLocaleString() }}</el-descriptions-item>
      <el-descriptions-item label="Duration">{{ run.duration_ms }} ms</el-descriptions-item>
      <el-descriptions-item v-if="run.error" label="Error">{{ run.error }}</el-descriptions-item>
    </el-descriptions>

    <h3 style="margin-top: 24px">Data Points</h3>
    <div v-if="datapoints.length" class="dp-list">
      <div v-for="dp in datapoints" :key="dp.id" class="dp-item">
        <span class="dp-name">{{ indicatorName[dp.indicator_id] || dp.indicator_id }}</span>
        <span class="dp-value">{{ dpDisplay(dp) }}</span>
      </div>
    </div>
    <el-empty v-else description="No data points" :image-size="60" />

    <h3 style="margin-top: 24px">Steps</h3>
    <el-timeline>
      <el-timeline-item v-for="s in logs" :key="s.id" :type="s.error ? 'danger' : 'success'" :timestamp="`#${s.index} · ${s.kind} · ${s.duration_ms} ms`">
        <div v-if="s.error" class="err">{{ s.error }}</div>
        <pre v-else class="snippet">{{ s.snippet || '(empty)' }}</pre>
      </el-timeline-item>
    </el-timeline>
  </div>
</template>

<style scoped>
.snippet {
  background: var(--sg-aside-hover-bg);
  color: var(--sg-text-primary);
  padding: 8px;
  border-radius: 6px;
  max-height: 240px;
  overflow: auto;
  font-size: 12px;
}
.err { color: var(--el-color-danger); font-weight: 500; }
.dp-list {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.dp-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  background: var(--sg-bg-card);
  border: 1px solid var(--sg-border-soft);
  border-radius: var(--sg-radius);
  padding: 8px 12px;
  min-width: 120px;
}
.dp-name {
  font-size: 12px;
  color: var(--sg-text-secondary);
}
.dp-value {
  font-size: 16px;
  font-weight: 600;
  color: var(--sg-text-primary);
  word-break: break-all;
}
</style>
