<script setup>
import { ref } from 'vue'
import { api, fen, statusText } from '../api.js'

const props = defineProps({ seniors: Array, seniorId: Number })
const emit = defineEmits(['update:senior-id', 'changed'])

const form = ref({
  mode: 'select', // select=选老人 / scan=输入卡号
  senior_id: null,
  card_no: '',
  meal_date: new Date().toISOString().slice(0, 10),
  meal_type: '午餐',
  price_yuan: 15,
})

const result = ref(null)
const err = ref('')

async function scan() {
  err.value = ''; result.value = null
  try {
    const body = {
      meal_date: form.value.meal_date,
      meal_type: form.value.meal_type,
      price_fen: Math.round(Number(form.value.price_yuan || 0) * 100),
    }
    if (form.value.mode === 'scan') body.card_no = form.value.card_no.trim()
    else body.senior_id = props.seniorId
    result.value = await api.verify(body)
    emit('changed')
  } catch (e) { err.value = e.message }
}
</script>

<template>
  <div class="panel">
    <h2>食堂扫码 / 刷卡核销</h2>
    <div class="sub">
      核销瞬间以「就餐日」适用资格固化补贴快照，并完成本地模拟身份核验与支付。
      同一老人同一天同一餐次重复扫码只核销一次，不产生第二笔补贴。
    </div>

    <div class="grid cols-4">
      <div>
        <label>录入方式</label>
        <select v-model="form.mode">
          <option value="select">选择老人（模拟）</option>
          <option value="scan">输入卡号（模拟扫码）</option>
        </select>
      </div>
      <div v-if="form.mode === 'select'">
        <label>老人</label>
        <select :value="seniorId" @change="emit('update:senior-id', Number($event.target.value))">
          <option v-for="s in seniors" :key="s.id" :value="s.id">{{ s.name }}（{{ s.card_no }}）</option>
        </select>
      </div>
      <div v-else>
        <label>卡号</label><input v-model="form.card_no" placeholder="如：C001" />
      </div>
      <div><label>就餐日期（可补录历史餐）</label><input type="date" v-model="form.meal_date" /></div>
      <div>
        <label>餐次</label>
        <select v-model="form.meal_type"><option>午餐</option><option>晚餐</option></select>
      </div>
      <div><label>餐价（元）</label><input type="number" step="0.01" v-model="form.price_yuan" /></div>
      <div class="row-actions"><button class="btn" @click="scan">核验并核销</button></div>
    </div>

    <div v-if="err" class="banner err" style="margin-top:12px">{{ err }}</div>

    <div v-if="result" class="banner" :class="result.duplicate ? 'warn' : 'ok'" style="margin-top:12px">
      {{ result.message }}
    </div>

    <table v-if="result">
      <thead>
        <tr><th>就餐日期/餐次</th><th>当日资格</th><th>餐价</th><th>补贴</th><th>个人自付</th><th>计算依据</th><th>核验/支付流水</th></tr>
      </thead>
      <tbody>
        <tr>
          <td>{{ result.meal.meal_date }} {{ result.meal.meal_type }}<br />
            <span class="muted small">{{ result.meal.senior_name }}（{{ result.meal.card_no }}）</span></td>
          <td><span class="chip" :class="result.meal.eli_status">{{ statusText[result.meal.eli_status] }}</span>
            <div v-if="result.meal.eli_level" class="basis">{{ result.meal.eli_level }}</div></td>
          <td class="money">{{ fen(result.meal.price_fen) }}</td>
          <td class="money pos">{{ fen(result.meal.subsidy_fen) }}</td>
          <td class="money">{{ fen(result.meal.self_pay_fen) }}</td>
          <td class="basis">{{ result.meal.basis_describe }}</td>
          <td class="small"><code class="tx">{{ result.meal.verify_tx_id }}<br />{{ result.meal.pay_tx_id }}</code></td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
