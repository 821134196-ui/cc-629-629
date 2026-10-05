<script setup>
import { ref, onMounted } from 'vue'
import { api } from './api.js'
import SeniorsView from './views/SeniorsView.vue'
import CanteenView from './views/CanteenView.vue'
import MealsView from './views/MealsView.vue'
import SettlementView from './views/SettlementView.vue'

const tab = ref('seniors')
const tabs = [
  { key: 'seniors', label: '老人与资格登记' },
  { key: 'canteen', label: '食堂扫码核销' },
  { key: 'meals', label: '老人就餐与补贴明细' },
  { key: 'settlement', label: '月结 / 重算 / 补差 / 对账' },
]

// 全局老人列表与当前选中老人，供多个页面共享
const seniors = ref([])
const seniorId = ref(null)
const refreshKey = ref(0)

async function loadSeniors() {
  seniors.value = await api.listSeniors()
  if (!seniorId.value && seniors.value.length) seniorId.value = seniors.value[0].id
}

onMounted(loadSeniors)
function onChanged() { refreshKey.value++; loadSeniors() }
</script>

<template>
  <header class="topbar">
    <h1>社区老人助餐资格变更与餐补结算系统</h1>
    <p>补贴按「实际就餐当日」资格计算并固化快照 · 逐餐核销 · 封账固化、封账后只追加补差单 · 身份核验与支付本地模拟</p>
  </header>
  <div class="layout">
    <div class="tabs">
      <button v-for="t in tabs" :key="t.key" :class="{ active: tab === t.key }" @click="tab = t.key">
        {{ t.label }}
      </button>
    </div>

    <SeniorsView v-if="tab === 'seniors'"
      :seniors="seniors" :senior-id="seniorId"
      @update:senior-id="v => (seniorId = v)" @changed="onChanged" />

    <CanteenView v-else-if="tab === 'canteen'"
      :seniors="seniors" :senior-id="seniorId"
      @update:senior-id="v => (seniorId = v)" @changed="onChanged" />

    <MealsView v-else-if="tab === 'meals'"
      :seniors="seniors" :senior-id="seniorId"
      @update:senior-id="v => (seniorId = v)" :refresh-key="refreshKey" />

    <SettlementView v-else
      :seniors="seniors" :senior-id="seniorId"
      @update:senior-id="v => (seniorId = v)" :refresh-key="refreshKey" @changed="onChanged" />
  </div>
</template>
