import { http } from './http'

export const api = {
  login(username: string, password: string) {
    return http.post('/auth/login', { username, password }).then(r => r.data)
  },
  me() { return http.get('/auth/me').then(r => r.data) },
  changePassword(old_password: string, new_password: string) {
    return http.post('/auth/password', { old_password, new_password }).then(r => r.data)
  },
  registry() { return http.get('/registry/steps').then(r => r.data) },
  templates: {
    list() { return http.get('/templates').then(r => r.data) },
    get(name: string) { return http.get(`/templates/${encodeURIComponent(name)}`).then(r => r.data) },
    create(body: any) { return http.post('/templates', body).then(r => r.data) },
    update(name: string, body: any) { return http.put(`/templates/${encodeURIComponent(name)}`, body).then(r => r.data) },
    remove(name: string) { return http.delete(`/templates/${encodeURIComponent(name)}`).then(r => r.data) },
    import(body: { templates: any[]; on_conflict: 'skip' | 'overwrite' }) {
      return http.post('/templates/import', body).then(r => r.data)
    },
    exportAll(names?: string[]) {
      const params = names && names.length ? { names: names.join(',') } : {}
      return http.get('/templates/export', { params }).then(r => r.data)
    }
  },
  dashboard() { return http.get('/dashboard').then(r => r.data) },

  sites: {
    list() { return http.get('/sites').then(r => r.data) },
    get(id: number) { return http.get(`/sites/${id}`).then(r => r.data) },
    create(b: any) { return http.post('/sites', b).then(r => r.data) },
    update(id: number, b: any) { return http.put(`/sites/${id}`, b).then(r => r.data) },
    remove(id: number) { return http.delete(`/sites/${id}`).then(r => r.data) }
  },

  credentials: {
    list(siteId?: number) {
      return http.get('/credentials', { params: siteId ? { site_id: siteId } : {} }).then(r => r.data)
    },
    get(id: number) { return http.get(`/credentials/${id}`).then(r => r.data) },
    create(b: any) { return http.post('/credentials', b).then(r => r.data) },
    update(id: number, b: any) { return http.put(`/credentials/${id}`, b).then(r => r.data) },
    remove(id: number) { return http.delete(`/credentials/${id}`).then(r => r.data) }
  },

  collectors: {
    list(siteId?: number) {
      return http.get('/collectors', { params: siteId ? { site_id: siteId } : {} }).then(r => r.data)
    },
    get(id: number) { return http.get(`/collectors/${id}`).then(r => r.data) },
    create(b: any) { return http.post('/collectors', b).then(r => r.data) },
    update(id: number, b: any) { return http.put(`/collectors/${id}`, b).then(r => r.data) },
    remove(id: number) { return http.delete(`/collectors/${id}`).then(r => r.data) },
    run(id: number, params: any = {}) { return http.post(`/collectors/${id}/run`, { params }).then(r => r.data) },
    dryrun(id: number, params: any = {}) { return http.post(`/collectors/${id}/dryrun`, { params }).then(r => r.data) },
    runs(id: number, limit = 50) { return http.get(`/collectors/${id}/runs`, { params: { limit } }).then(r => r.data) },
    datapoints(id: number, params: any = {}) { return http.get(`/collectors/${id}/datapoints`, { params }).then(r => r.data) }
  },

  indicators: {
    list(collectorId: number) { return http.get(`/collectors/${collectorId}/indicators`).then(r => r.data) },
    create(collectorId: number, b: any) { return http.post(`/collectors/${collectorId}/indicators`, b).then(r => r.data) },
    update(id: number, b: any) { return http.put(`/indicators/${id}`, b).then(r => r.data) },
    remove(id: number) { return http.delete(`/indicators/${id}`).then(r => r.data) }
  },

  runs: {
    get(id: number) { return http.get(`/runs/${id}`).then(r => r.data) }
  }
}
