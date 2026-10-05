<script setup>
import { ref, watch, onMounted } from 'vue'
import { api, fen, statusText } from '../api.js'

const props = defineProps({ seniors: Array, seniorId: Number })
const emit = defineEmits(['update:senior-id', 'changed'])

const records = ref([])
const currentStatus = ref('')
const msg = ref('')
const err = ref('')

const newSenior = ref({ name: '', card_no: '', community: '' })
const form = ref({
  effective_date: '',
  status: 'active',
  level_name: '普通补贴对象',
  subsidy_yuan: 8,
  reason: '',
})
const editId = ref(null)

async function load() {
  if (!props.seniorId) { records.value = []; return }
  records.value = await api.listEligibility(props.seniorId)
  const st = await api.currentStatus(props.seniorId)
  currentStatus.value = st.current_status
}
onMounted(load)
watch(() => props.seniorId, () => { editId.value = null; load() })

async function addSenior() {
  err.value = ''; msg.value = ''
  try {
    await api.createSenior(newSenior.value)
    newSenior.value = { name: '', card_no: '', community: '' }
    msg.value = '老人已登记'
    emit('changed')
  } catch (e) { err.value = e.message }
}

async function addElig() {
  err.value = ''; msg.value = ''
  try {
    const b = {
      effective_date: form.value.effective_date,
      status: form.value.status,
      level_name: form.value.level_name,
      subsidy_fen: Math.round(Number(form.value.subsidy_yuan || 0) * 100),
      reason: form.value.reason,
    }
    if (editId.value) {
      await api.updateEligibility(editId.value, b)
      msg.value = `资格登记 #${editId.value} 已更正（历史餐次需经重算或补差体现）`
      editId.value = null
    } else {
      const rec = await api.addEligibility(props.seniorId, b)
      msg.value = `已登记 #${rec.id}：${rec.effective_date} 起「${statusText[rec.status]}」`
    }
    form.value = { effective_date: '', status: 'active', level_name: '普通补贴对象', subsidy_yuan: 8, reason: '' }
    await load()
    emit('changed')
  } catch (e) { err.value = e.message }
}

function startEdit(r) {
  editId.value = r.id
  form.value = {
    effective_date: r.effective_date,
    status: r.status,
    level_name: r.level_name || '普通补贴对象',
    subsidy_yuan: Number(r.subsidy_fen || 0) / 100,
    reason: r.reason || '',
  }
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

async function removeElig(r) {
  err.value = ''; msg.value = ''
  if (!confirm(`删除 ${r.effective_date} 的资格登记？已核销餐次快照不受影响。`)) return
  try {
    await api.deleteEligibility(r.id)
    if (editId.value === r.id) editId.value = null
    msg.value = `资格登记 #${r.id} 已删除`
    await load()
    emit('changed')
  } catch (e) { err.value = e.message }
}
</script>

<template>
  <div class="panel">
    <h2>新增社区老人</h2>
    <div class="sub">登记后即可维护其资格时间线（生效 / 暂停 / 恢复），资格按「日期」查询，支持补录历史日期。</div>
    <div class="grid cols-4">
      <div><label>姓名</label><input v-model="newSenior.name" placeholder="如：王秀兰" /></div>
      <div><label>卡号（唯一）</label><input v-model="newSenior.card_no" placeholder="如：C001" /></div>
      <div><label>所属社区</label><input v-model="newSenior.community" placeholder="如：阳光社区" /></div>
      <div class="row-actions"><button class="btn" @click="addSenior">登记老人</button></div>
    </div>
    <div v-if="msg" class="banner ok" style="margin-top:12px">{{ msg }}</div>
    <div v-if="err" class="banner err" style="margin-top:12px">{{ err }}</div>
  </div>

  <div class="panel">
    <div class="senior-bar">
      <h2 style="margin:0">资格时间线</h2>
      <select :value="seniorId" @change="emit('update:senior-id', Number($event.target.value))">
        <option v-for="s in seniors" :key="s.id" :value="s.id">{{ s.name }}（{{ s.card_no }}·{{ s.community }}）</option>
      </select>
      <span v-if="seniorId">当前状态：
        <span class="chip" :class="currentStatus">{{ statusText[currentStatus] || '—' }}</span>
        <span class="muted small">（仅展示，历史餐次补贴永远按就餐当日资格计算）</span>
      </span>
    </div>

    <div class="grid cols-4" v-if="seniorId">
      <div><label>生效日期</label><input type="date" v-model="form.effective_date" /></div>
      <div>
        <label>动作</label>
        <select v-model="form.status">
          <option value="active">生效 / 恢复（享补贴）</option>
          <option value="suspended">暂停（无补贴）</option>
        </select>
      </div>
      <div>
        <label>补贴等级</label>
        <input v-model="form.level_name" placeholder="如：高龄补贴对象" :disabled="form.status === 'suspended'" />
      </div>
      <div>
        <label>每餐补贴标准（元）</label>
        <input type="number" step="0.01" v-model="form.subsidy_yuan" :disabled="form.status === 'suspended'" />
      </div>
      <div style="grid-column:1 / span 3"><label>原因 / 备注</label><input v-model="form.reason" placeholder="如：本人申请暂停 / 复核通过恢复" /></div>
      <div class="row-actions">
        <button class="btn" @click="addElig">{{ editId ? '保存更正（#' + editId + '）' : '写入资格登记' }}</button>
        <button v-if="editId" class="btn ghost" @click="editId = null">取消编辑</button>
      </div>
    </div>
    <div v-if="err" class="banner err" style="margin-top:12px">{{ err }}</div>

    <table v-if="records.length">
      <thead>
        <tr><th>#</th><th>生效日期</th><th>状态</th><th>等级</th><th>每餐补贴标准</th><th>原因</th><th></th></tr>
      </thead>
      <tbody>
        <tr v-for="r in records" :key="r.id" :style="editId === r.id ? 'background:#eef2ff' : ''">
          <td>{{ r.id }}</td>
          <td>{{ r.effective_date }}</td>
          <td><span class="chip" :class="r.status">{{ statusText[r.status] }}</span></td>
          <td>{{ r.level_name || '—' }}</td>
          <td class="money">{{ r.status === 'active' ? fen(r.subsidy_fen) : '—' }}</td>
          <td class="basis">{{ r.reason || '—' }}</td>
          <td style="white-space:nowrap">
            <button class="btn ghost" style="padding:3px 10px" @click="startEdit(r)">更正</button>
            <button class="btn warn" style="padding:3px 10px" @click="removeElig(r)">删除</button>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-else class="muted small" style="margin-top:10px">尚无资格登记。某就餐日若无任何「生效日 ≤ 就餐日」的登记，按全自付处理。</p>
  </div>
</template>
