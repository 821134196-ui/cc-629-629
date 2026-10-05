<script setup>
import { ref, onMounted } from 'vue'
import { api, today } from '../api.js'

const form = ref({
  card_no: '',
  meal_date: today(),
  meal_type: 'lunch',
  price: 15,
})
const result = ref(null)
const msg = ref(null)
const recent = ref([])

function flash(kind, text, timeout = 6000) {
  msg.value = { kind, text }
  if (timeout) setTimeout(() => (msg.value = null), timeout)
}

async function scan() {
  if (!form.value.card_no) {
    flash('err', '请输入或扫描卡号')
    return
  }
  try {
    const r = await api.checkin({ ...form.value })
    result.value = r
    flash(r.duplicate ? 'warn' : 'ok', r.message, 0)
    await loadRecent()
  } catch (e) {
    result.value = null
    flash('err', e.message)
  }
}

async function loadRecent() {
  recent.value = await api.allMeals('')
}

const mealCN = (t) => (t === 'dinner' ? '晚餐' : '午餐')

onMounted(loadRecent)
</script>

<template>
  <div class="rule-box">
    核销规则：每次扫码都按<b>就餐当天</b>的资格实时判定，并把「资格状态、补贴标准、补贴金额、计算依据」
    随餐次一起<b>快照保存</b>。同一卡号 + 同一天 + 同一餐次重复扫码，<b>只核销一次</b>，
    返回原记录、不产生第二笔补贴、不重复扣款。身份核验与支付均为本地模拟。
  </div>

  <div v-if="msg" :class="['msg', msg.kind]">{{ msg.text }}</div>

  <div class="sidebar-grid">
    <div>
      <div class="card">
        <h2>扫码 / 刷卡核销</h2>
        <div class="row" style="flex-direction:column;align-items:stretch;gap:12px">
          <div class="field">
            <label>老人卡号</label>
            <input v-model="form.card_no" placeholder="如 CARD-0001（模拟扫码）" @keyup.enter="scan" />
          </div>
          <div class="row">
            <div class="field">
              <label>就餐日期</label>
              <input type="date" v-model="form.meal_date" />
            </div>
            <div class="field">
              <label>餐次</label>
              <select v-model="form.meal_type">
                <option value="lunch">午餐</option>
                <option value="dinner">晚餐</option>
              </select>
            </div>
            <div class="field">
              <label>餐费（元）</label>
              <input type="number" min="0" step="0.5" v-model.number="form.price" />
            </div>
          </div>
          <button class="btn" style="width:100%;padding:11px;font-size:15px" @click="scan">核销并模拟支付</button>
          <p class="muted">就餐日期可补录历史日期，系统仍按那一天的资格计算补贴。</p>
        </div>
      </div>

      <div v-if="result && result.meal" class="card">
        <h2>
          核销结果
          <span v-if="result.duplicate" class="badge dup" style="margin-left:8px">重复扫码 · 未重复计费</span>
          <span v-else :class="['badge', result.meal.status === 'active' ? 'active' : 'suspended']" style="margin-left:8px">
            {{ result.meal.status === 'active' ? '当日资格生效' : '当日资格暂停' }}
          </span>
        </h2>
        <div class="stat-row">
          <div class="stat"><div class="k">餐费</div><div class="v">¥{{ Number(result.meal.price).toFixed(2) }}</div></div>
          <div class="stat"><div class="k">政府补贴</div><div class="v" style="color:#03543f">¥{{ Number(result.meal.subsidy_amount).toFixed(2) }}</div></div>
          <div class="stat"><div class="k">老人自付</div><div class="v">¥{{ Number(result.meal.self_pay).toFixed(2) }}</div></div>
        </div>
        <table>
          <tbody>
            <tr><th style="width:110px">就餐日期/餐次</th><td>{{ result.meal.meal_date }} {{ mealCN(result.meal.meal_type) }}</td></tr>
            <tr><th>计算依据</th><td class="basis">{{ result.meal.basis_detail }}</td></tr>
            <tr><th>适用补贴标准</th><td>¥{{ Number(result.meal.subsidy_rate).toFixed(2) }} / 餐</td></tr>
            <tr v-if="result.verify"><th>身份核验</th><td><span class="badge active">通过</span> {{ result.verify.message }}<div class="txn">{{ result.meal.verify_txn_id }}</div></td></tr>
            <tr v-if="result.pay"><th>模拟支付</th><td>{{ result.pay.message }}<div class="txn">{{ result.meal.pay_txn_id }}</div></td></tr>
            <tr><th>重算次数</th><td>{{ result.meal.recalc_seq }}（月结前修正资格后会增加；核销/支付流水号不变）</td></tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="card">
      <h2>近期核销流水（含资格快照与结算状态）</h2>
      <table>
        <thead>
          <tr>
            <th>就餐日期</th><th>餐次</th><th>老人</th>
            <th class="num">餐费</th><th class="num">补贴</th><th class="num">自付</th>
            <th>当日资格</th><th>结算状态</th><th>计算依据</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in recent" :key="m.id">
            <td>{{ m.meal_date }}</td>
            <td>{{ mealCN(m.meal_type) }}</td>
            <td>#{{ m.elderly_id }}</td>
            <td class="num money">{{ Number(m.price).toFixed(2) }}</td>
            <td class="num money" style="color:#03543f;font-weight:600">{{ Number(m.subsidy_amount).toFixed(2) }}</td>
            <td class="num money">{{ Number(m.self_pay).toFixed(2) }}</td>
            <td><span :class="['badge', m.status === 'active' ? 'active' : 'suspended']">{{ m.status === 'active' ? '生效' : '暂停' }}</span></td>
            <td>
              <span v-if="m.settlement_id" class="badge closed">已封账 #{{ m.settlement_id }}</span>
              <span v-else class="badge open">未结算·可重算</span>
            </td>
            <td class="basis" style="max-width:260px">{{ m.basis_detail }}</td>
          </tr>
          <tr v-if="!recent.length"><td colspan="9" class="muted">暂无核销记录</td></tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
