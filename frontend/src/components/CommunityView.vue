<script setup>
import { ref, onMounted } from 'vue'
import { api, today, ACTION_LABEL } from '../api.js'

const elders = ref([])
const selectedId = ref(null)
const quals = ref([])
const search = ref('')
const msg = ref(null)

const newElder = ref({ name: '', card_no: '', phone: '' })
const newQual = ref({ date: today(), action: 'activate', subsidy_rate: 10, note: '' })
const previewDate = ref(today())
const preview = ref(null)

function flash(kind, text) {
  msg.value = { kind, text }
  setTimeout(() => (msg.value = null), 5000)
}

async function loadElders() {
  elders.value = await api.listElders(search.value)
  if (!selectedId.value && elders.value.length) selectedId.value = elders.value[0].id
  if (selectedId.value) loadDetail()
}

async function selectElder(id) {
  selectedId.value = id
  await loadDetail()
}

async function loadDetail() {
  quals.value = await api.qualifications(selectedId.value)
  await loadPreview()
}

async function loadPreview() {
  try {
    preview.value = await api.eligibility(selectedId.value, previewDate.value)
  } catch {
    preview.value = null
  }
}

async function createElder() {
  try {
    const e = await api.createElder(newElder.value)
    newElder.value = { name: '', card_no: '', phone: '' }
    flash('ok', `已建档：${e.name}（卡号 ${e.card_no}）`)
    await loadElders()
    selectedId.value = e.id
    await loadDetail()
  } catch (e) {
    flash('err', e.message)
  }
}

async function addQual() {
  try {
    const r = await api.addQualification(selectedId.value, newQual.value)
    flash('ok', r.message + '（同一日期重复登记将覆盖原记录）')
    newQual.value.note = ''
    await loadDetail()
  } catch (e) {
    flash('err', e.message)
  }
}

async function delQual(qid) {
  if (!confirm('确认删除这条资格记录？删除后未结算餐次需点“重新计算”，已封账餐次不受影响。')) return
  try {
    const r = await api.delQualification(selectedId.value, qid)
    flash('ok', r.message)
    await loadDetail()
  } catch (e) {
    flash('err', e.message)
  }
}

const selected = () => elders.value.find((e) => e.id === selectedId.value)

onMounted(loadElders)
</script>

<template>
  <div class="rule-box">
    登记规则：每条资格记录都带<b>生效日期</b>。食堂核销时按<b>就餐当天</b>适用的最后一条记录判定，
    月底不会统一套用当前状态。例：9/20 暂停、10/3 恢复，则 10/1 的就餐仍按暂停计算。
  </div>

  <div v-if="msg" :class="['msg', msg.kind]">{{ msg.text }}</div>

  <div class="sidebar-grid">
    <!-- 左：老人列表 / 建档 -->
    <div>
      <div class="card">
        <h2>老人建档</h2>
        <div class="row">
          <div class="field"><label>姓名</label><input v-model="newElder.name" placeholder="姓名" /></div>
          <div class="field"><label>助餐卡号</label><input v-model="newElder.card_no" placeholder="CARD-0001" /></div>
          <div class="field"><label>电话</label><input v-model="newElder.phone" placeholder="选填" /></div>
          <button class="btn" @click="createElder">建档</button>
        </div>
      </div>

      <div class="card">
        <h2>老人名单</h2>
        <div class="row" style="margin-bottom:10px">
          <input v-model="search" placeholder="搜索姓名/卡号" @keyup.enter="loadElders" style="min-width:180px" />
          <button class="btn secondary" @click="loadElders">搜索</button>
        </div>
        <div class="elder-list">
          <div
            v-for="e in elders" :key="e.id"
            :class="['elder-item', { sel: e.id === selectedId }]"
            @click="selectElder(e.id)"
          >
            <div class="n">{{ e.name }}</div>
            <div class="c">{{ e.card_no }} · {{ e.phone || '无电话' }}</div>
          </div>
          <div v-if="!elders.length" class="muted">暂无老人，请先建档</div>
        </div>
      </div>
    </div>

    <!-- 右：资格记录 -->
    <div v-if="selected()">
      <div class="card">
        <h2>{{ selected().name }} 的资格记录</h2>

        <h3>登记 / 修正（生效、暂停、恢复）</h3>
        <div class="row">
          <div class="field">
            <label>日期（自该日起生效）</label>
            <input type="date" v-model="newQual.date" />
          </div>
          <div class="field">
            <label>动作</label>
            <select v-model="newQual.action">
              <option value="activate">资格生效</option>
              <option value="suspend">暂停</option>
              <option value="resume">恢复</option>
            </select>
          </div>
          <div class="field" v-if="newQual.action === 'activate'">
            <label>补贴标准（元/餐）</label>
            <input type="number" min="0" step="0.5" v-model.number="newQual.subsidy_rate" />
          </div>
          <div class="field">
            <label>备注</label>
            <input v-model="newQual.note" placeholder="如：资料复核暂停" />
          </div>
          <button class="btn" @click="addQual">保存记录</button>
        </div>
        <p class="muted" style="margin-top:8px">
          同一老人同一日期只保留一条记录，重复保存即覆盖（用于月结前纠错）；恢复动作自动沿用暂停前的补贴标准。
        </p>

        <h3>任意日期资格预览（按就餐日期判定）</h3>
        <div class="row">
          <input type="date" v-model="previewDate" @change="loadPreview" />
          <button class="btn secondary" @click="loadPreview">查询该日资格</button>
          <span v-if="preview">
            <span :class="['badge', preview.status === 'active' ? 'active' : 'suspended']">
              {{ preview.status === 'active' ? '生效中' : preview.status === 'suspended' ? '已暂停' : '无资格' }}
            </span>
            <span v-if="preview.subsidy_rate > 0" style="margin-left:8px">
              补贴标准 ¥{{ Number(preview.subsidy_rate).toFixed(2) }}/餐
            </span>
          </span>
        </div>
        <div v-if="preview" class="basis" style="margin-top:6px">{{ preview.basis_detail }}</div>

        <h3>已登记记录（按日期倒序）</h3>
        <table>
          <thead>
            <tr><th>日期</th><th>动作</th><th class="num">补贴标准</th><th>备注</th><th>登记时间</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="q in quals" :key="q.id">
              <td>{{ q.date }}</td>
              <td>{{ ACTION_LABEL[q.action] }}</td>
              <td class="num">{{ q.action === 'activate' ? '¥' + Number(q.subsidy_rate || 0).toFixed(2) : q.action === 'resume' ? '沿用生效标准' : '—' }}</td>
              <td>{{ q.note || '—' }}</td>
              <td class="muted">{{ q.created_at?.replace('T', ' ').slice(0, 16) }}</td>
              <td><button class="btn danger" style="padding:3px 10px" @click="delQual(q.id)">删除</button></td>
            </tr>
            <tr v-if="!quals.length"><td colspan="6" class="muted">尚未登记资格</td></tr>
          </tbody>
        </table>
      </div>
    </div>
    <div v-else class="card muted">请从左侧选择一位老人</div>
  </div>
</template>
