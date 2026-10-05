<script setup>
import { ref, watch, onMounted } from 'vue'
import { api, fen, statusText } from '../api.js'

const props = defineProps({ seniors: Array, seniorId: Number, refreshKey: Number })
const emit = defineEmits(['update:senior-id'])

const period = ref(new Date().toISOString().slice(0, 7))
const meals = ref([])
const totals = ref({ count: 0, subsidy: 0, self: 0 })

async function load() {
  if (!props.seniorId) { meals.value = []; return }
  meals.value = await api.listMeals(props.seniorId, period.value)
  totals.value = meals.value.reduce((a, m) => ({
    count: a.count + 1,
    subsidy: a.subsidy + Number(m.subsidy_fen),
    self: a.self + Number(m.self_pay_fen),
  }), { count: 0, subsidy: 0, self: 0 })
}
onMounted(load)
watch(() => [props.seniorId, period.value, props.refreshKey], load)
</script>

<template>
  <div class="panel">
    <div class="senior-bar">
      <h2 style="margin:0">老人就餐与每餐补贴</h2>
      <select :value="seniorId" @change="emit('update:senior-id', Number($event.target.value))">
        <option v-for="s in seniors" :key="s.id" :value="s.id">{{ s.name }}（{{ s.card_no }}）</option>
      </select>
      <input type="month" v-model="period" style="max-width:170px" />
    </div>
    <div class="sub">每餐后的补贴金额与计算依据在核销时固化；「重算」会产生修订版本号，原作废流水仍留痕。</div>

    <div class="grid cols-3" style="margin-bottom:12px">
      <div class="stat"><div class="k">餐次</div><div class="v">{{ totals.count }}</div></div>
      <div class="stat"><div class="k">补贴合计</div><div class="v pos">{{ fen(totals.subsidy) }}</div></div>
      <div class="stat"><div class="k">个人自付合计</div><div class="v">{{ fen(totals.self) }}</div></div>
    </div>

    <table v-if="meals.length">
      <thead>
        <tr><th>日期</th><th>餐次</th><th>当日资格</th><th>餐价</th><th>补贴</th><th>自付</th><th>状态</th><th>版本</th><th>计算依据</th></tr>
      </thead>
      <tbody>
        <tr v-for="m in meals" :key="m.id">
          <td>{{ m.meal_date }}</td>
          <td>{{ m.meal_type }}</td>
          <td><span class="chip" :class="m.eli_status">{{ statusText[m.eli_status] }}</span>
            <div class="basis">{{ m.eli_level || '—' }} · 标准 {{ fen(m.subsidy_rate_fen) }}</div></td>
          <td class="money">{{ fen(m.price_fen) }}</td>
          <td class="money pos">{{ fen(m.subsidy_fen) }}</td>
          <td class="money">{{ fen(m.self_pay_fen) }}</td>
          <td><span class="chip" :class="m.settled ? 'settled' : 'unsettled'">{{ m.settled ? '已封账' : '未封账' }}</span></td>
          <td>v{{ m.revision }}</td>
          <td class="basis">{{ m.basis_describe }}</td>
        </tr>
      </tbody>
    </table>
    <p v-else class="muted small">该月暂无就餐记录。</p>
  </div>
</template>
