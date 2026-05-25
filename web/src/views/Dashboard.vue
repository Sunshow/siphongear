<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { api } from '@/api'

interface Card {
  collector_id: number
  collector_name: string
  site_id: number
  site_name: string
  site_base_url: string
  indicator_id: number
  key: string
  name: string
  type: string
  unit: string
  display: string
  value_num: number | null
  value_str: string | null
  value_json: string | null
  ts: string | null
  last_status: string
}

const router = useRouter()
const cards = ref<Card[]>([])
const loading = ref(false)
const refreshing = ref<Record<number, boolean>>({})

async function reload() {
  loading.value = true
  try {
    cards.value = await api.dashboard()
  } finally {
    loading.value = false
  }
}

async function refreshAll() {
  if (loading.value) return
  loading.value = true
  try {
    const ids = Array.from(new Set(cards.value.map(c => c.collector_id))).filter(Boolean)
    if (!ids.length) {
      cards.value = await api.dashboard()
      return
    }
    const results = await Promise.allSettled(ids.map(id => api.collectors.run(id, {})))
    const failed = results.filter(r => r.status === 'rejected').length
    cards.value = await api.dashboard()
    if (failed) {
      ElMessage.warning(`${ids.length - failed}/${ids.length} collectors refreshed; ${failed} failed`)
    } else {
      ElMessage.success(`refreshed ${ids.length} collector(s)`)
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'refresh failed')
  } finally {
    loading.value = false
  }
}

function formatValue(c: Card): string {
  if (c.value_num !== null && c.value_num !== undefined) {
    const n = c.value_num
    if (Number.isInteger(n)) return String(n)
    return n.toFixed(Math.min(4, Math.max(0, 6 - String(Math.trunc(n)).length)))
  }
  if (c.value_str) return c.value_str
  if (c.value_json) return c.value_json
  return '—'
}

function relativeTime(iso: string | null): string {
  if (!iso) return 'no data'
  const t = new Date(iso).getTime()
  const diff = (Date.now() - t) / 1000
  if (diff < 60) return `${Math.floor(diff)}s ago`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

function statusType(status: string): 'success' | 'danger' | 'info' | 'warning' {
  if (status === 'success') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'running') return 'warning'
  return 'info'
}

// hash-derived accent color for cards (consistent per site)
function siteAccent(siteId: number): string {
  const palette = [
    '#5b8def', '#36b37e', '#f5a524', '#e25c84',
    '#9b59b6', '#26a69a', '#ef6c00', '#3f51b5'
  ]
  return palette[siteId % palette.length] || '#909399'
}

async function refresh(card: Card) {
  refreshing.value[card.collector_id] = true
  try {
    await api.collectors.run(card.collector_id, {})
    await reload()
    ElMessage.success(`refreshed ${card.collector_name}`)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || 'run failed')
  } finally {
    refreshing.value[card.collector_id] = false
  }
}

function gotoRuns(card: Card) {
  router.push({ name: 'collector-runs', params: { id: card.collector_id } })
}

function gotoEdit(card: Card) {
  router.push({ name: 'collector-edit', params: { id: card.collector_id } })
}

const groupedBySite = computed(() => {
  const groups: Record<string, { siteId: number; siteName: string; baseUrl: string; cards: Card[] }> = {}
  for (const c of cards.value) {
    const key = c.site_name || `Site #${c.site_id || 0}`
    if (!groups[key]) groups[key] = { siteId: c.site_id, siteName: key, baseUrl: c.site_base_url || '', cards: [] }
    else if (!groups[key].baseUrl && c.site_base_url) groups[key].baseUrl = c.site_base_url
    groups[key].cards.push(c)
  }
  return Object.values(groups).sort((a, b) => a.siteName.localeCompare(b.siteName))
})

onMounted(reload)
</script>

