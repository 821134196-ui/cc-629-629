// 后端 API 封装
const BASE = ''

async function request(method, path, body) {
  const opts = { method, headers: {} }
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json'
    opts.body = JSON.stringify(body)
  }
  const resp = await fetch(BASE + '/api' + path, opts)
  const data = await resp.json().catch(() => ({}))
  if (!resp.ok) {
    throw new Error(data.error || `请求失败 (${resp.status})`)
  }
  return data
}

export const api = {
  listElders: (q = '') => request('GET', '/elders' + (q ? `?q=${encodeURIComponent(q)}` : '')),
  createElder: (body) => request('POST', '/elders', body),
  qualifications: (id) => request('GET', `/elders/${id}/qualifications`),
  addQualification: (id, body) => request('POST', `/elders/${id}/qualifications`, body),
  delQualification: (id, qid) => request('DELETE', `/elders/${id}/qualifications/${qid}`),
  eligibility: (id, date) => request('GET', `/elders/${id}/eligibility?date=${date}`),
  meals: (id, from, to) =>
    request('GET', `/elders/${id}/meals?from=${from}&to=${to}`),
  checkin: (body) => request('POST', '/checkin', body),
  allMeals: (params = '') => request('GET', '/meals' + (params ? '?' + params : '')),
  recalc: (elderlyId) => request('POST', '/recalc', { elderly_id: elderlyId || 0 }),
  settlements: () => request('GET', '/settlements'),
  createSettlement: (period) => request('POST', `/settlements/${period}`),
  closeSettlement: (period) => request('POST', `/settlements/${period}/close`),
  settlementMeals: (period) => request('GET', `/settlements/${period}/meals`),
  previewAdjustment: (period, elderlyId) =>
    request('POST', `/settlements/${period}/adjustments/preview`, { elderly_id: elderlyId }),
  createAdjustment: (period, elderlyId, reason) =>
    request('POST', `/settlements/${period}/adjustments`, { elderly_id: elderlyId, reason }),
  adjustments: (period) => request('GET', '/adjustments' + (period ? `?period=${period}` : '')),
  reconcile: (period) => request('GET', `/settlements/${period}/reconcile`),
}

export function today() {
  return new Date().toISOString().slice(0, 10)
}

export function thisMonth() {
  return today().slice(0, 7)
}

export function fmtMoney(v) {
  return Number(v || 0).toFixed(2)
}

export const ACTION_LABEL = {
  activate: '资格生效',
  suspend: '暂停',
  resume: '恢复',
}

export const STATUS_LABEL = {
  active: '生效中',
  suspended: '已暂停',
}
