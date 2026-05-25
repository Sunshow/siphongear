<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

defineProps<{ collapsed?: boolean }>()
const emit = defineEmits<{ (e: 'navigate'): void }>()

const route = useRoute()
const activeMenu = computed(() => String(route.name || ''))

const items = [
  { name: 'dashboard', label: 'Dashboard', icon: 'Odometer' },
  { name: 'collectors', label: 'Collectors', icon: 'Connection' },
  { name: 'templates', label: 'Templates', icon: 'Files' },
  { name: 'sites', label: 'Sites', icon: 'MapLocation' },
  { name: 'credentials', label: 'Credentials', icon: 'Lock' },
  { name: 'settings', label: 'Settings', icon: 'Setting' }
]
</script>

<template>
  <div class="side-nav" :class="{ collapsed }">
    <div class="brand">
      <div class="logo">SG</div>
      <span v-if="!collapsed" class="brand-text">SiphonGear</span>
    </div>
    <el-menu :default-active="activeMenu" :collapse="collapsed" router>
      <el-menu-item
        v-for="it in items"
        :key="it.name"
        :index="it.name"
        :route="{ name: it.name }"
        @click="emit('navigate')"
      >
        <el-icon><component :is="it.icon" /></el-icon>
        <template #title>{{ it.label }}</template>
      </el-menu-item>
    </el-menu>
  </div>
</template>

<style scoped>
.side-nav {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--sg-aside-bg);
  border-right: 1px solid var(--sg-border-soft);
}
.brand {
  height: var(--sg-header-h);
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 16px;
  border-bottom: 1px solid var(--sg-border-soft);
}
.logo {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: linear-gradient(135deg, #6366f1, #8b5cf6);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 13px;
  letter-spacing: 0.5px;
  flex-shrink: 0;
}
.brand-text {
  font-size: 16px;
  font-weight: 600;
  color: var(--sg-text-primary);
  letter-spacing: -0.2px;
}
.side-nav.collapsed .brand {
  justify-content: center;
  padding: 0;
}
.side-nav :deep(.el-menu) {
  flex: 1;
  padding: 8px 8px;
  background: transparent;
}
.side-nav :deep(.el-menu-item) {
  border-radius: 8px;
  margin: 2px 0;
  height: 42px;
  line-height: 42px;
  color: var(--sg-aside-text);
}
.side-nav :deep(.el-menu-item:hover) {
  background: var(--sg-aside-hover-bg);
  color: var(--sg-text-primary);
}
.side-nav :deep(.el-menu-item.is-active) {
  background: var(--sg-aside-active-bg);
  color: var(--sg-aside-active-text);
  font-weight: 500;
}
.side-nav :deep(.el-menu--collapse) {
  width: var(--sg-aside-w-collapsed);
}
.side-nav.collapsed :deep(.el-menu-item) {
  justify-content: center;
}
</style>