<template>
  <div>
    <div class="page-bar">
      <div>
        <h2>Dashboard</h2>
        <div class="subtitle">{{ cards.length }} indicator(s) across {{ groupedBySite.length }} site(s)</div>
      </div>
      <div class="page-bar-actions">
        <el-button :loading="loading" @click="reload">Reload</el-button>
        <el-button type="primary" :loading="loading" @click="refreshAll">Refresh All</el-button>
      </div>
    </div>

    <el-empty v-if="!cards.length && !loading" description="No indicators yet. Add some collectors first." />

    <el-row :gutter="16">
      <el-col
        v-for="group in groupedBySite"
        :key="group.siteName"
        :xs="24" :sm="24" :md="12" :lg="8" :xl="6"
      >
        <div class="site-card" :style="{ '--accent': siteAccent(group.siteId) } as any">
          <div class="site-header">
            <span class="site-dot"></span>
            <a
              v-if="group.baseUrl"
              class="site-name site-link"
              :href="group.baseUrl"
              target="_blank"
              rel="noopener noreferrer"
              :title="group.baseUrl"
              @click.stop
            >{{ group.siteName }}</a>
            <span v-else class="site-name">{{ group.siteName }}</span>
            <span class="site-count">{{ group.cards.length }}</span>
          </div>

          <div class="metrics">
            <div v-for="c in group.cards" :key="c.indicator_id" class="metric">
              <div class="metric-head">
                <span class="metric-name">{{ c.name }}</span>
                <el-tag size="small" :type="statusType(c.last_status)" effect="plain">
                  {{ c.last_status || '—' }}
                </el-tag>
              </div>

              <div class="metric-value">
                <span class="num">{{ formatValue(c) }}</span>
                <span v-if="c.unit" class="unit">{{ c.unit }}</span>
              </div>

              <div class="meta">
                <span class="key">{{ c.key }}</span>
                <span class="dot">·</span>
                <span class="collector">{{ c.collector_name }}</span>
              </div>

              <div class="footer">
                <span class="ts">{{ relativeTime(c.ts) }}</span>
                <div class="actions">
                  <el-button
                    link size="small" :loading="refreshing[c.collector_id]"
                    @click="refresh(c)"
                  >Run</el-button>
                  <el-button link size="small" @click="gotoRuns(c)">History</el-button>
                  <el-button link size="small" @click="gotoEdit(c)">Edit</el-button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped>
.subtitle { color: var(--sg-text-secondary); font-size: 13px; margin-top: 4px; }

.site-card {
  position: relative;
  background: var(--sg-bg-card);
  border: 1px solid var(--sg-border-soft);
  border-radius: var(--sg-radius);
  padding: 14px 16px 12px;
  margin-bottom: 16px;
  overflow: hidden;
  transition: box-shadow .2s, transform .2s;
  box-shadow: var(--ep-box-shadow-light);
}
.site-card::before {
  content: '';
  position: absolute;
  left: 0; top: 0; bottom: 0;
  width: 3px;
  background: var(--accent, #6366f1);
}
.site-card:hover {
  box-shadow: 0 8px 22px rgba(15, 23, 42, .08);
  transform: translateY(-1px);
}

.site-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-bottom: 10px;
  margin-bottom: 10px;
  border-bottom: 1px solid var(--sg-border-soft);
}
.site-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent, #6366f1);
}
.site-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--sg-text-primary);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.site-link {
  color: inherit;
  text-decoration: none;
  cursor: pointer;
}
.site-link:hover {
  color: var(--accent, #6366f1);
  text-decoration: underline;
}
.site-count {
  font-size: 12px;
  color: var(--sg-text-secondary);
  background: var(--sg-aside-hover-bg);
  padding: 1px 8px;
  border-radius: 10px;
}

.metrics {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.metric {
  padding: 10px 0;
  border-bottom: 1px dashed var(--sg-border-soft);
}
.metric:last-child {
  border-bottom: none;
  padding-bottom: 2px;
}

.metric-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
  gap: 6px;
}
.metric-name {
  font-size: 13px;
  color: var(--sg-text-secondary);
  font-weight: 500;
}

.metric-value {
  display: flex;
  align-items: baseline;
  gap: 6px;
  margin-bottom: 6px;
  min-height: 32px;
}
.metric-value .num {
  font-size: 24px;
  font-weight: 600;
  color: var(--sg-text-primary);
  line-height: 1;
  letter-spacing: -0.5px;
}
.metric-value .unit {
  font-size: 12px;
  color: var(--sg-text-secondary);
}

.meta {
  font-size: 12px;
  color: var(--sg-text-secondary);
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
  flex-wrap: wrap;
}
.meta .key {
  font-family: ui-monospace, "SF Mono", Menlo, monospace;
  background: var(--sg-aside-hover-bg);
  padding: 1px 6px;
  border-radius: 4px;
  color: var(--sg-text-primary);
}
.meta .dot { color: var(--sg-text-muted); }
.meta .collector {
  color: var(--sg-text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}
.ts {
  font-size: 11px;
  color: var(--sg-text-muted);
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 2px;
}
.actions :deep(.el-button + .el-button) {
  margin-left: 4px;
}
.actions :deep(.el-button) {
  padding: 4px 6px;
  height: auto;
  font-size: 12px;
}
</style>
