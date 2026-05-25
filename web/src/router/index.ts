import { createRouter, createWebHistory } from 'vue-router'
import Login from '@/views/Login.vue'
import Dashboard from '@/views/Dashboard.vue'
import Sites from '@/views/Sites.vue'
import Credentials from '@/views/Credentials.vue'
import Collectors from '@/views/Collectors.vue'
import CollectorEdit from '@/views/CollectorEdit.vue'
import CollectorRuns from '@/views/CollectorRuns.vue'
import RunDetail from '@/views/RunDetail.vue'
import Settings from '@/views/Settings.vue'
import TemplateCenter from '@/views/TemplateCenter.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: Login },
    { path: '/', redirect: '/dashboard' },
    { path: '/dashboard', name: 'dashboard', component: Dashboard },
    { path: '/sites', name: 'sites', component: Sites },
    { path: '/credentials', name: 'credentials', component: Credentials },
    { path: '/collectors', name: 'collectors', component: Collectors },
    { path: '/collectors/new', name: 'collector-new', component: CollectorEdit },
    { path: '/collectors/:id', name: 'collector-edit', component: CollectorEdit, props: true },
    { path: '/collectors/:id/runs', name: 'collector-runs', component: CollectorRuns, props: true },
    { path: '/runs/:id', name: 'run-detail', component: RunDetail, props: true },
    { path: '/templates', name: 'templates', component: TemplateCenter },
    { path: '/settings', name: 'settings', component: Settings }
  ]
})

router.beforeEach((to) => {
  const tok = localStorage.getItem('token')
  if (!tok && to.name !== 'login') return { name: 'login' }
  if (tok && to.name === 'login') return { name: 'dashboard' }
})
