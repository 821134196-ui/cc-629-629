<script setup>
import { ref, computed, onMounted } from 'vue'
import { api, today } from '../api.js'

const elders = ref([])
const elderId = ref('')
const month = ref(today().slice(0, 7))
const meals = ref([])
const msg = ref(null)

async function loadElders() {
  elders.value = await api.listElders('')
  if (elders.value.length) elderId.value = elders.value[0].id
  loadMeals()
}

async function loadMeals() {
  msg.value = null
  if (!elderId.value) return
  try {
    meals.value = await api.meals(elderId.value, month.value + '-01', month.value + '-31')
  } catch (e) {
    msg.value = { kind: 'err', text: e.message }
  }
}

const elder = computed(() => elders.value.find((e) => e.id === Number(elderId.value)))
const totals = computed(() => {
  let price = 0, subsidy = 0, self = 0
  for (const m of meals.value) {
    price += m.price
    subsidy += m.subsidy_amount
    self += m.self_pay
  }
  return { price, subsidy, self, count: meals.value.length }
})

const mealCN = (t) => (t === 'dinner' ? '晚餐' : '午餐')
onMounted(loadElders)
</script>

<template>
  <div class="rule-box">
    老人可逐餐查看：<b>就餐日期、餐次、餐费、当餐补贴、自付金额以及计算依据</b>（依据哪一条资格记录）。
    每条金额来自核销当时保存的资格快照，之后社区修改资格也不会改变已封账餐次。
  </div>

  <div class="card">
    <div class="row">
      <div class="field">
        <label>选择老人（模拟本人登录）</label>
        <select v-model="elderId" @change="loadMeals" style="min-width:200px">
          <option v-for="e in elders" :key="e.id" :value="e.id">{{ e.name }}（{{ e.card_no }}）</option>
        </select>
      </div>
      <div class="field">
        <label>查询月份</label>
        <input type="month" v-model="month" @change="loadMeals" />
      </div>
    </div>
    <div v-if="elder" class="muted" style="margin-top:8px">
      {{ elder.name }} · 卡号 {{ elder.card_no }} · {{ elder.phone || '无电话' }}
    </div>
  </div>

  <div v-if="msg" :class="['msg', msg.kind]">{{ msg.text }}</div>

  <div class="card">
    <h2>本月就餐与补贴明细</h2>
    <div class="stat-row">
      <div class="stat"><div class="k">就餐餐次</div><div class="v">{{ totals.count }}</div></div>
      <div class="stat"><div class="k">餐费合计</div><div class="v">¥{{ totals.price.toFixed(2) }}</div></div>
      <div class="stat"><div class="k">补贴合计</div><div class="v" style="color:#03543f">¥{{ totals.subsidy.toFixed(2) }}</div></div>
      <div class="stat"><div class="k">自付合计</div><div class="v">¥{{ totals.self.toFixed(2) }}</div></div>
    </div>

    <table style="margin-top:12px">
      <thead>
        <tr>
          <th>就餐日期</th><th>餐次</th>
          <th class="num">餐费</th><th class="num">当餐补贴</th><th class="num">自付</th>
          <th>当日资格</th><th>计算依据（该餐日期适用哪条记录）</th>
          <th>结算状态</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="m in meals" :key="m.id">
          <td><b>{{ m.meal_date }}</b></td>
          <td>{{ mealCN(m.meal_type) }}</td>
          <td class="num money">¥{{ Number(m.price).toFixed(2) }}</td>
          <td class="num money" style="color:#03543f;font-weight:700">¥{{ Number(m.subsidy_amount).toFixed(2) }}</td>
          <td class="num money">¥{{ Number(m.self_pay).toFixed(2) }}</td>
          <td>
            <span :class="['badge', m.status === 'active' ? 'active' : 'suspended']">
              {{ m.status === 'active' ? '生效 ¥' + Number(m.subsidy_rate).toFixed(2) : '暂停' }}
            </span>
          </td>
          <td class="basis" style="max-width:330px">{{ m.basis_detail }}</td>
          <td>
            <span v-if="m.settlement_id" class="badge closed">已封账</span>
            <span v-else class="badge open">未结算</span>
            <div v-if="m.recalc_seq > 0" class="muted">已重算 {{ m.recalc_seq }} 次</div>
          </td>
        </tr>
        <tr v-if="!meals.length"><td colspan="8" class="muted">该月暂无就餐记录</td></tr>
      </tbody>
    </table>
  </div>
</template>
