async function req(method, url, body) {
  const opt = { method, headers: { 'Content-Type': 'application/json' } }
  if (body !== undefined) opt.body = JSON.stringify(body)
  const res = await fetch(url, opt)
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `请求失败 ${res.status}`)
  return data
}

export const api = {
  listSeniors: () => req('GET', '/api/seniors'),
  createSenior: (b) => req('POST', '/api/seniors', b),
  currentStatus: (id) => req('GET', `/api/seniors/${id}/status`),

  listEligibility: (id) => req('GET', `/api/seniors/${id}/eligibility`),
  addEligibility: (id, b) => req('POST', `/api/seniors/${id}/eligibility`, b),
  updateEligibility: (rid, b) => req('PUT', `/api/eligibility/${rid}`, b),
  deleteEligibility: (rid) => req('DELETE', `/api/eligibility/${rid}`),

  verify: (b) => req('POST', '/api/canteen/verify', b),
  listMeals: (id, period) =>
    req('GET', `/api/seniors/${id}/meals${period ? `?period=${period}` : ''}`),

  recalc: (id, b) => req('POST', `/api/seniors/${id}/recalc`, b),
  closeMonth: (id, period) => req('POST', `/api/seniors/${id}/settlements`, { period }),
  listSettlements: (seniorId, period) =>
    req('GET', `/api/settlements?${seniorId ? `senior_id=${seniorId}&` : ''}${period ? `period=${period}` : ''}`),
  getSettlement: (id) => req('GET', `/api/settlements/${id}`),
  createAdjustment: (id, b) => req('POST', `/api/seniors/${id}/adjustments`, b),
  listAdjustments: (seniorId, period) =>
    req('GET', `/api/adjustments?${seniorId ? `senior_id=${seniorId}&` : ''}${period ? `period=${period}` : ''}`),
  reconcile: (id, period) => req('GET', `/api/seniors/${id}/reconcile?period=${period}`),
}

export const fen = (n) => `¥${(Number(n || 0) / 100).toFixed(2)}`

export const statusText = {
  active: '生效（享补贴）',
  suspended: '暂停（无补贴）',
  inactive: '无生效登记',
}
